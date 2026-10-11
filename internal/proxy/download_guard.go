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
	"fmt"
	"io"
	"net/http"
	"os"
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
	tempDir string
}

func (t *completeUnknownBodyTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	res, err := t.RoundTripper.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	if r.Method == http.MethodHead || res.StatusCode < 200 || res.StatusCode == 204 || res.StatusCode == 304 || res.Body == nil || res.Body == http.NoBody || res.ContentLength >= 0 {
		return res, nil
	}
	file, err := os.CreateTemp(t.tempDir, "apt-proxy-download-*.tmp")
	if err != nil {
		_ = res.Body.Close()
		return nil, fmt.Errorf("prepare unknown-length download: %w", err)
	}
	path := file.Name()
	size, copyErr := io.Copy(file, res.Body)
	closeErr := res.Body.Close()
	if copyErr == nil {
		copyErr = closeErr
	}
	if copyErr == nil {
		_, copyErr = file.Seek(0, io.SeekStart)
	}
	if copyErr != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, fmt.Errorf("incomplete unknown-length download: %w", copyErr)
	}
	res.Body = &downloadFile{File: file, path: path}
	res.ContentLength = size
	res.TransferEncoding = nil
	if res.Header == nil {
		res.Header = make(http.Header)
	}
	res.Header.Set("Content-Length", fmt.Sprint(size))
	return res, nil
}

type downloadFile struct {
	*os.File
	path string
}

func (f *downloadFile) Close() error {
	err := f.File.Close()
	removeErr := os.Remove(f.path)
	if err != nil {
		return err
	}
	return removeErr
}
