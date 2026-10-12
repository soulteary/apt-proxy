package resource

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"

	httpcache "github.com/soulteary/httpcache-kit/v4"
)

func testManager(t *testing.T, total int64, active int) *Manager {
	t.Helper()
	m, err := New(Config{MaxObjectBytes: 64, TotalTempBytes: total, MaxActiveDownloads: active})
	if err != nil {
		t.Fatal(err)
	}
	return m
}
func TestManagerConcurrentNonblockingBudgetAndReuse(t *testing.T) {
	m := testManager(t, 64, 2)
	leases := make(chan httpcache.StagingLease, 20)
	var workers sync.WaitGroup
	for i := 0; i < 20; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			l, err := m.NewLease(context.Background(), httpcache.StageHandlerBody)
			if err != nil {
				t.Error(err)
				return
			}
			if err := l.Reserve(16); err != nil {
				if !errors.Is(err, ErrAdmission) {
					t.Error(err)
				}
				l.Close()
				return
			}
			leases <- l
		}()
	}
	workers.Wait()
	close(leases)
	s := m.Stats()
	if s.TemporaryBytes != 64 || s.PeakTemporaryBytes != 64 {
		t.Fatalf("temporary quota exceeded or admission waited: %+v", s)
	}
	count := 0
	for l := range leases {
		count++
		l.Release(4)
		l.Close()
		l.Close()
	}
	if count != 4 || m.Stats().TemporaryBytes != 0 {
		t.Fatalf("release count=%d stats=%+v", count, m.Stats())
	}
	l, err := m.NewLease(context.Background(), StageS3Spill)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if err := l.Reserve(64); err != nil {
		t.Fatalf("released quota not reusable: %v", err)
	}
}
func TestManagerCountsEveryOverlappingCopy(t *testing.T) {
	m := testManager(t, 16, 1)
	handler, _ := m.NewLease(context.Background(), httpcache.StageHandlerBody)
	defer handler.Close()
	inline, _ := m.NewLease(context.Background(), StageS3Inline)
	defer inline.Close()
	spill, _ := m.NewLease(context.Background(), StageS3Spill)
	defer spill.Close()
	if err := handler.Reserve(8); err != nil {
		t.Fatal(err)
	}
	if err := inline.Reserve(8); err != nil {
		t.Fatal(err)
	}
	if err := spill.Reserve(8); !errors.Is(err, ErrAdmission) {
		t.Fatalf("transition double-copy not counted: %v", err)
	}
	inline.Close()
	if err := spill.Reserve(8); err != nil {
		t.Fatal(err)
	}
	if s := m.Stats(); s.TemporaryBytes != 16 || s.ByKind[httpcache.StageHandlerBody] != 8 || s.ByKind[StageS3Spill] != 8 {
		t.Fatalf("stage ledger=%+v", s)
	}
}
func TestManagerDownloadAdmissionCancellationAndObjectCaps(t *testing.T) {
	m := testManager(t, 64, 1)
	first, err := m.AcquireDownload(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.AcquireDownload(context.Background()); !errors.Is(err, ErrAdmission) {
		t.Fatalf("extra download admitted: %v", err)
	}
	first.Close()
	first.Close()
	second, err := m.AcquireDownload(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.AcquireDownload(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled download admitted: %v", err)
	}
	if err := m.CheckObjectSize(64); err != nil {
		t.Fatal(err)
	}
	if err := m.CheckObjectSize(65); !errors.Is(err, ErrObjectTooLarge) {
		t.Fatalf("oversized object admitted: %v", err)
	}
	l, err := m.NewLease(context.Background(), StageUnknownSpool)
	if err != nil {
		t.Fatal(err)
	}
	l.Close()
	if err := l.Reserve(1); !errors.Is(err, ErrAdmission) {
		t.Fatalf("closed lease admitted bytes: %v", err)
	}
}
func TestManagerHeadroomConservativelyAccountsPendingReservations(t *testing.T) {
	m, err := New(Config{MaxObjectBytes: 64, TotalTempBytes: 1024, MaxActiveDownloads: 1, MinFreeBytes: 10, DiskDirs: []string{t.TempDir()}})
	if err != nil {
		t.Fatal(err)
	}
	m.available = func(string) (int64, error) { return 100, nil }
	first, _ := m.NewLease(context.Background(), httpcache.StageCacheBody)
	defer first.Close()
	second, _ := m.NewLease(context.Background(), StageUnknownSpool)
	defer second.Close()
	if err := first.Reserve(60); err != nil {
		t.Fatal(err)
	}
	if err := second.Reserve(31); !errors.Is(err, ErrAdmission) {
		t.Fatalf("spent same free bytes twice: %v", err)
	}
	m.available = func(string) (int64, error) { return 9, nil }
	if err := second.Reserve(1); !errors.Is(err, ErrAdmission) {
		t.Fatalf("disk headroom ignored: %v", err)
	}
	m.available = func(string) (int64, error) { return 0, errors.New("disk stat failed") }
	if err := second.Reserve(1); !errors.Is(err, ErrAdmission) {
		t.Fatalf("disk inspection failed open: %v", err)
	}
	if saturatingFree(math.MaxUint64, 4096) != math.MaxInt64 {
		t.Fatal("free-space calculation overflowed")
	}
}
