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
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/soulteary/apt-proxy/internal/resource"
)

// abortSafeHandler contains ReverseProxy's intentional copy-error panic in the
// cache's worker goroutine. Returning lets the cache close its pipe and finish
// the miss flight; its existing length check rejects incomplete fixed-size
// responses. Programming errors are never swallowed.
type abortSafeHandler struct{ handler http.Handler }

func (h *abortSafeHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if recovered := recover(); recovered != nil && recovered != http.ErrAbortHandler {
			panic(recovered)
		}
	}()
	h.handler.ServeHTTP(w, r)
}

// completeUnknownBodyTransport verifies unknown-length bodies before exposing
// their headers to the cache. A clean EOF is the only completeness signal for
// chunked/decompressed responses; spooling keeps memory bounded and makes that
// signal observable before any partial object can be cached.
type completeUnknownBodyTransport struct {
	http.RoundTripper
	tempDir   string
	maxBytes  int64
	timeout   time.Duration
	budget    *spoolBudget
	resources *resource.Manager
	initOnce  sync.Once
}

func (t *completeUnknownBodyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	t.initOnce.Do(func() {
		if t.maxBytes == 0 {
			t.maxBytes = DefaultMaxSpoolBytes
		}
		if t.timeout == 0 {
			t.timeout = DefaultDownloadTimeout
		}
		if t.budget == nil {
			t.budget = &spoolBudget{limit: DefaultTotalSpoolBytes}
		}
	})
	var admission *resource.DownloadLease
	if t.resources != nil {
		var err error
		admission, err = t.resources.AcquireDownload(r.Context())
		if err != nil {
			return nil, err
		}
	}
	ctx, cancel := context.WithTimeout(r.Context(), t.timeout)
	if admission != nil {
		context.AfterFunc(ctx, admission.Close)
	}
	res, err := t.RoundTripper.RoundTrip(r.WithContext(ctx))
	if err != nil {
		cancel()
		admission.Close()
		return nil, err
	}
	if res.StatusCode == http.StatusSwitchingProtocols {
		if body, ok := res.Body.(io.ReadWriteCloser); ok {
			upgrade := &timeoutUpgradeBody{ReadWriteCloser: body, cancel: cancel, release: admission.Close}
			// The HTTP transport transfers connection ownership after 101 and no
			// longer interrupts reads when the request context expires.
			upgrade.stop = context.AfterFunc(ctx, func() { _ = upgrade.closeBody() })
			if halfCloser, ok := body.(interface{ CloseWrite() error }); ok {
				res.Body = &timeoutUpgradeHalfCloser{timeoutUpgradeBody: upgrade, closeWriter: halfCloser}
			} else {
				res.Body = upgrade
			}
		} else {
			cancel()
			admission.Close()
		}
		return res, nil
	}
	if r.Method == http.MethodHead || res.StatusCode < 200 || res.StatusCode == 204 || res.StatusCode == 304 || res.Body == nil || res.Body == http.NoBody {
		cancel()
		admission.Close()
		return res, nil
	}
	res.Body = &timeoutBody{ReadCloser: res.Body, ctx: ctx, cancel: cancel}
	if t.resources != nil {
		res.Body = &admittedDownloadBody{ReadCloser: res.Body, lease: admission, max: t.resources.MaxObjectBytes()}
		if err := t.resources.CheckObjectSize(res.ContentLength); err != nil {
			_ = res.Body.Close()
			return nil, err
		}
	}
	if res.ContentLength >= 0 {
		return res, nil
	}
	defer cancel()
	file, err := os.CreateTemp(t.tempDir, "apt-proxy-download-*.tmp")
	if err != nil {
		_ = res.Body.Close()
		return nil, fmt.Errorf("prepare unknown-length download: %w", err)
	}
	path := file.Name()
	// Linux and macOS keep an unlinked file readable through its open descriptor.
	// Remove the name before writing any body bytes, so process exit (including
	// SIGKILL) reclaims the spool without depending on application cleanup. Do not
	// sweep a shared directory: another proxy may have an active download there.
	if err := os.Remove(path); err != nil {
		_ = file.Close()
		_ = res.Body.Close()
		return nil, fmt.Errorf("unlink unknown-length download: %w", err)
	}
	writer := &budgetWriter{writer: file, budget: t.budget, max: t.maxBytes}
	if t.resources != nil {
		writer.lease, err = t.resources.NewLease(ctx, resource.StageUnknownSpool)
		if err != nil {
			_ = file.Close()
			_ = res.Body.Close()
			return nil, err
		}
	}
	size, copyErr := io.Copy(writer, res.Body)
	closeErr := res.Body.Close()
	if copyErr == nil {
		copyErr = closeErr
	}
	if copyErr == nil {
		_, copyErr = file.Seek(0, io.SeekStart)
	}
	if copyErr != nil {
		spool := &downloadFile{File: file, path: path, unlinked: true, release: writer.release}
		cleanupErr := spool.Close()
		return nil, fmt.Errorf("incomplete unknown-length download: %w", errors.Join(copyErr, cleanupErr))
	}
	res.Body = &downloadFile{File: file, path: path, unlinked: true, release: writer.release}
	res.ContentLength = size
	res.TransferEncoding = nil
	if res.Header == nil {
		res.Header = make(http.Header)
	}
	// EOF has populated trailer values. Promote them before switching to
	// fixed-length framing, which cannot serialize HTTP/1 trailing fields.
	for name, values := range res.Trailer {
		for _, value := range values {
			res.Header.Add(name, value)
		}
	}
	res.Trailer = nil
	res.Header.Del("Trailer")
	res.Header.Set("Content-Length", fmt.Sprint(size))
	return res, nil
}

type downloadFile struct {
	*os.File
	path      string
	unlinked  bool
	release   func()
	closeOnce sync.Once
	closeErr  error
	cleanupMu sync.Mutex
}

func (f *downloadFile) Close() error {
	f.cleanupMu.Lock()
	defer f.cleanupMu.Unlock()
	f.closeOnce.Do(func() { f.closeErr = f.File.Close() })
	var removeErr error
	if !f.unlinked {
		removeErr = os.Remove(f.path)
	}
	if os.IsNotExist(removeErr) {
		removeErr = nil
	}
	// Failed deletion still consumes disk: keep it charged, allowing a later
	// Close to retry removal without closing the file descriptor again.
	if removeErr == nil && f.release != nil {
		f.release()
	}
	return errors.Join(f.closeErr, removeErr)
}
