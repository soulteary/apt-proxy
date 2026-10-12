//go:build linux || darwin

package resource

import (
	"context"
	"errors"
	"testing"

	httpcache "github.com/soulteary/httpcache-kit/v4"
	"golang.org/x/sys/unix"
)

func assertInvalidStatfsRefusesAdmission(t *testing.T, stat unix.Statfs_t) {
	t.Helper()
	free, err := statfsFreeBytes(stat)
	if free != 0 || err == nil {
		t.Fatalf("invalid filesystem reported capacity: free=%d err=%v", free, err)
	}
	m, err := New(Config{MaxObjectBytes: 8, TotalTempBytes: 8, MaxActiveDownloads: 1, MinFreeBytes: 1, DiskDirs: []string{t.TempDir()}})
	if err != nil {
		t.Fatal(err)
	}
	m.available = func(string) (int64, error) { return statfsFreeBytes(stat) }
	lease, err := m.NewLease(context.Background(), httpcache.StageKind("fixture"))
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	if err := lease.Reserve(1); !errors.Is(err, ErrAdmission) {
		t.Fatalf("invalid filesystem admitted temporary copy: %v", err)
	}
	if stats := m.Stats(); stats.TemporaryBytes != 0 || stats.PeakTemporaryBytes != 0 {
		t.Fatalf("refused copy retained capacity: %+v", stats)
	}
}

func TestZeroStatfsBlockSizeRefusesAdmission(t *testing.T) {
	assertInvalidStatfsRefusesAdmission(t, unix.Statfs_t{Bavail: 1})
}

func TestStatfsFreeBytesPreservesPositiveCapacity(t *testing.T) {
	free, err := statfsFreeBytes(unix.Statfs_t{Bavail: 8, Bsize: 4096})
	if err != nil || free != 8*4096 {
		t.Fatalf("valid filesystem capacity changed: free=%d err=%v", free, err)
	}
}
