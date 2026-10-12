package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRepositoryCommandPrefetchVerifyRepairExportImport(t *testing.T) {
	binary := []byte("fixture package")
	packages := []byte(fmt.Sprintf("Package: fixture\nFilename: pool/fixture.deb\nSize: %d\nSHA256: %x\n", len(binary), sha256.Sum256(binary)))
	release := []byte(fmt.Sprintf("Suite: stable\nSHA256:\n %x %d main/binary-amd64/Packages\n", sha256.Sum256(packages), len(packages)))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/dists/stable/Release":
			_, _ = w.Write(release)
		case "/dists/stable/main/binary-amd64/Packages":
			_, _ = w.Write(packages)
		case "/pool/fixture.deb":
			_, _ = w.Write(binary)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store := filepath.Join(dir, "store")
	var output bytes.Buffer
	if err := RunRepository(context.Background(), []string{"prefetch", "--dir", store, "--url", server.URL, "--suite", "stable"}, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Published complete snapshot") {
		t.Fatal(output.String())
	}
	if err := RunRepository(context.Background(), []string{"verify", "--dir", store}, &output); err != nil {
		t.Fatal(err)
	}
	object := filepath.Join(store, "objects", fmt.Sprintf("%x", sha256.Sum256(binary)))
	if err := os.Remove(object); err != nil {
		t.Fatal(err)
	}
	if err := RunRepository(context.Background(), []string{"verify", "--dir", store}, &output); err == nil {
		t.Fatal("verify command accepted missing package")
	}
	if err := RunRepository(context.Background(), []string{"repair", "--dir", store, "--paths", "pool/fixture.deb"}, &output); err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(dir, "snapshot.tar")
	if err := RunRepository(context.Background(), []string{"export", "--dir", store, "--file", archive}, &output); err != nil {
		t.Fatal(err)
	}
	imported := filepath.Join(dir, "imported")
	if err := RunRepository(context.Background(), []string{"import", "--dir", imported, "--file", archive}, &output); err != nil {
		t.Fatal(err)
	}
	if err := RunRepository(context.Background(), []string{"verify", "--dir", imported}, &output); err != nil {
		t.Fatal(err)
	}
	// The CLI's server itself, including cancellation, runs against the imported set.
	server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	listening := make(chan string, 1)
	done := make(chan error, 1)
	go func() {
		done <- RunRepository(ctx, []string{"serve", "--dir", imported, "--listen", "127.0.0.1:0"}, listeningWriter{listening})
	}()
	var address string
	select {
	case line := <-listening:
		address = strings.TrimSpace(strings.TrimPrefix(line, "Offline repository listening on "))
	case <-time.After(5 * time.Second):
		t.Fatal("offline CLI did not start")
	}
	if _, err := url.ParseRequestURI(address); err != nil {
		t.Fatal(err)
	}
	response, err := http.Get(address + "/pool/fixture.deb")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil || response.StatusCode != 200 || !bytes.Equal(data, binary) {
		t.Fatalf("offline CLI response: %d %q %v", response.StatusCode, data, err)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("offline CLI did not stop")
	}
}

type listeningWriter struct{ lines chan string }

func (w listeningWriter) Write(p []byte) (int, error) { w.lines <- string(p); return len(p), nil }

func TestRepositoryCommandRejectsInvalidArguments(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{nil, {"unknown"}, {"verify"}, {"verify", "--dir", dir, "extra"}, {"export", "--dir", dir}, {"import", "--dir", dir}, {"prefetch", "--dir", dir, "--url", "http://user:secret@example.invalid", "--suite", "stable"}, {"verify", "--dir", dir, "--max-file-bytes", "-1"}} {
		if err := RunRepository(context.Background(), args, io.Discard); err == nil {
			t.Fatalf("invalid arguments accepted: %v", args)
		}
	}
	if err := RunRepository(context.Background(), []string{"prefetch", "--help"}, io.Discard); err != nil {
		t.Fatal(err)
	}
}
