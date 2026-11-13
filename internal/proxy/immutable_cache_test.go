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
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	httpcache "github.com/soulteary/httpcache-kit/v4"
	logger "github.com/soulteary/logger-kit/v3"

	"github.com/soulteary/apt-proxy/internal/distro"
	"github.com/soulteary/apt-proxy/internal/storage"
)

const (
	immutableTestPackage  = "/ubuntu/pool/main/d/demo/demo_1.0_amd64.deb"
	immutableTestIndex    = "/ubuntu/dists/jammy/main/binary-amd64/by-hash/SHA256/"
	mutableTestIndex      = "/ubuntu/dists/jammy/InRelease"
	immutableTestModified = "Wed, 01 Jan 2020 00:00:00 GMT"
)

// failingOriginTestServer is downloadTestServer without the retry layer, whose
// backoff would only delay tests that make the origin fail on purpose.
func failingOriginTestServer(t *testing.T, upstream string) (*httptest.Server, httpcache.ExtendedCache) {
	t.Helper()
	st := newTestState()
	st.SetMirror(distro.TypeUbuntu, upstream+"/ubuntu/")
	ps, err := NewPackageStruct(Options{
		State: st, Registry: newTestRegistry(), Mode: distro.TypeUbuntu, Logger: logger.Default(),
		TransportOverride: NewUpstreamTransport(false),
	})
	if err != nil {
		t.Fatal(err)
	}
	backend, err := httpcache.NewDiskCacheWithConfig(t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	cached := httpcache.NewHandlerWithOptions(&storage.VerifiedCache{ExtendedCache: backend}, ps.Handler, &httpcache.HandlerOptions{Logger: logger.Default()})
	cached.Shared = true
	ps.Handler = cached
	server := httptest.NewServer(ps)
	t.Cleanup(func() {
		server.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := cached.Shutdown(ctx); err != nil {
			t.Error(err)
		}
		if err := backend.Close(); err != nil {
			t.Error(err)
		}
	})
	return server, backend
}

func immutableTestGet(t *testing.T, server *httptest.Server, path string, header http.Header) (int, string, http.Header) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, server.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	for name, values := range header {
		req.Header[name] = values
	}
	res, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return res.StatusCode, string(body), res.Header
}

// APT sends max-age=0 with every index request, by-hash ones included. For a
// content-addressed path that hint must not keep the response out of the cache.
func TestByHashIndexIsCachedForAPT(t *testing.T) {
	var hits atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get("Cache-Control") != "" {
			t.Errorf("the revalidation hint reached the origin: %v", r.Header)
		}
		_, _ = io.WriteString(w, "index")
	}))
	defer upstream.Close()
	server, cache := downloadTestServer(t, upstream.URL)
	path := immutableTestIndex + strings.Repeat("a", 64)
	for _, want := range []string{"MISS", "HIT"} {
		status, body, header := immutableTestGet(t, server, path, http.Header{"Cache-Control": {"max-age=0"}})
		if status != http.StatusOK || body != "index" || header.Get("X-Cache") != want ||
			header.Get("Cache-Control") != "max-age=100000, immutable" {
			t.Fatalf("%s request: status=%d body=%q header=%v", want, status, body, header)
		}
		waitForReviewCache(t, func() int { return cache.Stats().ItemCount })
	}
	if hits.Load() != 1 {
		t.Fatalf("origin requests = %d, want 1", hits.Load())
	}
}

// An explicit no-cache, which APT sends with Acquire::http::No-Cache, still
// fetches from the origin: it is the client's way around a bad stored copy.
func TestExplicitNoCacheBypassesImmutableEntry(t *testing.T) {
	var hits atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = io.WriteString(w, "index")
	}))
	defer upstream.Close()
	server, cache := downloadTestServer(t, upstream.URL)
	path := immutableTestIndex + strings.Repeat("c", 64)
	if status, _, header := immutableTestGet(t, server, path, nil); status != http.StatusOK || header.Get("X-Cache") != "MISS" {
		t.Fatalf("warming request: status=%d header=%v", status, header)
	}
	waitForReviewCache(t, func() int { return cache.Stats().ItemCount })
	explicit := http.Header{"Cache-Control": {"no-cache"}, "Pragma": {"no-cache"}}
	status, body, header := immutableTestGet(t, server, path, explicit)
	if status != http.StatusOK || body != "index" || header.Get("X-Cache") != "SKIP" || hits.Load() != 2 {
		t.Fatalf("no-cache request: status=%d body=%q X-Cache=%q origin requests=%d", status, body, header.Get("X-Cache"), hits.Load())
	}
	// The stored entry is untouched and keeps serving ordinary requests.
	if status, _, header := immutableTestGet(t, server, path, nil); status != http.StatusOK || header.Get("X-Cache") != "HIT" || hits.Load() != 2 {
		t.Fatalf("request after the bypass: status=%d X-Cache=%q origin requests=%d", status, header.Get("X-Cache"), hits.Load())
	}
}

func TestImmutableCacheOnlyRequestStaysCacheOnly(t *testing.T) {
	var hits atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		_, _ = io.WriteString(w, "index")
	}))
	defer upstream.Close()
	server, cache := downloadTestServer(t, upstream.URL)
	path := immutableTestIndex + strings.Repeat("b", 64)
	cacheOnly := http.Header{"Cache-Control": {"max-age=0, only-if-cached"}}
	if status, _, _ := immutableTestGet(t, server, path, cacheOnly); status != http.StatusGatewayTimeout || hits.Load() != 0 {
		t.Fatalf("cache-only miss: status=%d origin requests=%d", status, hits.Load())
	}
	if status, _, _ := immutableTestGet(t, server, path, nil); status != http.StatusOK {
		t.Fatalf("fill: status=%d", status)
	}
	waitForReviewCache(t, func() int { return cache.Stats().ItemCount })
	status, body, header := immutableTestGet(t, server, path, cacheOnly)
	if status != http.StatusOK || body != "index" || header.Get("X-Cache") != "HIT" || hits.Load() != 1 {
		t.Fatalf("cache-only hit: status=%d body=%q cache=%s origin requests=%d", status, body, header.Get("X-Cache"), hits.Load())
	}
}

func TestExpiredEntryDuringUpstreamFailure(t *testing.T) {
	for _, tc := range []struct {
		name      string
		path      string
		validator bool
		served    bool
	}{
		{"immutable package", immutableTestPackage, true, true},
		{"mutable index", mutableTestIndex, true, false},
		// The cache refetches an entry without validators unconditionally.
		{"immutable package without validators", immutableTestPackage, false, false},
	} {
		for _, unreachable := range []bool{false, true} {
			failure, wantFailure := "status 503", http.StatusServiceUnavailable
			if unreachable {
				failure, wantFailure = "unreachable", http.StatusBadGateway
			}
			t.Run(tc.name+"/"+failure, func(t *testing.T) {
				var failing atomic.Bool
				var conditional, unconditional atomic.Int64
				upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if failing.Load() {
						if r.Header.Get("If-Modified-Since") != "" {
							conditional.Add(1)
						} else {
							unconditional.Add(1)
						}
						http.Error(w, "mirror unavailable", http.StatusServiceUnavailable)
						return
					}
					// Age beyond max-age: the entry is expired as soon as it is
					// stored, without waiting or replacing the cache clock.
					w.Header().Set("Cache-Control", "max-age=60")
					w.Header().Set("Age", "3600")
					if tc.validator {
						w.Header().Set("Last-Modified", immutableTestModified)
					}
					_, _ = io.WriteString(w, "payload")
				}))
				defer upstream.Close()
				server, cache := failingOriginTestServer(t, upstream.URL)
				if status, body, _ := immutableTestGet(t, server, tc.path, nil); status != http.StatusOK || body != "payload" {
					t.Fatalf("fill: status=%d body=%q", status, body)
				}
				waitForReviewCache(t, func() int { return cache.Stats().ItemCount })
				failing.Store(true)
				if unreachable {
					upstream.Close()
				}
				wantConditional, wantUnconditional := int64(0), int64(0)
				if !unreachable {
					if tc.validator {
						wantConditional = 1
					} else {
						wantUnconditional = 1
					}
				}
				status, body, header := immutableTestGet(t, server, tc.path, nil)
				if conditional.Load() != wantConditional || unconditional.Load() != wantUnconditional {
					t.Fatalf("origin requests: conditional=%d unconditional=%d", conditional.Load(), unconditional.Load())
				}
				if !tc.served {
					if status != wantFailure || body == "payload" {
						t.Fatalf("upstream failure hidden: status=%d body=%q", status, body)
					}
					return
				}
				if status != http.StatusOK || body != "payload" || header.Get("X-Cache") != "HIT" {
					t.Fatalf("expired entry not served: status=%d body=%q header=%v", status, body, header)
				}
				// The accepted 304 starts a new freshness lifetime for the entry.
				status, body, header = immutableTestGet(t, server, tc.path, nil)
				if status != http.StatusOK || body != "payload" || header.Get("X-Cache") != "HIT" || conditional.Load() != wantConditional {
					t.Fatalf("freshened entry: status=%d body=%q cache=%s conditional=%d", status, body, header.Get("X-Cache"), conditional.Load())
				}
			})
		}
	}
}

// A client that revalidates its own copy of an immutable artifact is told it
// is current. The same request for mutable metadata still fails.
func TestClientRevalidationDuringUpstreamFailure(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "mirror unavailable", http.StatusServiceUnavailable)
	}))
	defer upstream.Close()
	server, cache := failingOriginTestServer(t, upstream.URL)
	conditional := http.Header{"If-Modified-Since": {immutableTestModified}}
	if status, body, _ := immutableTestGet(t, server, immutableTestPackage, conditional); status != http.StatusNotModified || body != "" {
		t.Errorf("immutable artifact: status=%d body=%q", status, body)
	}
	if status, _, _ := immutableTestGet(t, server, mutableTestIndex, conditional); status != http.StatusServiceUnavailable {
		t.Errorf("mutable index: status=%d", status)
	}
	if status, _, _ := immutableTestGet(t, server, immutableTestPackage, nil); status != http.StatusServiceUnavailable {
		t.Errorf("unconditional request: status=%d", status)
	}
	if cache.Stats().ItemCount != 0 {
		t.Errorf("failed upstream responses stored: %d", cache.Stats().ItemCount)
	}
}
