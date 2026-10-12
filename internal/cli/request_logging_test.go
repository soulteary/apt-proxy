package cli

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/soulteary/apt-proxy/internal/config"
	"github.com/soulteary/apt-proxy/internal/distro"
	logger "github.com/soulteary/logger-kit/v3"
)

func TestRequestLogsRedactCustomQueryAuthentication(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("custom_auth") != "query-secret" {
			t.Error("redaction changed repository authentication")
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, "package-data")
	}))
	defer upstream.Close()
	previous := logger.Default()
	srv, err := NewServer(&config.Config{CacheDir: t.TempDir(), Mode: distro.TypeUbuntu, Listen: "127.0.0.1:0", Mirrors: config.MirrorConfig{Ubuntu: upstream.URL + "/ubuntu/"}})
	if err != nil {
		logger.SetDefault(previous)
		t.Fatal(err)
	}
	var output bytes.Buffer
	srv.log = logger.New(logger.Config{Level: logger.InfoLevel, Output: &output, Format: logger.FormatJSON})
	srv.app = srv.createFiberApp()
	t.Cleanup(func() {
		_ = srv.shutdown()
		logger.SetDefault(previous)
	})
	res, err := srv.app.Test(httptest.NewRequest("GET", "/ubuntu/pool/package.deb?custom_auth=query-secret&arch=amd64", nil))
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil || res.StatusCode != 200 || string(body) != "package-data" {
		t.Fatalf("repository response changed: %d %q %v", res.StatusCode, body, err)
	}
	logs := output.String()
	if strings.Contains(logs, "query-secret") || strings.Contains(logs, "amd64") {
		t.Fatalf("query values leaked in production request logs: %s", logs)
	}
	if !strings.Contains(logs, "HTTP request") || !strings.Contains(logs, "custom_auth") || !strings.Contains(logs, "arch") {
		t.Fatalf("query keys missing from request diagnostics: %s", logs)
	}
}
