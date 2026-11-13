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
	"net/http"

	httpcache "github.com/soulteary/httpcache-kit/v4"
	logger "github.com/soulteary/logger-kit/v3"

	"github.com/soulteary/apt-proxy/internal/distro"
	"github.com/soulteary/apt-proxy/internal/logsafe"
)

// immutableFallbackTransport answers a failed conditional request for an
// immutable artifact with 304 Not Modified.
//
// The cache has no stale-if-error: when revalidation of an expired entry
// fails, the upstream failure is delivered although the body is on disk. A 304
// without validator fields is accepted by the cache, which merges it into the
// stored headers and freshens the entry. The cache sends If-None-Match or
// If-Modified-Since only for an entry stored with ETag or Last-Modified; an
// entry without them is refetched unconditionally and still fails with the
// upstream. A client that sent its own conditional request receives the 304.
type immutableFallbackTransport struct {
	http.RoundTripper
	log *logger.Logger
}

func (t *immutableFallbackTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	res, err := t.RoundTripper.RoundTrip(req)
	if err == nil && res.StatusCode < http.StatusInternalServerError {
		return res, nil
	}
	// A canceled or expired request says nothing about the upstream.
	if req.Context().Err() != nil || !immutableRevalidation(req) {
		return res, err
	}
	event := t.log.Warn().Err(logsafe.Error(err)).Str("url", logsafe.URL(req.URL.String()))
	if res != nil {
		event = event.Int("status", res.StatusCode)
		if res.Body != nil {
			_ = res.Body.Close()
		}
	}
	event.Msg("upstream request failed, immutable artifact reported as not modified")
	return &http.Response{
		Status:        "304 Not Modified",
		StatusCode:    http.StatusNotModified,
		Proto:         "HTTP/1.1",
		ProtoMajor:    1,
		ProtoMinor:    1,
		Header:        http.Header{"Date": {httpcache.Clock().UTC().Format(http.TimeFormat)}},
		Body:          http.NoBody,
		ContentLength: 0,
		Request:       req,
	}, nil
}

// immutableRevalidation reports whether req is a conditional GET or HEAD for
// a path matched by an immutable rule. Range and If-Range requests are
// excluded: a client resuming a download needs bytes, not a 304.
func immutableRevalidation(req *http.Request) bool {
	if req.Method != http.MethodGet && req.Method != http.MethodHead {
		return false
	}
	h := req.Header
	if h.Get("If-None-Match") == "" && h.Get("If-Modified-Since") == "" {
		return false
	}
	if h.Get("Range") != "" || h.Get("If-Range") != "" {
		return false
	}
	rule, _ := req.Context().Value(cacheRuleKey{}).(*distro.Rule)
	return immutableRule(rule)
}
