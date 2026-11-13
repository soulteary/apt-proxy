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

package cli

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soulteary/apt-proxy/internal/config"
	"github.com/soulteary/apt-proxy/internal/distro"
)

// TestProxyModeCredentialsStayWithNamedOrigin drives the daemon over a real
// connection. A client that uses it as an HTTP proxy names a private origin
// and sends credentials for it; the request is served from the configured
// mirror, which must not receive them. A request addressed to the daemon
// itself keeps forwarding its credentials to the mirror.
func TestProxyModeCredentialsStayWithNamedOrigin(t *testing.T) {
	type upstreamRequest struct{ path, authorization, cookie string }
	seen := make(chan upstreamRequest, 4)
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- upstreamRequest{r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("Cookie")}
		_, _ = io.WriteString(w, "from the mirror")
	}))
	defer mirror.Close()

	srv, err := NewServer(&config.Config{
		CacheDir: t.TempDir(), Mode: distro.TypeUbuntu, Listen: "127.0.0.1:0",
		Mirrors: config.MirrorConfig{Ubuntu: mirror.URL + "/ubuntu/"},
	})
	if err != nil {
		t.Fatal(err)
	}
	address := listenOn(t, srv)

	proxied := &http.Transport{Proxy: http.ProxyURL(&url.URL{Scheme: "http", Host: address})}
	defer proxied.CloseIdleConnections()
	get := func(transport http.RoundTripper, target string) upstreamRequest {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, target, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer client-token")
		req.Header.Set("Cookie", "session=client")
		res, err := (&http.Client{Timeout: 10 * time.Second, Transport: transport}).Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, err := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if err != nil || res.StatusCode != http.StatusOK || string(body) != "from the mirror" {
			t.Fatalf("GET %s: status=%d body=%q err=%v", target, res.StatusCode, body, err)
		}
		select {
		case request := <-seen:
			return request
		case <-time.After(5 * time.Second):
			t.Fatalf("GET %s did not reach the mirror", target)
			return upstreamRequest{}
		}
	}

	got := get(proxied, "http://private.example.test/ubuntu/dists/jammy/InRelease")
	if want := (upstreamRequest{path: "/ubuntu/dists/jammy/InRelease"}); got != want {
		t.Errorf("request naming another origin reached the mirror as %+v, want %+v", got, want)
	}

	got = get(http.DefaultTransport, "http://"+address+"/ubuntu/dists/noble/InRelease")
	if want := (upstreamRequest{"/ubuntu/dists/noble/InRelease", "Bearer client-token", "session=client"}); got != want {
		t.Errorf("request addressed to the daemon reached the mirror as %+v, want %+v", got, want)
	}
}

// TestProxyModeFetchesOnlyFromConfiguredMirrors drives the daemon over a real
// connection as an HTTP proxy. The request line names a host that is no mirror
// of anything. A distribution request is served from the configured mirror
// whatever host it names, and one that cannot be pointed at a mirror ends at
// the daemon with 404: the named host must never be contacted.
func TestProxyModeFetchesOnlyFromConfiguredMirrors(t *testing.T) {
	var namedHostRequests atomic.Int64
	named := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		namedHostRequests.Add(1)
		_, _ = io.WriteString(w, "from the named host")
	}))
	defer named.Close()
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "mirror "+r.URL.EscapedPath())
	}))
	defer mirror.Close()
	mirrors := config.MirrorConfig{
		Ubuntu: mirror.URL + "/ubuntu/", UbuntuPorts: mirror.URL + "/ubuntu-ports/", Debian: mirror.URL + "/debian/",
		CentOS: mirror.URL + "/centos/", Alpine: mirror.URL + "/alpine/",
	}

	for _, tc := range []struct {
		name       string
		mode       int
		path       string
		wantStatus int
		wantBody   string
	}{
		{"default mode", distro.TypeAllDistros, "/debian/dists/x/InRelease", http.StatusOK, "mirror /debian/dists/x/InRelease"},
		{"default mode, escaped distribution segment", distro.TypeAllDistros, "/%64ebian/dists/x/InRelease", http.StatusNotFound, ""},
		{"single mode, selected distribution", distro.TypeDebian, "/debian/dists/x/InRelease", http.StatusOK, "mirror /debian/dists/x/InRelease"},
		{"single mode, another distribution", distro.TypeDebian, "/ubuntu/dists/x/InRelease", http.StatusNotFound, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv, err := NewServer(&config.Config{CacheDir: t.TempDir(), Mode: tc.mode, Listen: "127.0.0.1:0", Mirrors: mirrors})
			if err != nil {
				t.Fatal(err)
			}
			address := listenOn(t, srv)
			proxied := &http.Transport{Proxy: http.ProxyURL(&url.URL{Scheme: "http", Host: address})}
			defer proxied.CloseIdleConnections()

			res, err := (&http.Client{Timeout: 10 * time.Second, Transport: proxied}).Get(named.URL + tc.path)
			if err != nil {
				t.Fatal(err)
			}
			body, err := io.ReadAll(res.Body)
			_ = res.Body.Close()
			if err != nil || res.StatusCode != tc.wantStatus || (tc.wantBody != "" && string(body) != tc.wantBody) {
				t.Errorf("GET %s%s through the proxy: status=%d body=%q err=%v, want %d %q", named.URL, tc.path, res.StatusCode, body, err, tc.wantStatus, tc.wantBody)
			}
			if got := namedHostRequests.Load(); got != 0 {
				t.Errorf("the host named by the client received %d request(s)", got)
			}
		})
	}
}
