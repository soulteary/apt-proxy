package resource

import (
	"context"
	"errors"
	"math"
	"testing"
)

func TestInvalidLimitsAndCanceledStaging(t *testing.T) {
	for _, cfg := range []Config{
		{}, {MaxObjectBytes: 1, TotalTempBytes: 1, MaxActiveDownloads: 1, MinFreeBytes: -1},
		{MaxObjectBytes: 1, TotalTempBytes: 1, MaxActiveDownloads: 1, MinFreeBytes: 1},
	} {
		if _, err := New(cfg); err == nil {
			t.Fatalf("invalid limits accepted: %+v", cfg)
		}
	}
	m := testManager(t, 64, 1)
	if m.MaxObjectBytes() != 64 {
		t.Fatal("object cap changed")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.NewLease(ctx, StageUnknownSpool); !errors.Is(err, context.Canceled) {
		t.Fatalf("lease error=%v", err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	l, err := m.NewLease(ctx, StageUnknownSpool)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	cancel()
	if err := l.Reserve(1); !errors.Is(err, context.Canceled) {
		t.Fatalf("reserve error=%v", err)
	}
	var download *DownloadLease
	download.Close()
	if saturatingFree(7, 0) != 0 || saturatingFree(2, 4) != 8 || saturatingFree(math.MaxUint64, 2) != math.MaxInt64 {
		t.Fatal("incorrect saturation")
	}
}

func TestLeaseRefundValidationAndStatsIsolation(t *testing.T) {
	m := testManager(t, 64, 1)
	lease, _ := m.NewLease(context.Background(), StageUnknownSpool)
	defer lease.Close()
	if err := lease.Reserve(0); err != nil {
		t.Fatal(err)
	}
	if err := lease.Reserve(-1); !errors.Is(err, ErrAdmission) {
		t.Fatal(err)
	}
	if err := lease.Reserve(8); err != nil {
		t.Fatal(err)
	}
	stats := m.Stats()
	stats.ByKind[StageUnknownSpool] = 999
	if m.Stats().ByKind[StageUnknownSpool] != 8 {
		t.Fatal("stats exposes live ledger")
	}
	for _, n := range []int64{-1, 9} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("invalid refund %d did not panic", n)
				}
			}()
			lease.Release(n)
		}()
	}
	lease.Release(8)
	if _, ok := m.Stats().ByKind[StageUnknownSpool]; ok {
		t.Fatal("refunded stage still counted")
	}
}
