package cli

import (
	"crypto/sha256"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soulteary/apt-proxy/internal/access"
	"github.com/soulteary/apt-proxy/internal/config"
	"github.com/soulteary/apt-proxy/internal/distro"
	"github.com/soulteary/apt-proxy/internal/metalink"
)

func TestMetalinkServerCacheAndRefresh(t *testing.T) {
	metadata := `<repomd xmlns="http://linux.duke.edu/metadata/repo"></repomd>`
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte(metadata)))
	var documents, packages atomic.Int32
	var origin *httptest.Server
	origin = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/metalink":
			documents.Add(1)
			if r.URL.Query().Get("repo") != "centos-baseos-9-stream" || r.URL.Query().Get("arch") != "x86_64" {
				t.Errorf("upstream metalink query changed: %s", r.URL.RawQuery)
			}
			w.Header().Set("Content-Type", "application/metalink+xml")
			_, _ = fmt.Fprintf(w, `<metalink xmlns="http://www.metalinker.org/" version="3.0"><files><file name="repomd.xml"><size>%d</size><verification><hash type="sha256">%s</hash></verification><resources><url protocol="http" preference="100">%s/archive/repodata/repomd.xml</url></resources></file></files></metalink>`, len(metadata), digest, origin.URL)
		case "/archive/repodata/repomd.xml":
			_, _ = io.WriteString(w, metadata)
		case "/archive/Packages/example.rpm":
			packages.Add(1)
			w.Header().Set("Cache-Control", "public, max-age=600")
			_, _ = io.WriteString(w, "rpm-payload")
		default:
			http.NotFound(w, r)
		}
	}))
	defer origin.Close()
	u, err := url.Parse(origin.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		CacheDir: t.TempDir(), Listen: "127.0.0.1:0", Mode: distro.TypeCentOS,
		Mirrors:        config.MirrorConfig{CentOS: origin.URL + "/centos/"},
		Metalink:       &metalink.Config{PublicURL: "http://proxy.example:3142", SourceURL: origin.URL + "/metalink", AllowedMirrorHosts: []string{u.Hostname()}},
		UpstreamAccess: &access.UpstreamConfig{Enabled: true, AllowedHosts: []string{u.Hostname()}, AllowedPorts: []int{port}, AllowCIDRs: []string{"127.0.0.0/8"}},
	}
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = srv.shutdown() })
	request := func(target string, headers http.Header) (int, []byte) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, target, nil)
		req.Header = headers
		res, err := srv.app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		body, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		return res.StatusCode, body
	}
	metalinkURL := "/metalink?repo=centos-baseos-9-stream&arch=x86_64&protocol=http"
	getDocument := func() string {
		t.Helper()
		status, body := request(metalinkURL, nil)
		if status != 200 {
			t.Fatalf("metalink: %d %s", status, body)
		}
		var doc struct {
			Files struct {
				File struct {
					Resources struct {
						URLs []string `xml:"url"`
					} `xml:"resources"`
				} `xml:"file"`
			} `xml:"files"`
		}
		if err := xml.Unmarshal(body, &doc); err != nil {
			t.Fatal(err)
		}
		if len(doc.Files.File.Resources.URLs) != 1 || !strings.HasPrefix(doc.Files.File.Resources.URLs[0], "http://proxy.example:3142/metalink/repos/") {
			t.Fatalf("unproxied document: %s", body)
		}
		return doc.Files.File.Resources.URLs[0]
	}
	metadataURL := getDocument()
	if again := getDocument(); again != metadataURL || documents.Load() != 1 {
		t.Fatalf("clients failed to converge: %q %q documents=%d", metadataURL, again, documents.Load())
	}
	if status, body := request(metadataURL, nil); status != 200 || string(body) != metadata {
		t.Fatalf("metadata route: %d %s", status, body)
	}
	packageURL := strings.TrimSuffix(metadataURL, "repodata/repomd.xml") + "Packages/example.rpm"
	if status, body := request(packageURL, nil); status != 200 || string(body) != "rpm-payload" {
		t.Fatalf("package: %d %s", status, body)
	}
	// Cache publication owns its request context after the HTTP handler returns.
	time.Sleep(50 * time.Millisecond)
	if status, body := request(packageURL, nil); status != 200 || string(body) != "rpm-payload" || packages.Load() != 1 {
		t.Fatalf("repeated package failed cache hit: status=%d body=%s origin=%d", status, body, packages.Load())
	}
	for _, header := range []http.Header{{"Authorization": {"Bearer secret"}}, {"Cookie": {"tenant=private"}}, {"X-Token": {"private"}}} {
		if status, _ := request(packageURL, header); status < 400 || packages.Load() != 1 {
			t.Fatalf("private request reached public cache: status=%d origin=%d", status, packages.Load())
		}
	}
	if err := srv.refreshMirrors(); err != nil {
		t.Fatal(err)
	}
	if getDocument() != metadataURL || documents.Load() != 2 {
		t.Fatalf("refresh did not refetch stable snapshot: documents=%d", documents.Load())
	}
}
