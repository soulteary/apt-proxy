package sharedcache

import (
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMappedUnverifiedStatusesCannotPublishSharedResponses(t *testing.T) {
	policy, err := New(&Config{Repositories: []Repository{{ID: "public", Bases: []string{"https://mirror.example/ubuntu/"}}}})
	if err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("https://mirror.example/ubuntu/dists/test/main/binary-amd64/by-hash/SHA256/%x", sha256.Sum256([]byte("valid index")))
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		for _, status := range []int{204, 206, 301, 302, 307, 308, 404, 410, 503} {
			t.Run(fmt.Sprintf("%s/%d", method, status), func(t *testing.T) {
				for _, nilHeader := range []bool{false, true} {
					upstream := &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader("unverified upstream representation"))}
					if !nilHeader {
						upstream.Header = http.Header{"Cache-Control": []string{"public, max-age=3600"}, "Expires": []string{"Mon, 12 Oct 2037 00:00:00 GMT"}}
					}
					transport := limitTransport(func(*http.Request) (*http.Response, error) { return upstream, nil })
					res, err := policy.WrapTransport(transport, t.TempDir()).RoundTrip(httptest.NewRequest(method, path, nil))
					if err != nil {
						t.Fatal(err)
					}
					data, readErr := io.ReadAll(res.Body)
					_ = res.Body.Close()
					if readErr != nil || res.StatusCode != status || string(data) != "unverified upstream representation" || res.Header.Get("Cache-Control") != "no-store" {
						t.Fatalf("unverified response changed or cacheable: status=%d body=%q cc=%q err=%v", res.StatusCode, data, res.Header.Get("Cache-Control"), readErr)
					}
				}
			})
		}
	}
}

func TestMappedBodyless304PreservesFresheningMetadata(t *testing.T) {
	policy, err := New(&Config{Repositories: []Repository{{ID: "public", Bases: []string{"https://mirror.example/ubuntu/"}}}})
	if err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("https://mirror.example/ubuntu/dists/test/main/binary-amd64/by-hash/SHA256/%x", sha256.Sum256([]byte("valid index")))
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		for _, body := range []io.ReadCloser{nil, http.NoBody, io.NopCloser(strings.NewReader(""))} {
			res, err := policy.WrapTransport(limitTransport(func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: 304, ContentLength: 1024, Header: http.Header{"Cache-Control": []string{"public, max-age=3600"}, "Etag": []string{`"v1"`}}, Body: body}, nil
			}), t.TempDir()).RoundTrip(httptest.NewRequest(method, path, nil))
			if err != nil {
				t.Fatal(err)
			}
			if res.Body != nil {
				_ = res.Body.Close()
			}
			if res.StatusCode != 304 || res.ContentLength != 1024 || res.Header.Get("Cache-Control") != "public, max-age=3600" || res.Header.Get("ETag") != `"v1"` {
				t.Fatalf("valid304 metadata changed: %+v", res)
			}
		}
	}
}

func TestMappedHead200PreservesMetadataWithoutSpooling(t *testing.T) {
	policy, err := New(&Config{Repositories: []Repository{{ID: "public", Bases: []string{"https://mirror.example/ubuntu/"}}}})
	if err != nil {
		t.Fatal(err)
	}
	path := fmt.Sprintf("https://mirror.example/ubuntu/dists/test/main/binary-amd64/by-hash/SHA256/%x", sha256.Sum256([]byte("valid index")))
	transport := limitTransport(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Accept-Encoding") != "gzip" {
			t.Fatal("HEAD wire metadata request changed")
		}
		return &http.Response{StatusCode: 200, ContentLength: 1024, Header: http.Header{"Cache-Control": []string{"public, max-age=3600"}, "Content-Encoding": []string{"gzip"}}, Body: http.NoBody}, nil
	})
	req := httptest.NewRequest("HEAD", path, nil)
	req.Header.Set("Accept-Encoding", "gzip")
	res, err := policy.WrapTransportWithBudget(transport, t.TempDir(), &testBudget{limit: 0}).RoundTrip(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != 200 || res.ContentLength != 1024 || res.Header.Get("Cache-Control") != "public, max-age=3600" || res.Header.Get("Content-Encoding") != "gzip" {
		t.Fatalf("HEAD200 metadata changed: %+v", res)
	}
}
