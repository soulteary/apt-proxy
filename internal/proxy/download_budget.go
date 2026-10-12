// Copyright 2022 Su Yang
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package proxy

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	httpcache "github.com/soulteary/httpcache-kit/v4"
)

const (
	DefaultMaxSpoolBytes   int64 = 1 << 30
	DefaultTotalSpoolBytes int64 = 4 << 30
	DefaultDownloadTimeout       = 20 * time.Minute
)

type spoolBudget struct {
	mu          sync.Mutex
	used, limit int64
}

func (b *spoolBudget) reserve(n int64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if n > b.limit-b.used {
		return false
	}
	b.used += n
	return true
}
func (b *spoolBudget) release(n int64) { b.mu.Lock(); defer b.mu.Unlock(); b.used -= n }

type budgetWriter struct {
	writer    io.Writer
	budget    *spoolBudget
	lease     httpcache.StagingLease
	max, used int64
	once      sync.Once
}

func (w *budgetWriter) Write(p []byte) (int, error) {
	size := int64(len(p))
	if size > w.max-w.used {
		return 0, fmt.Errorf("unknown-length response exceeds spool limit")
	}
	if !w.budget.reserve(size) {
		return 0, fmt.Errorf("unknown-length downloads exceed shared spool budget")
	}
	if w.lease != nil {
		if err := w.lease.Reserve(size); err != nil {
			w.budget.release(size)
			return 0, err
		}
	}
	n, err := w.writer.Write(p)
	w.used += int64(n)
	w.budget.release(size - int64(n))
	if w.lease != nil {
		w.lease.Release(size - int64(n))
	}
	return n, err
}
func (w *budgetWriter) release() {
	w.once.Do(func() {
		w.budget.release(w.used)
		if w.lease != nil {
			w.lease.Close()
		}
	})
}

type timeoutBody struct {
	io.ReadCloser
	ctx    context.Context
	cancel context.CancelFunc
}

func (b *timeoutBody) Read(p []byte) (int, error) {
	if err := b.ctx.Err(); err != nil {
		return 0, err
	}
	return b.ReadCloser.Read(p)
}
func (b *timeoutBody) Close() error { b.cancel(); return b.ReadCloser.Close() }

// Keep the bidirectional interface required by ReverseProxy's upgrade path.
type timeoutUpgradeBody struct {
	io.ReadWriteCloser
	cancel   context.CancelFunc
	stop     func() bool
	release  func()
	once     sync.Once
	closeErr error
}

func (b *timeoutUpgradeBody) closeBody() error {
	b.once.Do(func() {
		b.closeErr = b.ReadWriteCloser.Close()
		if b.release != nil {
			b.release()
		}
	})
	return b.closeErr
}
func (b *timeoutUpgradeBody) Close() error {
	b.stop()
	b.cancel()
	return b.closeBody()
}

// Advertise half-close only when the wrapped upgrade body supports it.
type timeoutUpgradeHalfCloser struct {
	*timeoutUpgradeBody
	closeWriter interface{ CloseWrite() error }
}

func (b *timeoutUpgradeHalfCloser) CloseWrite() error { return b.closeWriter.CloseWrite() }
