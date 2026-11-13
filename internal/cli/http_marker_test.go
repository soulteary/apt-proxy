//go:build unix

package cli

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soulteary/apt-proxy/internal/config"
	"github.com/soulteary/apt-proxy/internal/distro"
	"github.com/soulteary/apt-proxy/internal/proxy"
	"github.com/soulteary/apt-proxy/internal/resource"
	httpcache "github.com/soulteary/httpcache-kit/v4"
)

func TestRepositoryMarkerTrailerOverTheNetwork(t *testing.T) {
	body := bytes.Repeat([]byte("b"), 64<<10)
	budget, err := resource.New(resource.Config{MaxObjectBytes: 1 << 20, TotalTempBytes: 1 << 20, MaxActiveDownloads: 1})
	if err != nil {
		t.Fatal(err)
	}
	cache := httpcache.NewMemoryCacheWithConfig(httpcache.DefaultCacheConfig().WithCleanupInterval(0))
	t.Cleanup(func() { _ = cache.Close() })
	var calls atomic.Int32
	handler := httpcache.NewHandlerWithOptions(cache, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Date", time.Now().Format(http.TimeFormat))
		w.Header().Set("Cache-Control", "public, max-age=3600")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		_, _ = w.Write(body)
	}), &httpcache.HandlerOptions{StagingBudget: budget, TempDir: t.TempDir()})
	// The marker's empty path segments must pass the daemon's path guard.
	server := httptest.NewServer(proxy.GuardRequestPath(handler))
	t.Cleanup(server.Close)
	address := server.Listener.Addr().String()
	transport := &http.Transport{Proxy: http.ProxyURL(&url.URL{Scheme: "http", Host: address})}
	t.Cleanup(transport.CloseIdleConnections)
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	for attempt := 0; attempt < 2; attempt++ {
		res, err := client.Get("http://HTTPS///get.docker.com/repo/Packages")
		if err != nil {
			t.Fatal(err)
		}
		assertMarkerStagingWire(t, res, body, attempt)
	}
	if stats := budget.Stats(); calls.Load() != 1 || stats.ActiveDownloads != 0 || stats.TemporaryBytes != 0 {
		t.Fatalf("marker publication leaked resources or failed its warm hit: calls=%d stats=%+v", calls.Load(), stats)
	}
}

func assertMarkerStagingWire(t *testing.T, res *http.Response, body []byte, attempt int) {
	t.Helper()
	got, err := io.ReadAll(res.Body)
	_ = res.Body.Close()
	httpcache.Writes.Wait()
	if err != nil || res.StatusCode != http.StatusOK || !bytes.Equal(got, body) {
		t.Fatalf("marker delivery=%d bytes=%d err=%v", res.StatusCode, len(got), err)
	}
	label, trailer := "MISS", "ADMITTED"
	if attempt == 1 {
		label, trailer = "HIT", ""
	}
	if res.Header.Get("X-Cache") != label || res.Trailer.Get("X-Cache-Staging") != trailer {
		t.Fatalf("marker outcome: headers=%v trailers=%v, want %s/%s", res.Header, res.Trailer, label, trailer)
	}
}

func TestProductionMarkerDownloadClientAccess(t *testing.T) {
	srv, err := NewServer(withTestMirrors(&config.Config{CacheDir: t.TempDir(), Mode: distro.TypeUbuntu, Listen: "127.0.0.1:0", Security: config.SecurityConfig{
		DownloadDenyCIDRs: []string{"127.0.0.0/8"}, TrustedProxies: []string{"127.0.0.0/8"},
	}}))
	if err != nil {
		t.Fatal(err)
	}
	address := listenOn(t, srv)
	transport := &http.Transport{Proxy: http.ProxyURL(&url.URL{Scheme: "http", Host: address})}
	t.Cleanup(transport.CloseIdleConnections)
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	for _, target := range []string{
		"http://HTTPS///not-allowed.example/repo/Packages",
		"http://" + address + "/HTTPS///not-allowed.example/repo/Packages",
	} {
		req, err := http.NewRequest(http.MethodGet, target, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-Forwarded-For", "192.0.2.1")
		req.Header.Set("X-Real-IP", "192.0.2.1")
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		assertMarkerAccessDenied(t, res)
	}
}

func assertMarkerAccessDenied(t *testing.T, res *http.Response) {
	t.Helper()
	body, err := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if err != nil || res.StatusCode != http.StatusForbidden || res.Header.Get("Cache-Control") != "no-store" || !strings.Contains(string(body), "package download access denied") {
		t.Fatalf("marker bypassed connection-peer access policy: status=%d body=%q err=%v", res.StatusCode, body, err)
	}
}
