package sharedcache

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/soulteary/apt-proxy/internal/resource"
)

type limitTransport func(*http.Request) (*http.Response, error)

func (f limitTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type countedBody struct {
	io.Reader
	reads  int
	closed bool
}

func (b *countedBody) Read(p []byte) (int, error) { b.reads++; return b.Reader.Read(p) }
func (b *countedBody) Close() error               { b.closed = true; return nil }

func TestByHashVerifierHonorsSmallerGlobalObjectLimit(t *testing.T) {
	body := []byte("index exceeding global cap")
	for _, known := range []bool{true, false} {
		t.Run(fmt.Sprintf("known=%v", known), func(t *testing.T) {
			dir := t.TempDir()
			manager, err := resource.New(resource.Config{MaxObjectBytes: 8, TotalTempBytes: 1024, MaxActiveDownloads: 1})
			if err != nil {
				t.Fatal(err)
			}
			policy, err := New(&Config{Repositories: []Repository{{ID: "public", Bases: []string{"https://mirror.example/ubuntu/"}}}})
			if err != nil {
				t.Fatal(err)
			}
			upstream := &countedBody{Reader: bytes.NewReader(body)}
			n := int64(-1)
			if known {
				n = int64(len(body))
			}
			transport := limitTransport(func(r *http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 200, Header: make(http.Header), ContentLength: n, Body: upstream, Request: r}, nil
			})
			req := httptest.NewRequest("GET", fmt.Sprintf("https://mirror.example/ubuntu/dists/test/main/binary-amd64/by-hash/SHA256/%x", sha256.Sum256(body)), nil)
			res, err := policy.WrapTransportWithBudget(transport, dir, manager).RoundTrip(req)
			if res != nil {
				_ = res.Body.Close()
			}
			if err == nil || res != nil {
				t.Fatalf("oversized object accepted: res=%v err=%v", res, err)
			}
			if !upstream.closed {
				t.Fatal("refused upstream body was not closed")
			}
			if known && upstream.reads != 0 {
				t.Fatal("known oversized object read before refusal")
			}
			if stats := manager.Stats(); stats.TemporaryBytes != 0 || stats.PeakTemporaryBytes > 9 {
				t.Fatalf("verification exceeded cap or retained bytes: %+v", stats)
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 0 {
				t.Fatalf("verification left temporary names: %v, %v", entries, err)
			}
		})
	}
}

func TestRepositoryIDCannotInjectFingerprintDelimiter(t *testing.T) {
	// Before validation, this ID and single base produced the same fingerprint
	// as ID "public" with both bases, crossing different operator policies.
	bad := &Config{Repositories: []Repository{{ID: "public\x00https://a.example/ubuntu/", Bases: []string{"https://b.example/ubuntu/"}}}}
	if _, err := New(bad); err == nil {
		t.Fatal("repository ID accepted a fingerprint delimiter")
	}
	if _, err := New(&Config{Repositories: []Repository{{ID: "public", Bases: []string{"https://a.example/ubuntu/", "https://b.example/ubuntu/"}}}}); err != nil {
		t.Fatal(err)
	}
}
