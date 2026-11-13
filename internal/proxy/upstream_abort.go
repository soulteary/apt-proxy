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
	"io"
	"net/http"
	"sync/atomic"
)

// upstreamAbort records that the upstream response of one client request
// ended before it was complete. The reverse proxy runs in the cache's worker
// goroutine, so the failure is carried back through the request context to
// the goroutine that owns the client connection.
type upstreamAbort struct {
	failed atomic.Bool
	// serving reports that the request came through net/http's server, which
	// recovers http.ErrAbortHandler and drops the connection.
	serving bool
}

type upstreamAbortKey struct{}

func trackUpstreamAbort(ctx context.Context) (context.Context, *upstreamAbort) {
	abort := &upstreamAbort{serving: ctx.Value(http.ServerContextKey) != nil}
	return context.WithValue(ctx, upstreamAbortKey{}, abort), abort
}

func markUpstreamAborted(ctx context.Context) {
	if abort, ok := ctx.Value(upstreamAbortKey{}).(*upstreamAbort); ok {
		abort.failed.Store(true)
	}
}

// propagate aborts the client response when its upstream body was cut short.
// The cache can re-frame a fixed-length response as chunked to carry a
// trailer; returning normally would then end the response cleanly and hide
// the truncation from the client.
func (a *upstreamAbort) propagate() {
	if a.serving && a.failed.Load() {
		panic(http.ErrAbortHandler)
	}
}

// abortTrackingBody marks the request when the upstream body fails before EOF.
type abortTrackingBody struct {
	io.ReadCloser
	ctx context.Context
}

func (b *abortTrackingBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if err != nil && err != io.EOF {
		markUpstreamAborted(b.ctx)
	}
	return n, err
}

// trackUpstreamBody wraps a response body that will be copied to the client.
// Protocol upgrades keep their body: the reverse proxy needs its write side.
func trackUpstreamBody(resp *http.Response) {
	if resp.StatusCode == http.StatusSwitchingProtocols || resp.Body == nil || resp.Body == http.NoBody || resp.Request == nil {
		return
	}
	resp.Body = &abortTrackingBody{ReadCloser: resp.Body, ctx: resp.Request.Context()}
}

// validUpstreamStatus reports whether a status code can be written to an
// http.ResponseWriter. net/http panics outside this range, and the Go client
// accepts any three-digit code an upstream sends.
func validUpstreamStatus(code int) bool {
	return code >= 100 && code <= 999
}
