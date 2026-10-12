package repository

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

type fixture struct {
	mu       sync.Mutex
	files    map[string][]byte
	requests map[string]int
	server   *httptest.Server
}

func newFixture(t *testing.T, sources bool) *fixture {
	t.Helper()
	f := &fixture{files: make(map[string][]byte), requests: make(map[string]int)}
	binary := []byte("fixture binary package")
	f.files["pool/main/h/hello/hello_1_amd64.deb"] = binary
	packages := []byte(fmt.Sprintf("Package: hello\nVersion: 1\nArchitecture: amd64\nFilename: pool/main/h/hello/hello_1_amd64.deb\nSize: %d\nSHA256: %x\n\n", len(binary), sha256.Sum256(binary)))
	f.files["dists/stable/main/binary-amd64/Packages"] = packages
	f.files["dists/stable/main/binary-amd64/Packages.gz"] = gzData(t, packages)
	if sources {
		dsc, source := []byte("Format: 3.0 (native)\nSource: hello\n"), []byte("fixture source archive")
		f.files["pool/main/h/hello/hello_1.dsc"] = dsc
		f.files["pool/main/h/hello/hello_1.tar.gz"] = source
		index := []byte(fmt.Sprintf("Package: hello\nDirectory: pool/main/h/hello\nChecksums-Sha256:\n %x %d hello_1.dsc\n %x %d hello_1.tar.gz\n\n", sha256.Sum256(dsc), len(dsc), sha256.Sum256(source), len(source)))
		f.files["dists/stable/main/source/Sources.gz"] = gzData(t, index)
	}
	f.release()
	f.files["dists/stable/Release.gpg"] = []byte("signature is checked by APT, not by this proxy")
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		p := strings.TrimPrefix(r.URL.Path, "/repo/")
		f.requests[p]++
		body, ok := f.files[p]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fixture) release() {
	var release strings.Builder
	release.WriteString("Suite: stable\nComponents: main\nArchitectures: amd64\nAcquire-By-Hash: yes\nSHA256:\n")
	for p, data := range f.files {
		if strings.Contains(p, "/Packages") || strings.Contains(p, "/Sources") {
			fmt.Fprintf(&release, " %x %d %s\n", sha256.Sum256(data), len(data), strings.TrimPrefix(p, "dists/stable/"))
		}
	}
	f.files["dists/stable/Release"] = []byte(release.String())
}

func (f *fixture) selection(sources bool) Selection {
	return Selection{URL: f.server.URL + "/repo", Suite: "stable", Components: []string{"main"}, Architectures: []string{"amd64"}, Sources: sources}
}

func gzData(t *testing.T, data []byte) []byte {
	t.Helper()
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	if _, err := w.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func testStore(t *testing.T, limits Limits) *Store {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir, limits, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestCompleteBinarySourceSnapshotOffline(t *testing.T) {
	f := newFixture(t, true)
	s := testStore(t, Limits{})
	m, err := s.Prefetch(context.Background(), f.selection(true))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}
	handler, err := s.Handler(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	f.server.Close() // Offline requests cannot use the original repository.
	for _, o := range m.Objects {
		r := httptest.NewRequest(http.MethodGet, "/"+o.Path, nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 200 || int64(w.Body.Len()) != o.Size || fmt.Sprintf("%x", sha256.Sum256(w.Body.Bytes())) != o.SHA256 {
			t.Fatalf("offline object %s: status %d", o.Path, w.Code)
		}
	}
	p := "pool/main/h/hello/hello_1_amd64.deb"
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/"+p, nil)
	r.Header.Set("Range", "bytes=0-6")
	handler.ServeHTTP(w, r)
	if w.Code != 206 || w.Body.String() != "fixture" {
		t.Fatalf("range: %d %q", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodHead, "/"+p, nil))
	if w.Code != 200 || w.Body.Len() != 0 {
		t.Fatalf("HEAD: %d %q", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/pool/missing.deb", nil))
	if w.Code != 404 {
		t.Fatalf("missing object: %d", w.Code)
	}
}

func TestFailedPrefetchPreservesPublishedSnapshot(t *testing.T) {
	f := newFixture(t, false)
	s := testStore(t, Limits{})
	if _, err := s.Prefetch(context.Background(), f.selection(false)); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(s.dir, "current"))
	f.mu.Lock()
	extra := []byte("never served")
	f.files["dists/stable/main/binary-amd64/Packages"] = []byte(fmt.Sprintf("Package: missing\nFilename: pool/missing.deb\nSize: %d\nSHA256: %x\n", len(extra), sha256.Sum256(extra)))
	f.files["dists/stable/main/binary-amd64/Packages.gz"] = gzData(t, f.files["dists/stable/main/binary-amd64/Packages"])
	f.release()
	f.mu.Unlock()
	if _, err := s.Prefetch(context.Background(), f.selection(false)); err == nil {
		t.Fatal("missing dependency was published")
	}
	after, _ := os.ReadFile(filepath.Join(s.dir, "current"))
	if !bytes.Equal(before, after) {
		t.Fatal("failed snapshot replaced current")
	}
	if _, err := s.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestCorruptionRepairAndOfflineRejection(t *testing.T) {
	f := newFixture(t, true)
	s := testStore(t, Limits{})
	m, err := s.Prefetch(context.Background(), f.selection(true))
	if err != nil {
		t.Fatal(err)
	}
	handler, err := s.Handler(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	p := "pool/main/h/hello/hello_1.tar.gz"
	o := objectMap(m)[p]
	if err := os.WriteFile(s.filename(o.SHA256), bytes.Repeat([]byte("X"), int(o.Size)), 0o600); err != nil {
		t.Fatal(err)
	}
	if damaged, err := s.Verify(context.Background()); err == nil || len(damaged) != 1 || damaged[0] != p {
		t.Fatalf("verify: %v %v", damaged, err)
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/"+p, nil))
	if w.Code != 503 {
		t.Fatalf("corrupt object served: %d", w.Code)
	}
	if _, err := s.Handler(context.Background()); err == nil {
		t.Fatal("incomplete snapshot accepted at startup")
	}
	f.mu.Lock()
	f.requests = make(map[string]int)
	f.mu.Unlock()
	if err := s.Repair(context.Background(), []string{p}); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) != 1 || f.requests[p] != 1 {
		t.Fatalf("repair fetched undamaged objects: %v", f.requests)
	}
}

func TestManifestCannotOmitReferencedDependency(t *testing.T) {
	f := newFixture(t, false)
	s := testStore(t, Limits{})
	m, err := s.Prefetch(context.Background(), f.selection(false))
	if err != nil {
		t.Fatal(err)
	}
	for i, o := range m.Objects {
		if strings.HasSuffix(o.Path, ".deb") {
			m.Objects = append(m.Objects[:i], m.Objects[i+1:]...)
			break
		}
	}
	if err := s.publish(m); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Verify(context.Background()); err == nil {
		t.Fatal("manifest with omitted dependency was accepted")
	}
	if _, err := s.Handler(context.Background()); err == nil {
		t.Fatal("partial set served offline")
	}
}

func TestSnapshotExportImportAndArchiveRejection(t *testing.T) {
	f := newFixture(t, true)
	s := testStore(t, Limits{})
	if _, err := s.Prefetch(context.Background(), f.selection(true)); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if err := s.Export(context.Background(), &archive); err != nil {
		t.Fatal(err)
	}
	imported := testStore(t, Limits{})
	if _, err := imported.Import(context.Background(), bytes.NewReader(archive.Bytes())); err != nil {
		t.Fatal(err)
	}
	if _, err := imported.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, attack := range []string{"traversal", "symlink", "missing", "corrupt", "extra-path"} {
		t.Run(attack, func(t *testing.T) {
			tr := tar.NewReader(bytes.NewReader(archive.Bytes()))
			var bad bytes.Buffer
			tw := tar.NewWriter(&bad)
			index := 0
			for {
				header, err := tr.Next()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
				data, err := io.ReadAll(tr)
				if err != nil {
					t.Fatal(err)
				}
				if index == 1 {
					switch attack {
					case "traversal":
						header.Name = "../escape"
					case "symlink":
						header.Typeflag = tar.TypeSymlink
						header.Linkname = "/etc/passwd"
						header.Size = 0
						data = nil
					case "missing":
						index++
						continue
					case "corrupt":
						if len(data) > 0 {
							data[0] ^= 1
						}
					}
				}
				if index == 0 && attack == "extra-path" {
					var m Manifest
					if err := json.Unmarshal(data, &m); err != nil {
						t.Fatal(err)
					}
					extra := m.Objects[0]
					extra.Path = "unselected/object"
					m.Objects = append(m.Objects, extra)
					data, _ = json.Marshal(m)
					header.Size = int64(len(data))
				}
				if err := tw.WriteHeader(header); err != nil {
					t.Fatal(err)
				}
				if _, err := tw.Write(data); err != nil {
					t.Fatal(err)
				}
				index++
			}
			if err := tw.Close(); err != nil {
				t.Fatal(err)
			}
			destination := testStore(t, Limits{})
			if _, err := destination.Import(context.Background(), &bad); err == nil {
				t.Fatalf("%s archive accepted", attack)
			}
			if _, err := destination.Current(); err == nil {
				t.Fatal("failed import published a snapshot")
			}
		})
	}
}

func TestRepositoryLimitsPathsAndSymlinks(t *testing.T) {
	f := newFixture(t, false)
	for _, limit := range []Limits{{MaxTotalBytes: 10}, {MaxFileBytes: 10}, {MaxIndexBytes: 10}, {MaxObjects: 2}, {MaxManifestBytes: 10}} {
		s := testStore(t, limit)
		if _, err := s.Prefetch(context.Background(), f.selection(false)); err == nil {
			t.Fatalf("limit accepted: %+v", limit)
		}
		if _, err := s.Current(); err == nil {
			t.Fatal("limited partial set published")
		}
	}
	for _, suffix := range []string{"/../bad", "/%2e%2e/bad", "?token=secret", "#fragment"} {
		selection := f.selection(false)
		selection.URL += suffix
		if err := validateSelection(selection); err == nil {
			t.Fatalf("unsafe URL accepted: %s", suffix)
		}
	}
	selection := f.selection(false)
	selection.URL = "http://user:secret@example.invalid/repo"
	if err := validateSelection(selection); err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatal("credential URL leaked or accepted")
	}
	root := t.TempDir()
	if err := os.Symlink(t.TempDir(), filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(filepath.Join(root, "linked", "snapshot"), Limits{}, nil); err == nil {
		t.Fatal("symlink directory accepted")
	}
	s := testStore(t, Limits{})
	m, err := s.Prefetch(context.Background(), f.selection(false))
	if err != nil {
		t.Fatal(err)
	}
	o := m.Objects[0]
	if err := os.Remove(s.filename(o.SHA256)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "target"), s.filename(o.SHA256)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Verify(context.Background()); err == nil {
		t.Fatal("symlink object accepted")
	}
}

func TestInReleaseAndXZIndex(t *testing.T) {
	if _, err := exec.LookPath("xz"); err != nil {
		t.Skip("xz is not available")
	}
	f := newFixture(t, false)
	plain := f.files["dists/stable/main/binary-amd64/Packages"]
	cmd := exec.Command("xz", "--compress", "--stdout")
	cmd.Stdin = bytes.NewReader(plain)
	compressed, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	f.files["dists/stable/main/binary-amd64/Packages.xz"] = compressed
	f.release()
	f.files["dists/stable/InRelease"] = []byte("-----BEGIN PGP SIGNED MESSAGE-----\nHash: SHA256\n\n" + string(f.files["dists/stable/Release"]) + "-----BEGIN PGP SIGNATURE-----\nfixture\n-----END PGP SIGNATURE-----\n")
	s := testStore(t, Limits{})
	m, err := s.Prefetch(context.Background(), f.selection(false))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := objectMap(m)["dists/stable/InRelease"]; !ok {
		t.Fatal("InRelease not retained")
	}
	if _, ok := objectMap(m)["dists/stable/Release"]; ok {
		t.Fatal("unexpected fallback Release")
	}
	if _, err := s.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestAllCompressionVariantsContributeDependencies(t *testing.T) {
	f := newFixture(t, false)
	extra := []byte("second package is present only in gzip index")
	f.files["pool/second.deb"] = extra
	additional := fmt.Sprintf("Package: second\nFilename: pool/second.deb\nSize: %d\nSHA256: %x\n\n", len(extra), sha256.Sum256(extra))
	plain := f.files["dists/stable/main/binary-amd64/Packages"]
	f.files["dists/stable/main/binary-amd64/Packages.gz"] = gzData(t, append(append([]byte(nil), plain...), []byte(additional)...))
	f.release()
	s := testStore(t, Limits{})
	m, err := s.Prefetch(context.Background(), f.selection(false))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := objectMap(m)["pool/second.deb"]; !ok {
		t.Fatal("preferred plain variant omitted a gzip dependency")
	}
	if _, err := s.Verify(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestMalformedAndOversizedIndexFailsWithoutPublication(t *testing.T) {
	for _, data := range []string{
		"Package: a\nFilename: ../outside\nSize: 0\nSHA256: " + strings.Repeat("0", 64) + "\n",
		"Package: a\nFilename: pool/a.deb\nFilename: pool/b.deb\nSize: 0\nSHA256: " + strings.Repeat("0", 64) + "\n",
		"Package: a\nFilename: pool/a.deb\nSize: -1\nSHA256: " + strings.Repeat("0", 64) + "\n",
		strings.Repeat("A", 1<<20) + "\n",
	} {
		f := newFixture(t, false)
		f.files["dists/stable/main/binary-amd64/Packages"] = []byte(data)
		f.files["dists/stable/main/binary-amd64/Packages.gz"] = gzData(t, []byte(data))
		f.release()
		s := testStore(t, Limits{})
		if _, err := s.Prefetch(context.Background(), f.selection(false)); err == nil {
			t.Fatal("malformed index accepted")
		}
		if _, err := s.Current(); err == nil {
			t.Fatal("malformed index published")
		}
	}
	f := newFixture(t, false)
	// Small compressed bytes cannot bypass the decompressed parsing limit.
	delete(f.files, "dists/stable/main/binary-amd64/Packages")
	f.files["dists/stable/main/binary-amd64/Packages.gz"] = gzData(t, bytes.Repeat([]byte("\n"), 4096))
	f.release()
	s := testStore(t, Limits{MaxIndexBytes: 1024})
	if _, err := s.Prefetch(context.Background(), f.selection(false)); err == nil {
		t.Fatal("gzip expansion exceeded parsing bound")
	}
}

func TestRedirectCannotIntroduceCredentialsOrCrossOrigin(t *testing.T) {
	var externalHits int
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { externalHits++; _, _ = w.Write([]byte("unexpected")) }))
	defer external.Close()
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, external.URL+"/metadata?token=secret", http.StatusFound)
	}))
	defer origin.Close()
	s := testStore(t, Limits{})
	selection := Selection{URL: origin.URL, Suite: "stable", Components: []string{"main"}, Architectures: []string{"amd64"}}
	_, err := s.Prefetch(context.Background(), selection)
	if err == nil || strings.Contains(err.Error(), "secret") || externalHits != 0 {
		t.Fatalf("redirect boundary or error redaction: %v hits %d", err, externalHits)
	}
}
