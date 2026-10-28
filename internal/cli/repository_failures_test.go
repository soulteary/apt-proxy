package cli

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/soulteary/apt-proxy/internal/repository"
)

type repositoryBrokenOutput struct{}

func (repositoryBrokenOutput) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestRepositoryCommandHelpAndMalformedInputs(t *testing.T) {
	if err := RunRepository(context.Background(), []string{"--help"}, repositoryBrokenOutput{}); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("help output error: %v", err)
	}
	if err := RunRepository(context.Background(), []string{"--help"}, io.Discard); err != nil {
		t.Fatal(err)
	}
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(dir, "bad.tar")
	if err := os.WriteFile(archive, []byte("not a tar"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"verify", "--unknown"},
		{"repair", "--dir", dir},
		{"import", "--dir", dir, "--file", filepath.Join(dir, "missing")},
		{"import", "--dir", dir, "--file", dir},
		{"import", "--dir", dir, "--file", archive},
		{"export", "--dir", dir, "--file", filepath.Join(dir, "missing", "archive.tar")},
		{"export", "--dir", dir, "--file", filepath.Join(dir, "archive.tar")},
		{"serve", "--dir", dir},
	} {
		if err := RunRepository(context.Background(), args, io.Discard); err == nil {
			t.Errorf("accepted invalid input %v", args)
		}
	}
}

func TestRepositoryServePropagatesListenerAndOutputErrors(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	store, err := repository.Open(filepath.Join(dir, "snapshot"), repository.Limits{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	// An empty Packages index still forms a complete, verified snapshot.
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/Release") {
			_, _ = fmt.Fprintf(w, "Suite: stable\nSHA256:\n %x 0 main/binary-amd64/Packages\n", sha256.Sum256(nil))
			return
		}
		if strings.HasSuffix(r.URL.Path, "/Packages") {
			return
		}
		http.NotFound(w, r)
	}))
	defer origin.Close()
	if _, err := store.Prefetch(context.Background(), repository.Selection{URL: origin.URL, Suite: "stable", Components: []string{"main"}, Architectures: []string{"amd64"}}); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := serveRepository(context.Background(), store, listener.Addr().String(), io.Discard); err == nil {
		t.Fatal("occupied listener accepted")
	}
	if err := serveRepository(context.Background(), store, "127.0.0.1:0", repositoryBrokenOutput{}); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("output failure: %v", err)
	}
	if err := exportRepository(context.Background(), store, dir); err == nil {
		t.Fatal("export replaced directory")
	}
}
