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
	"bufio"
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	logger "github.com/soulteary/logger-kit/v3"

	"github.com/soulteary/apt-proxy/internal/config"
	"github.com/soulteary/apt-proxy/internal/distro"
)

// testRequest serves req through the server's handler in process and returns
// the recorded response. A handler panic is returned as an error, which is how
// a client of the listening server observes an aborted connection.
func (s *Server) testRequest(req *http.Request) (res *http.Response, err error) {
	if req.Header == nil {
		req.Header = make(http.Header) // as every request read from a connection has
	}
	rec := httptest.NewRecorder()
	defer func() {
		if recovered := recover(); recovered != nil {
			res, err = nil, fmt.Errorf("handler panicked: %v", recovered)
		}
	}()
	s.handler.ServeHTTP(rec, req)
	return rec.Result(), nil
}

// listenOn serves srv on a loopback listener for the duration of the test and
// returns its address.
func listenOn(t *testing.T, srv *Server) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- srv.listen(listener) }()
	t.Cleanup(func() {
		_ = srv.shutdown()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("server exit: %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("server did not stop")
		}
	})
	return listener.Addr().String()
}

// syncBuffer is a log sink that is safe to read while handlers still write.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestOperationalRoutes(t *testing.T) {
	srv := makeIsolationServer(t, "routes", "", "http://127.0.0.1:1")
	for _, tt := range []struct {
		method, target string
		status         int
		contentType    string
		body           string
	}{
		{"GET", "/healthz", 200, "application/json", `"status"`},
		{"HEAD", "/healthz", 200, "application/json", ""},
		{"GET", "/readyz", 200, "application/json", `"status"`},
		{"GET", "/livez", 200, "application/json", `"apt-proxy"`},
		{"GET", "/version", 200, "application/json", `"version"`},
		{"GET", "/metrics", 200, "text/plain", "apt_proxy"},
		{"GET", "/", 200, "text/html", "<html"},
		{"HEAD", "/", 200, "text/html", ""},
		{"GET", "/static/apt-proxy-logo.png", 200, "image/png", "PNG"},
		{"GET", "/_/ping", 200, "text/plain", "pong"},
		{"POST", "/_/ping", 200, "text/plain", "pong"},
		{"GET", "/_/ping/any/depth", 200, "text/plain", "pong"},
		{"GET", "/api/cache/stats", 200, "application/json", ""},
		{"GET", "/api/mirrors/status", 200, "application/json", ""},
		{"POST", "/api/mirrors/status", 405, "application/json", ""},
		// Operational endpoints match without regard to case or a trailing slash.
		{"GET", "/Healthz", 200, "application/json", `"status"`},
		{"GET", "/healthz/", 200, "application/json", `"status"`},
		{"GET", "/_/ping/", 200, "text/plain", "pong"},
		// An absolute request target is routed by its path.
		{"GET", "http://archive.ubuntu.com/livez", 200, "application/json", `"apt-proxy"`},
		// Read-only endpoints leave other methods to the repository handler,
		// which accepts GET and HEAD only.
		{"POST", "/healthz", 405, "", "GET and HEAD"},
		{"DELETE", "/version", 405, "", "GET and HEAD"},
		{"POST", "/", 405, "", "GET and HEAD"},
		{"GET", "/no/such/repository", 404, "", ""},
	} {
		res, err := srv.testRequest(httptest.NewRequest(tt.method, tt.target, nil))
		if err != nil {
			t.Fatalf("%s %s: %v", tt.method, tt.target, err)
		}
		body, _ := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if res.StatusCode != tt.status {
			t.Errorf("%s %s: status = %d, want %d", tt.method, tt.target, res.StatusCode, tt.status)
		}
		if tt.contentType != "" && !strings.HasPrefix(res.Header.Get("Content-Type"), tt.contentType) {
			t.Errorf("%s %s: Content-Type = %q, want %q", tt.method, tt.target, res.Header.Get("Content-Type"), tt.contentType)
		}
		if tt.body != "" && !strings.Contains(string(body), tt.body) {
			t.Errorf("%s %s: body %.80q does not contain %q", tt.method, tt.target, body, tt.body)
		}
		if res.Header.Get("X-Content-Type-Options") != "nosniff" || res.Header.Get("X-Version") == "" {
			t.Errorf("%s %s: security or version headers missing: %v", tt.method, tt.target, res.Header)
		}
	}
}

func TestRoutePath(t *testing.T) {
	for in, want := range map[string]string{
		"/":                 "/",
		"/Healthz":          "/healthz",
		"/healthz/":         "/healthz",
		"/API/Cache/Stats/": "/api/cache/stats",
		"/_/ping//":         "/_/ping/",
		"":                  "",
	} {
		if got := routePath(in); got != want {
			t.Errorf("routePath(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAccessLogRecordsCacheOutcome(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", "max-age=60")
		_, _ = io.WriteString(w, "package-data")
	}))
	defer upstream.Close()
	previous := logger.Default()
	srv, err := NewServer(&config.Config{CacheDir: t.TempDir(), Mode: distro.TypeUbuntu, Listen: "127.0.0.1:0", Mirrors: config.MirrorConfig{Ubuntu: upstream.URL + "/ubuntu/"}})
	if err != nil {
		logger.SetDefault(previous)
		t.Fatal(err)
	}
	var logs syncBuffer
	srv.log = logger.New(logger.Config{Level: logger.InfoLevel, Output: &logs, Format: logger.FormatJSON})
	srv.handler = srv.newHandler()
	t.Cleanup(func() {
		_ = srv.shutdown()
		logger.SetDefault(previous)
	})

	for _, want := range []string{`"cache":"MISS"`, `"cache":"HIT"`} {
		res, err := srv.testRequest(httptest.NewRequest("GET", "/ubuntu/pool/main/p/package.deb", nil))
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if res.StatusCode != 200 || string(body) != "package-data" {
			t.Fatalf("response: %d %q", res.StatusCode, body)
		}
		waitForCacheStore(t, srv)
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("access log lacks %s: %s", want, logs.String())
		}
	}
	if !strings.Contains(logs.String(), `"size":12`) || !strings.Contains(logs.String(), `"path":"/ubuntu/pool/main/p/package.deb"`) {
		t.Fatalf("access log lacks size or the client's path: %s", logs.String())
	}

	before := logs.String()
	for _, path := range []string{"/healthz", "/livez", "/readyz"} {
		res, err := srv.testRequest(httptest.NewRequest("GET", path, nil))
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
	}
	if logs.String() != before {
		t.Fatalf("health probes were logged: %s", strings.TrimPrefix(logs.String(), before))
	}
}

// Responses must reach the client while the upstream is still sending, and a
// large body must not be held in memory until the handler returns.
func TestRepositoryResponseStreamsToClient(t *testing.T) {
	const chunk, chunks = 64 << 10, 16
	release := make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(chunk*chunks))
		w.Header().Set("Cache-Control", "max-age=60")
		payload := bytes.Repeat([]byte("s"), chunk)
		_, _ = w.Write(payload)
		w.(http.Flusher).Flush()
		<-release
		for i := 1; i < chunks; i++ {
			_, _ = w.Write(payload)
		}
	}))
	defer upstream.Close()
	defer unblock()
	srv, err := NewServer(&config.Config{CacheDir: t.TempDir(), Mode: distro.TypeUbuntu, Listen: "127.0.0.1:0", Mirrors: config.MirrorConfig{Ubuntu: upstream.URL + "/ubuntu/"}})
	if err != nil {
		t.Fatal(err)
	}
	address := listenOn(t, srv)

	res, err := (&http.Client{Timeout: 10 * time.Second}).Get("http://" + address + "/ubuntu/pool/main/s/stream.deb")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	// The upstream has sent one chunk and is blocked. Headers and that chunk
	// must already be readable.
	first := make([]byte, chunk)
	if _, err := io.ReadFull(res.Body, first); err != nil {
		t.Fatalf("first chunk was not delivered while the upstream was still sending: %v", err)
	}
	if res.StatusCode != http.StatusOK || res.Header.Get("X-Cache") != "MISS" {
		t.Fatalf("status=%d X-Cache=%q", res.StatusCode, res.Header.Get("X-Cache"))
	}
	unblock()
	rest, err := io.ReadAll(res.Body)
	if err != nil || len(rest) != chunk*(chunks-1) {
		t.Fatalf("remaining body: %d bytes, err=%v", len(rest), err)
	}
}

func TestHandlerPanicFailsOnlyItsConnection(t *testing.T) {
	srv := makeIsolationServer(t, "panic", "", "http://127.0.0.1:1")
	var logs syncBuffer
	srv.log = logger.New(logger.Config{Level: logger.InfoLevel, Output: &logs, Format: logger.FormatJSON})
	routes := srv.handler
	var panics atomic.Int32
	srv.handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/boom" {
			panics.Add(1)
			panic("handler defect")
		}
		routes.ServeHTTP(w, r)
	})
	address := listenOn(t, srv)
	client := &http.Client{Timeout: 5 * time.Second}

	if res, err := client.Get("http://" + address + "/boom"); err == nil {
		_ = res.Body.Close()
		t.Fatalf("panicking handler answered with status %d", res.StatusCode)
	}
	res, err := client.Get("http://" + address + "/livez")
	if err != nil {
		t.Fatalf("server stopped serving after a handler panic: %v", err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK || panics.Load() == 0 {
		t.Fatalf("status=%d panics=%d", res.StatusCode, panics.Load())
	}
	if !strings.Contains(logs.String(), "http: panic serving") || !strings.Contains(logs.String(), `"level":"error"`) {
		t.Fatalf("panic was not reported at error level: %s", logs.String())
	}
}

func TestServerLogWriterLevels(t *testing.T) {
	srv := makeIsolationServer(t, "server-log", "", "http://127.0.0.1:1")
	var logs syncBuffer
	srv.log = logger.New(logger.Config{Level: logger.InfoLevel, Output: &logs, Format: logger.FormatJSON})
	writer := serverLogWriter{srv}
	for message, level := range map[string]string{
		"http: panic serving 127.0.0.1:1: defect\ngoroutine 1 [running]:\n": "error",
		"http: TLS handshake error from 127.0.0.1:1: EOF\n":                 "warn",
	} {
		before := len(logs.String())
		if n, err := writer.Write([]byte(message)); err != nil || n != len(message) {
			t.Fatalf("Write = %d, %v", n, err)
		}
		line := logs.String()[before:]
		if !strings.Contains(line, `"level":"`+level+`"`) || !strings.Contains(line, strings.SplitN(message, "\n", 2)[0][:20]) {
			t.Errorf("message %q logged as %s", message, line)
		}
	}
}

func writeTestCertificate(t *testing.T) (certFile, keyFile string, pool *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "apt-proxy test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	certFile, keyFile = filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0600); err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool = x509.NewCertPool()
	pool.AddCert(certificate)
	return certFile, keyFile, pool
}

func TestServerServesTLS(t *testing.T) {
	certFile, keyFile, pool := writeTestCertificate(t)
	srv := makeIsolationServer(t, "tls", "", "http://127.0.0.1:1")
	srv.config.TLS = config.TLSConfig{Enabled: true, CertFile: certFile, KeyFile: keyFile}
	address := listenOn(t, srv)

	transport := &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}
	t.Cleanup(transport.CloseIdleConnections)
	res, err := (&http.Client{Transport: transport, Timeout: 5 * time.Second}).Get("https://" + address + "/livez")
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != http.StatusOK || res.TLS == nil {
		t.Fatalf("status=%d tls=%v", res.StatusCode, res.TLS != nil)
	}
	// The client offers HTTP/2 by default; the server must stay on HTTP/1.1.
	if res.ProtoMajor != 1 {
		t.Fatalf("negotiated %s, want HTTP/1.1", res.Proto)
	}
}

// A client configured with apt-proxy as its HTTPS proxy opens a CONNECT
// tunnel. The proxy cannot cache through a tunnel and must refuse it.
func TestConnectTunnelIsRefused(t *testing.T) {
	srv := makeIsolationServer(t, "connect", "", "http://127.0.0.1:1")
	address := listenOn(t, srv)
	conn, err := net.DialTimeout("tcp", address, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.WriteString(conn, "CONNECT archive.ubuntu.com:443 HTTP/1.1\r\nHost: archive.ubuntu.com:443\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	res, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: http.MethodConnect})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusMethodNotAllowed || res.Header.Get("Allow") != "GET, HEAD" {
		t.Fatalf("CONNECT: status=%d Allow=%q", res.StatusCode, res.Header.Get("Allow"))
	}
}

func TestListenReportsUnusableCertificate(t *testing.T) {
	srv := makeIsolationServer(t, "tls-missing", "", "http://127.0.0.1:1")
	srv.config.TLS = config.TLSConfig{Enabled: true, CertFile: filepath.Join(t.TempDir(), "missing.pem"), KeyFile: filepath.Join(t.TempDir(), "missing.key")}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	if err := srv.listen(listener); err == nil {
		t.Fatal("missing certificate was accepted")
	}
}
