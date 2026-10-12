// Package resource coordinates bounded per-process download and temporary-copy
// admission. It never waits for capacity held by another copy of a response.
package resource

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"sync"

	httpcache "github.com/soulteary/httpcache-kit/v4"
)

const (
	DefaultMaxObjectBytes     int64               = 1 << 30
	DefaultTotalTempBytes     int64               = 4 << 30
	DefaultMaxActiveDownloads                     = 32
	DefaultMinFreeBytes       int64               = 128 << 20
	StageUnknownSpool         httpcache.StageKind = "unknown-spool"
	StageS3Inline             httpcache.StageKind = "s3-inline"
	StageS3Spill              httpcache.StageKind = "s3-spill"
)

var (
	ErrAdmission      = errors.New("temporary resource admission refused")
	ErrObjectTooLarge = errors.New("upstream object exceeds configured limit")
)

type Config struct {
	MaxObjectBytes, TotalTempBytes int64
	MaxActiveDownloads             int
	MinFreeBytes                   int64
	// DiskDirs includes all temporary/cache staging volumes. Paths may not yet
	// exist at construction; admission examines the nearest existing ancestor.
	DiskDirs []string
}

type Manager struct {
	mu         sync.Mutex
	cfg        Config
	used, peak int64
	active     int
	kinds      map[httpcache.StageKind]int64
	available  func(string) (int64, error)
}

type Stats struct {
	TemporaryBytes, PeakTemporaryBytes int64
	ActiveDownloads                    int
	ByKind                             map[httpcache.StageKind]int64
}

func New(cfg Config) (*Manager, error) {
	if cfg.MaxObjectBytes <= 0 || cfg.TotalTempBytes <= 0 || cfg.MaxActiveDownloads <= 0 || cfg.MinFreeBytes < 0 {
		return nil, errors.New("resource limits must be positive and headroom nonnegative")
	}
	cfg.DiskDirs = append([]string(nil), cfg.DiskDirs...)
	for i, dir := range cfg.DiskDirs {
		p, err := filepath.Abs(dir)
		if err != nil {
			return nil, err
		}
		cfg.DiskDirs[i] = p
	}
	if cfg.MinFreeBytes > 0 && len(cfg.DiskDirs) == 0 {
		return nil, errors.New("disk headroom requires a staging directory")
	}
	return &Manager{cfg: cfg, kinds: make(map[httpcache.StageKind]int64), available: availableBytes}, nil
}
func (m *Manager) MaxObjectBytes() int64 { return m.cfg.MaxObjectBytes }
func (m *Manager) Stats() Stats {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := Stats{TemporaryBytes: m.used, PeakTemporaryBytes: m.peak, ActiveDownloads: m.active, ByKind: make(map[httpcache.StageKind]int64, len(m.kinds))}
	for k, v := range m.kinds {
		s.ByKind[k] = v
	}
	return s
}
func (m *Manager) NewLease(ctx context.Context, kind httpcache.StageKind) (httpcache.StagingLease, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &Lease{manager: m, ctx: ctx, kind: kind}, nil
}

type Lease struct {
	manager *Manager
	ctx     context.Context
	kind    httpcache.StageKind
	held    int64
	closed  bool
}

func (l *Lease) Reserve(n int64) error {
	m := l.manager
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := l.ctx.Err(); err != nil {
		return err
	}
	if l.closed || n < 0 || n > m.cfg.TotalTempBytes-m.used {
		return fmt.Errorf("%w: temporary byte limit", ErrAdmission)
	}
	if n == 0 {
		return nil
	}
	if l.kind != StageS3Inline && m.cfg.MinFreeBytes > 0 {
		for _, dir := range m.cfg.DiskDirs {
			free, err := m.available(dir)
			if err != nil {
				return fmt.Errorf("%w: inspect staging disk: %w", ErrAdmission, err)
			}
			// Count held reservations conservatively as well as current available
			// space, so concurrent reserves cannot all spend the same free bytes.
			if free < m.cfg.MinFreeBytes || m.used > free-m.cfg.MinFreeBytes || n > free-m.cfg.MinFreeBytes-m.used {
				return fmt.Errorf("%w: staging disk headroom", ErrAdmission)
			}
		}
	}
	m.used += n
	l.held += n
	m.kinds[l.kind] += n
	if m.used > m.peak {
		m.peak = m.used
	}
	return nil
}
func (l *Lease) Release(n int64) {
	m := l.manager
	m.mu.Lock()
	defer m.mu.Unlock()
	if n < 0 || n > l.held {
		panic("resource: invalid lease refund")
	}
	l.held -= n
	m.used -= n
	m.kinds[l.kind] -= n
	if m.kinds[l.kind] == 0 {
		delete(m.kinds, l.kind)
	}
}
func (l *Lease) Close() {
	m := l.manager
	m.mu.Lock()
	defer m.mu.Unlock()
	if l.closed {
		return
	}
	l.closed = true
	m.used -= l.held
	m.kinds[l.kind] -= l.held
	if m.kinds[l.kind] == 0 {
		delete(m.kinds, l.kind)
	}
	l.held = 0
}

type DownloadLease struct {
	manager *Manager
	once    sync.Once
}

func (m *Manager) AcquireDownload(ctx context.Context) (*DownloadLease, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active >= m.cfg.MaxActiveDownloads {
		return nil, fmt.Errorf("%w: active download limit", ErrAdmission)
	}
	m.active++
	return &DownloadLease{manager: m}, nil
}
func (l *DownloadLease) Close() {
	if l == nil {
		return
	}
	l.once.Do(func() { m := l.manager; m.mu.Lock(); m.active--; m.mu.Unlock() })
}
func (m *Manager) CheckObjectSize(n int64) error {
	if n > m.cfg.MaxObjectBytes {
		return fmt.Errorf("%w: %d bytes (maximum %d)", ErrObjectTooLarge, n, m.cfg.MaxObjectBytes)
	}
	return nil
}

func saturatingFree(blocks, size uint64) int64 {
	if size == 0 {
		return 0
	}
	if blocks > math.MaxInt64/size {
		return math.MaxInt64
	}
	// The division guard proves this product is at most MaxInt64.
	return int64(blocks * size) // #nosec G115 -- multiplication bounded above before conversion.
}

var _ httpcache.StagingBudget = (*Manager)(nil)
