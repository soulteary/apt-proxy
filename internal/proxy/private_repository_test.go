package proxy

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPrivateRepositoryRequestsBypassSharedCache(t *testing.T) {
	for _, header := range []string{"Authorization", "Cookie"} {
		t.Run(header, func(t *testing.T) {
			var hits atomic.Int64
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				w.Header().Set("Cache-Control", "public, max-age=3600")
				value := r.Header.Get(header)
				if value == "" {
					value = "anonymous"
				}
				io.WriteString(w, value)
			}))
			defer upstream.Close()
			server, cache := downloadTestServer(t, upstream.URL)
			get := func(identity, directive string) (int, string) {
				t.Helper()
				req, _ := http.NewRequest("GET", server.URL+"/ubuntu/pool/private.deb", nil)
				if identity != "" {
					req.Header.Set(header, identity)
				}
				if directive != "" {
					req.Header.Set("Cache-Control", directive)
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
				return res.StatusCode, string(body)
			}
			get("", "")
			deadline := time.Now().Add(2 * time.Second)
			for cache.Stats().ItemCount == 0 && time.Now().Before(deadline) {
				time.Sleep(5 * time.Millisecond)
			}
			if cache.Stats().ItemCount == 0 {
				t.Fatal("anonymous cache not warmed")
			}
			for _, identity := range []string{"alice", "bob"} {
				status, body := get(identity, "")
				if status != 200 || body != identity {
					t.Fatalf("private cache isolation: %d %q, want %q", status, body, identity)
				}
			}
			if hits.Load() != 3 {
				t.Fatalf("private requests did not bypass upstream: hits=%d", hits.Load())
			}
			before := hits.Load()
			status, _ := get("alice", "only-if-cached")
			if status != 504 || hits.Load() != before {
				t.Fatalf("private cache-only read: %d hits=%d", status, hits.Load())
			}
			status, body := get("", "")
			if status != 200 || body != "anonymous" || hits.Load() != before {
				t.Fatal("private request replaced anonymous cache")
			}
		})
	}
}

func TestSetCookieResponsesAreNotShared(t *testing.T) {
	var hits atomic.Int64
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := hits.Add(1)
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.Header().Add("Set-Cookie", fmt.Sprintf("session=%d", n))
		io.WriteString(w, fmt.Sprint(n))
	}))
	defer upstream.Close()
	server, cache := downloadTestServer(t, upstream.URL)
	for i := int64(1); i <= 2; i++ {
		res, err := server.Client().Get(server.URL + "/ubuntu/pool/session.deb")
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(res.Body)
		res.Body.Close()
		if err != nil || string(body) != fmt.Sprint(i) || res.Header.Get("Set-Cookie") != fmt.Sprintf("session=%d", i) {
			t.Fatalf("session response reused: %q %v", body, err)
		}
	}
	if cache.Stats().ItemCount != 0 || hits.Load() != 2 {
		t.Fatal("Set-Cookie response stored in shared cache")
	}
}

func TestConcurrentPrivateRepositoryRequestsStaySeparate(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=3600")
		io.WriteString(w, r.Header.Get("Authorization"))
	}))
	defer upstream.Close()
	server, cache := downloadTestServer(t, upstream.URL)
	errors := make(chan error, 8)
	var workers sync.WaitGroup
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			identity := fmt.Sprintf("Bearer user-%d", i)
			req, _ := http.NewRequest("GET", server.URL+"/ubuntu/pool/concurrent-private.deb", nil)
			req.Header.Set("Authorization", identity)
			res, err := server.Client().Do(req)
			if err == nil {
				body, readErr := io.ReadAll(res.Body)
				res.Body.Close()
				err = readErr
				if err == nil && string(body) != identity {
					err = fmt.Errorf("cross-user body: %q, want %q", body, identity)
				}
			}
			errors <- err
		}(i)
	}
	workers.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Error(err)
		}
	}
	if cache.Stats().ItemCount != 0 {
		t.Fatal("concurrent private responses stored")
	}
}

func TestRepositoryURLCredentialsBecomePrivateHeaders(t *testing.T) {
	for _, explicit := range []string{"", "Bearer explicit"} {
		req := httptest.NewRequest("GET", "http://user:password@mirror.example/package.deb", nil)
		if explicit != "" {
			req.Header.Set("Authorization", explicit)
		}
		normalizeRepositoryCredentials(req)
		if req.URL.User != nil || !privateRepositoryRequest(req) {
			t.Fatal("URL credentials remained in shared cache key")
		}
		if explicit != "" {
			if req.Header.Get("Authorization") != explicit {
				t.Fatal("explicit Authorization replaced")
			}
		} else {
			user, password, ok := req.BasicAuth()
			if !ok || user != "user" || password != "password" {
				t.Fatal("URL credentials not forwarded as Basic authentication")
			}
		}
	}
}
