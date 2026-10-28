package metalink

import (
	"context"
	"crypto/md5"
	"crypto/sha1"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/soulteary/apt-proxy/internal/access"
	"github.com/soulteary/apt-proxy/internal/resource"
)

func edgeController(t *testing.T) *Controller {
	t.Helper()
	c, e := New(&Config{PublicURL: "http://proxy.example", AllowedMirrorHosts: []string{"mirror.example", "second.example"}}, nil)
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func TestMetadataEndpointSuccessFailureAndExpiry(t *testing.T) {
	c := edgeController(t)
	const content = "repomd"
	c.transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := content
		if r.URL.Hostname() == "mirrors.centos.org" {
			body = document(content, resourceURL("http://mirror.example/repo", 100))
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body))}, nil
	})
	for _, method := range []string{"GET", "HEAD"} {
		w := httptest.NewRecorder()
		if !c.Handle(w, httptest.NewRequest(method, "/metalink?repo=r&arch=x", nil), nil) || w.Code != 200 {
			t.Fatalf("endpoint status %d", w.Code)
		}
		if method == "GET" && !strings.Contains(w.Body.String(), "/metalink/repos/") {
			t.Fatal("no pinned metadata URL")
		}
		if method == "HEAD" && w.Body.Len() != 0 {
			t.Fatal("HEAD body")
		}
	}
	w := httptest.NewRecorder()
	c.Handle(w, httptest.NewRequest("GET", "/metalink?repo=%ZZ", nil), nil)
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
	c.Refresh()
	c.transport = roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("private upstream details") })
	w = httptest.NewRecorder()
	c.Handle(w, httptest.NewRequest("GET", "/metalink?repo=r&arch=x", nil), nil)
	if w.Code != 502 || strings.Contains(w.Body.String(), "private upstream") {
		t.Fatal("failure not sanitized")
	}
	for _, cause := range []error{ErrCapacity, resource.ErrAdmission} {
		w = httptest.NewRecorder()
		fail(w, cause)
		if w.Code != 503 || w.Header().Get("Retry-After") != "60" || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("capacity response")
		}
	}
	token := strings.Repeat("a", 64)
	c.snapshots[token] = &snapshot{expires: time.Now().Add(-time.Second)}
	w = httptest.NewRecorder()
	c.Handle(w, httptest.NewRequest("GET", "/metalink/repos/"+token+"/repodata/repomd.xml", nil), nil)
	if w.Code != 410 || c.snapshots[token] != nil {
		t.Fatal("expired token retained")
	}
}
func TestMetadataPublicFramingAndFreshnessEdges(t *testing.T) {
	for _, raw := range []string{strings.Repeat("a", 513), "repo=%zz&arch=x"} {
		if _, e := parseQuery(raw); e == nil {
			t.Fatal("invalid query")
		}
	}
	for _, tc := range []struct {
		name, value string
		want        bool
	}{{"Content-Length", "0", true}, {"Content-Length", "1", false}, {"Connection", "close", true}, {"Connection", "keep-alive", true}, {"Connection", "upgrade", false}} {
		r := httptest.NewRequest("GET", "http://proxy.example/metalink", nil)
		r.Header.Set(tc.name, tc.value)
		if publicRequest(r) != tc.want {
			t.Fatal(tc)
		}
	}
	for _, p := range []string{"", "relative", "/a\t/b"} {
		if safePath(p) {
			t.Fatal("unsafe path", p)
		}
	}
	for _, h := range []string{"fe80::1%eth0"} {
		if _, e := normalizeHost(h); e == nil {
			t.Fatal("zone accepted")
		}
	}
	if _, e := parseURL("http://a..b/path"); e == nil {
		t.Fatal("bad host accepted")
	}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		h    http.Header
		want time.Duration
	}{{http.Header{"Cache-Control": []string{"max-age=-1"}}, 0}, {http.Header{"Cache-Control": []string{"max-age=invalid"}}, 0}, {http.Header{"Cache-Control": []string{"max-age=10"}, "Age": []string{"20"}}, 0}, {http.Header{"Expires": []string{now.Add(time.Minute).Format(http.TimeFormat)}}, time.Minute}, {http.Header{"Expires": []string{now.Add(-time.Minute).Format(http.TimeFormat)}}, 0}} {
		ttl := time.Hour
		capSourceFreshness(tc.h, &ttl, now)
		if ttl != tc.want {
			t.Fatalf("freshness %v != %v", ttl, tc.want)
		}
	}
	var nilController *Controller
	next := roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, nil })
	if nilController.WrapTransport(next) == nil {
		t.Fatal("nil controller lost transport")
	}
	r := httptest.NewRequest("POST", "http://example.org", nil)
	r.Header.Set("If-None-Match", "*")
	if validArtifactResponse(r, &http.Response{StatusCode: 304}) {
		t.Fatal("POST 304 accepted")
	}
}
func TestMetadataXMLBoundsAndHashCompatibility(t *testing.T) {
	c := edgeController(t)
	good := document("repomd", resourceURL("http://mirror.example/repo", 100))
	for _, v := range []string{"<", "text" + good, strings.Replace(good, timestampNamespace, "wrong", 1), strings.Replace(good, "1791315727", "0", 1), strings.Replace(good, "<size>6</size>", "<size>0</size>", 1), strings.Replace(good, "<size>6</size>", "", 1), strings.Replace(good, "<files>", "<files a=\"one\" a=\"two\">", 1), strings.Replace(good, "<files>", "<files a=\""+strings.Repeat("x", 4097)+"\">", 1), strings.Replace(good, "<resources>", "<resources>"+strings.Repeat("x", 8193), 1)} {
		if _, e := c.parse([]byte(v)); e == nil {
			t.Fatalf("malformed XML accepted: %.80s", v)
		}
	}
	hash := fmt.Sprintf("<hash type=\"md5\">%x</hash>", md5.Sum([]byte("repomd")))
	weak := strings.Replace(good, good[strings.Index(good, "<hash"):strings.Index(good, "</verification>")], hash, 1)
	if _, e := c.parse([]byte(weak)); e == nil {
		t.Fatal("weak hashes accepted")
	}
	extra := hash + fmt.Sprintf("<hash type=\"sha1\">%x</hash>", sha1.Sum([]byte("repomd")))
	m, e := c.parse([]byte(strings.Replace(good, "</verification>", extra+"</verification>", 1)))
	if e != nil || !m.verify([]byte("repomd")) {
		t.Fatalf("legacy compatibility hashes: %v", e)
	}
	if m.verify([]byte("broken")) {
		t.Fatal("same-size corruption accepted")
	}
	mixed := resourceURL("http://mirror.example/repo", 100) + resourceURL("http://mirror.example/repo", 100) + resourceURL("http://second.example/repo", 100) + resourceURL("http://mirror.example/bad", 101)
	m, e = c.parse([]byte(document("repomd", mixed)))
	if e != nil || len(m.mirrors) != 2 || m.mirrors[0].base.Host != "mirror.example" {
		t.Fatal("mirror sorting/deduplication", e)
	}
	c.outer, _ = access.NewUpstream(&access.UpstreamConfig{Enabled: true, AllowedHosts: []string{"second.example"}})
	m, e = c.parse([]byte(document("repomd", mixed)))
	if e != nil || len(m.mirrors) != 1 || m.mirrors[0].base.Host != "second.example" {
		t.Fatal("upstream restriction", e)
	}
}
func TestMetadataReadResourceAndTransportFailures(t *testing.T) {
	c := edgeController(t)
	u, _ := url.Parse("http://mirror.example/repo")
	c.transport = roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("too large")), ContentLength: -1}, nil
	})
	if _, _, e := c.read(context.Background(), u, 2); e == nil {
		t.Fatal("unbounded metadata accepted")
	}
	c.outer, _ = access.NewUpstream(&access.UpstreamConfig{Enabled: true, AllowedHosts: []string{"second.example"}})
	if _, _, e := c.read(context.Background(), u, 2); e == nil {
		t.Fatal("denied host read")
	}
	c.outer = nil
	budget, e := resource.New(resource.Config{MaxObjectBytes: 2, TotalTempBytes: 100, MaxActiveDownloads: 1})
	if e != nil {
		t.Fatal(e)
	}
	c.resources = budget
	if _, _, e = c.read(context.Background(), u, 20); e == nil {
		t.Fatal("object limit ignored")
	}
	if budget.Stats().TemporaryBytes != 0 {
		t.Fatal("read leaked reservation")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, e = c.read(ctx, u, 1); !errors.Is(e, context.Canceled) {
		t.Fatalf("read canceled: %v", e)
	}
	if _, e = c.fetch(ctx, url.Values{"repo": {"r"}, "arch": {"x"}}); !errors.Is(e, context.Canceled) {
		t.Fatalf("fetch canceled: %v", e)
	}
	q := url.Values{"repo": {"r"}, "arch": {"x"}}
	c.repos[q.Encode()] = &repository{done: make(chan struct{})}
	if _, e = c.get(ctx, q); !errors.Is(e, context.Canceled) {
		t.Fatalf("waiting request cancellation: %v", e)
	}
}

func TestMetadataFetchRejectsInvalidOrUnverifiableSource(t *testing.T) {
	for _, badXML := range []bool{false, true} {
		t.Run(fmt.Sprint(badXML), func(t *testing.T) {
			c := edgeController(t)
			c.transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				body := "bad"
				if r.URL.Hostname() == "mirrors.centos.org" && !badXML {
					body = document("repomd", resourceURL("http://mirror.example/repo", 100))
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body))}, nil
			})
			_, err := c.fetch(context.Background(), url.Values{"repo": {"r"}, "arch": {"x"}})
			if err == nil {
				t.Fatal("invalid metadata produced a usable snapshot")
			}
		})
	}
}
func TestMetadataSnapshotCapacityAndForwardedHeaderIsolation(t *testing.T) {
	c := edgeController(t)
	c.cfg.MaxRepositories = 1
	c.transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body := "repomd"
		if r.URL.Hostname() == "mirrors.centos.org" {
			body = document(body, resourceURL("http://mirror.example/repo", 100))
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), ContentLength: int64(len(body))}, nil
	})
	q := url.Values{"repo": {"r"}, "arch": {"x"}}
	s, err := c.get(context.Background(), q)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/metalink/repos/"+s.token+"/Packages/pkg.rpm", nil)
	for _, name := range []string{"X-Request-ID", "Connection", "Proxy-Connection", "Keep-Alive", "Host"} {
		r.Header.Set(name, "close")
	}
	r.Header.Set("Content-Length", "0")
	seen := false
	w := httptest.NewRecorder()
	c.Handle(w, r, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = true
		if len(r.Header) != 0 {
			t.Fatalf("operational headers reached canonical request: %v", r.Header)
		}
		w.WriteHeader(200)
	}))
	if !seen || w.Code != 200 {
		t.Fatal("canonical forwarding was not reached")
	}
	c.Refresh()
	for i := 0; i < 3; i++ {
		c.snapshots[fmt.Sprintf("%064d", i)] = &snapshot{expires: time.Now().Add(time.Hour)}
	}
	_, err = c.get(context.Background(), url.Values{"repo": {"new"}, "arch": {"x"}})
	if !errors.Is(err, ErrCapacity) {
		t.Fatalf("immutable snapshot capacity: %v", err)
	}
}
