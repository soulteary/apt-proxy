package repository

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func requireRepoError(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("expected rejection")
	}
}
func storeBytes(t *testing.T, s *Store, p string, b []byte) Object {
	t.Helper()
	o, err := s.saveObject(context.Background(), bytes.NewReader(b), nil)
	if err != nil {
		t.Fatal(err)
	}
	o.Path = p
	return o
}
func TestRepositoryDescriptorAndManifestValidation(t *testing.T) {
	s := testStore(t, Limits{})
	selection := Selection{URL: "https://example.org/repo", Suite: "stable", Components: []string{"main"}, Architectures: []string{"amd64"}}
	for _, mutate := range []func(*Selection){func(v *Selection) { v.Suite = "" }, func(v *Selection) { v.Components = nil }, func(v *Selection) { v.Architectures = []string{"amd64", "amd64"} }, func(v *Selection) { v.Components = []string{"main/other"} }} {
		v := selection
		mutate(&v)
		requireRepoError(t, validateSelection(v))
		_, err := s.Prefetch(context.Background(), v)
		requireRepoError(t, err)
	}
	for _, l := range []Limits{{MaxObjects: -1}, {MaxFileBytes: math.MaxInt64}, {MaxManifestBytes: math.MaxInt64}, {MaxIndexBytes: math.MaxInt64}} {
		_, err := Open(t.TempDir(), l, nil)
		requireRepoError(t, err)
	}
	for _, d := range []string{"", strings.Repeat("G", 64), strings.Repeat("A", 64)} {
		if validDigest(d) {
			t.Fatal("invalid digest accepted")
		}
	}
	good := Object{Path: "pool/a.deb", SHA256: strings.Repeat("a", 64), Size: 1}
	for _, m := range []*Manifest{{Version: 2, Selection: selection}, {Version: 1}, {Version: 1, Selection: selection, Objects: []Object{{Path: "../bad", SHA256: good.SHA256}}}, {Version: 1, Selection: selection, Objects: []Object{good, {Path: "pool/b.deb", SHA256: good.SHA256, Size: 2}}}, {Version: 1, Selection: selection, Created: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)}} {
		requireRepoError(t, s.validateManifest(m))
	}
	m := &Manifest{Version: 1, Selection: selection, Objects: []Object{good}}
	b, _ := json.Marshal(m)
	for _, data := range [][]byte{[]byte("{"), append(append([]byte(nil), b...), []byte(" {}")...), []byte(`{"unknown":true}`)} {
		_, err := s.decodeManifest(data)
		requireRepoError(t, err)
	}
	m.Objects = append(m.Objects, good)
	b, _ = json.Marshal(m)
	_, err := s.decodeManifest(b)
	requireRepoError(t, err)
	_, err = uniqueObjects([]Object{good, {Path: good.Path, SHA256: strings.Repeat("b", 64), Size: 1}})
	requireRepoError(t, err)
	small := testStore(t, Limits{MaxTotalBytes: 1})
	m.Objects = []Object{good, {Path: "pool/b.deb", SHA256: strings.Repeat("b", 64), Size: 1}}
	requireRepoError(t, small.validateManifest(m))
	tiny := testStore(t, Limits{MaxManifestBytes: 1})
	_, err = tiny.decodeManifest(b)
	requireRepoError(t, err)
	requireRepoError(t, tiny.publish(m))
	m.Objects = nil
	requireRepoError(t, tiny.validateManifest(m))
}
func TestRepositoryMetadataFailures(t *testing.T) {
	s := testStore(t, Limits{})
	ctx := context.Background()
	for _, f := range []func() error{func() error { _, e := s.Verify(ctx); return e }, func() error { return s.Repair(ctx, nil) }, func() error { return s.Export(ctx, io.Discard) }, func() error { _, e := s.Handler(ctx); return e }} {
		requireRepoError(t, f())
	}
	for _, data := range []string{"bad\n", strings.Repeat("a", 64) + "\n"} {
		if err := os.WriteFile(filepath.Join(s.dir, "current"), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		_, err := s.Current()
		requireRepoError(t, err)
	}
	digest := strings.Repeat("a", 64)
	if err := os.WriteFile(filepath.Join(s.dir, "manifests", digest+".json"), []byte("bad"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := s.Current()
	requireRepoError(t, err)
	if _, err = s.readRegular("manifests", 100); err == nil {
		t.Fatal("directory metadata accepted")
	}
	for _, o := range []Object{{}, {SHA256: digest, Size: -1}, {SHA256: digest, Size: s.limits.MaxFileBytes + 1}} {
		_, err := s.openObject(o)
		requireRepoError(t, err)
	}
	_, err = s.objectBytes(Object{Size: 2}, 1)
	requireRepoError(t, err)
	_, err = s.objectBytes(Object{SHA256: digest}, 10)
	requireRepoError(t, err)
	if err := os.Remove(filepath.Join(s.dir, "objects")); err != nil {
		t.Fatal(err)
	}
	requireRepoError(t, s.checkDirectories())
	_, err = s.saveObject(ctx, strings.NewReader("x"), nil)
	requireRepoError(t, err)
	requireRepoError(t, s.atomicFile("current", nil))
	_, err = s.readRegular("current", 65)
	requireRepoError(t, err)
	_, err = s.openObject(Object{SHA256: digest})
	requireRepoError(t, err)
	if err := os.WriteFile(filepath.Join(s.dir, "objects"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	requireRepoError(t, s.checkDirectories())
}
func TestRepositoryParsingRejectsMalformedMetadata(t *testing.T) {
	for _, v := range []string{" continuation\n", "bad\n", ": empty\n", "bad field: x\n"} {
		_, err := controlStanzas(v)
		requireRepoError(t, err)
	}
	for _, v := range []string{"-----BEGIN PGP SIGNED MESSAGE-----", "-----BEGIN PGP SIGNED MESSAGE-----\n\nhello"} {
		_, err := releasePayload(v)
		requireRepoError(t, err)
	}
	payload, err := releasePayload("-----BEGIN PGP SIGNED MESSAGE-----\r\nHash: SHA256\r\n\r\n- -escaped\r\n-----BEGIN PGP SIGNATURE-----\n-----END PGP SIGNATURE-----")
	if err != nil || payload != "-escaped\n" {
		t.Fatalf("cleartext: %q %v", payload, err)
	}
	for _, v := range []string{"", "bad 1 file", strings.Repeat("a", 64) + " -1 file", strings.Repeat("a", 64) + " huge file"} {
		_, err := checksumLines(v, "")
		requireRepoError(t, err)
	}
	s := testStore(t, Limits{})
	selection := Selection{Suite: "stable", Components: []string{"main"}, Architectures: []string{"amd64"}}
	for _, data := range []string{"invalid", "Suite: stable\n\nSuite: other\n", "SHA256:\n", "-----BEGIN PGP SIGNED MESSAGE-----", fmt.Sprintf("SHA256:\n %s 1 other/Packages\n", strings.Repeat("a", 64))} {
		o := storeBytes(t, s, "dists/stable/Release", []byte(data))
		_, err := s.selectedIndexes(context.Background(), &Manifest{Selection: selection, Objects: []Object{o}})
		requireRepoError(t, err)
	}
	_, err = s.selectedIndexes(context.Background(), &Manifest{Selection: selection})
	requireRepoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = s.selectedIndexes(ctx, &Manifest{})
	requireRepoError(t, err)
	for _, tc := range []struct{ path, data string }{{"Packages.gz", "not gzip"}, {"Packages", " invalid"}, {"Sources", "Directory: ../unsafe\n"}, {"Sources", "Directory: pool/a\nChecksums-Sha256: bad\n"}} {
		o := storeBytes(t, s, tc.path, []byte(tc.data))
		_, err := s.packageObjects(context.Background(), []Object{o})
		requireRepoError(t, err)
	}
	o := storeBytes(t, s, "Packages", []byte("Package: a\n"))
	_, err = s.decodedIndex(ctx, o)
	requireRepoError(t, err)
	_, err = s.decodedIndex(context.Background(), Object{SHA256: strings.Repeat("a", 64)})
	requireRepoError(t, err)
	o = storeBytes(t, s, "Packages.xz", []byte("bad xz"))
	_, err = s.decodedIndex(context.Background(), o)
	requireRepoError(t, err)
	t.Setenv("PATH", t.TempDir())
	_, err = s.decodedIndex(context.Background(), o)
	if err == nil || !strings.Contains(err.Error(), "xz executable") {
		t.Fatalf("missing decoder: %v", err)
	}
}
func TestRepositoryNetworkAndCancellationFailures(t *testing.T) {
	for _, tc := range []struct {
		status   int
		encoding string
	}{{500, ""}, {200, "gzip"}} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Encoding", tc.encoding)
			w.WriteHeader(tc.status)
		}))
		s := testStore(t, Limits{})
		_, err := s.download(context.Background(), Selection{URL: server.URL}, "Release", nil)
		requireRepoError(t, err)
		server.Close()
	}
	s := testStore(t, Limits{})
	for _, tc := range []struct{ url, path string }{{"https://example.org", "../escape"}, {"http://%", "Release"}, {"http://example.org/\x00", "Release"}} {
		_, err := s.download(context.Background(), Selection{URL: tc.url}, tc.path, nil)
		requireRepoError(t, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := s.saveObject(ctx, strings.NewReader("x"), nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("save cancellation: %v", err)
	}
	f := newFixture(t, false)
	m, err := s.Prefetch(context.Background(), f.selection(false))
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.verify(ctx, m)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("verify cancellation: %v", err)
	}
	requireRepoError(t, s.Repair(context.Background(), []string{"not/in/snapshot"}))
	o := objectMap(m)["pool/main/h/hello/hello_1_amd64.deb"]
	if err := os.Remove(s.filename(o.SHA256)); err != nil {
		t.Fatal(err)
	}
	f.server.Close()
	requireRepoError(t, s.Repair(context.Background(), nil))
}
func TestRepositoryHTTPRejectsUnsafeRequests(t *testing.T) {
	f := newFixture(t, false)
	s := testStore(t, Limits{})
	if _, err := s.Prefetch(context.Background(), f.selection(false)); err != nil {
		t.Fatal(err)
	}
	h, err := s.Handler(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		method, path string
		status       int
	}{{"POST", "/pool/a", 405}, {"GET", "/pool/a?key=x", 400}, {"GET", "/../escape", 400}, {"GET", "/pool/%61", 400}} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		if w.Code != tc.status {
			t.Fatalf("%s: got %d", tc.path, w.Code)
		}
	}
}
func TestRepositoryArchivesRejectBadFraming(t *testing.T) {
	s := testStore(t, Limits{})
	for _, data := range [][]byte{nil, []byte("invalid tar")} {
		_, err := s.Import(context.Background(), bytes.NewReader(data))
		requireRepoError(t, err)
	}
	var b bytes.Buffer
	tw := tar.NewWriter(&b)
	if err := tw.WriteHeader(&tar.Header{Name: "wrong", Mode: 0600, Size: 0}); err != nil {
		t.Fatal(err)
	}
	_ = tw.Close()
	_, err := s.Import(context.Background(), &b)
	requireRepoError(t, err)
}

// A failing destination must never be reported as a successfully exported archive.
type exportFailureWriter struct {
	remaining int
	err       error
}

func (w *exportFailureWriter) Write(p []byte) (int, error) {
	if len(p) > w.remaining {
		n := w.remaining
		w.remaining = 0
		return n, w.err
	}
	w.remaining -= len(p)
	return len(p), nil
}
func TestRepositoryExportAndPublicationFailures(t *testing.T) {
	f := newFixture(t, false)
	s := testStore(t, Limits{})
	m, err := s.Prefetch(context.Background(), f.selection(false))
	if err != nil {
		t.Fatal(err)
	}
	boom := errors.New("export destination failed")
	for _, remaining := range []int{0, 512, 2048, 2560} {
		err := s.Export(context.Background(), &exportFailureWriter{remaining: remaining, err: boom})
		if !errors.Is(err, boom) {
			t.Fatalf("export budget %d: %v", remaining, err)
		}
	}
	if err := os.Mkdir(filepath.Join(s.dir, "current-dir"), 0700); err != nil {
		t.Fatal(err)
	}
	requireRepoError(t, s.atomicFile("current-dir", nil))
	requireRepoError(t, s.publish(&Manifest{Created: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)}))
	m.Objects[0].Path = "../unsafe"
	_, err = s.verify(context.Background(), m)
	requireRepoError(t, err)
	if err := os.WriteFile(filepath.Join(s.dir, "objects-bad"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	if err := os.WriteFile(filepath.Join(dir, "objects"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	_, err = Open(dir, Limits{}, nil)
	requireRepoError(t, err)
}
func TestRepositoryIndexClosureFailures(t *testing.T) {
	f := newFixture(t, false)
	s := testStore(t, Limits{})
	m, err := s.Prefetch(context.Background(), f.selection(false))
	if err != nil {
		t.Fatal(err)
	}
	sourceSelection := m.Selection
	sourceSelection.Sources = true
	_, err = s.selectedIndexes(context.Background(), &Manifest{Selection: sourceSelection, Objects: m.Objects})
	if err == nil || !strings.Contains(err.Error(), "no Sources") {
		t.Fatalf("missing Sources: %v", err)
	}
	m.Selection.Components = []string{"missing"}
	_, err = s.verify(context.Background(), m)
	requireRepoError(t, err)
	m.Selection.Components = []string{"main"}
	for i, o := range m.Objects {
		if strings.HasSuffix(o.Path, "/by-hash/SHA256/"+o.SHA256) {
			m.Objects[i].Path = "other/alias"
			break
		}
	}
	damaged, err := s.verify(context.Background(), m)
	if err == nil || len(damaged) != 1 {
		t.Fatalf("missing by-hash alias: %v %v", damaged, err)
	}
	p := []byte(fmt.Sprintf("Package: a\nFilename: pool/a.deb\nSize: 0\nSHA256: %s\n\nPackage: b\nFilename: pool/b.deb\nSize: 0\nSHA256: %s\n", strings.Repeat("a", 64), strings.Repeat("b", 64)))
	o := storeBytes(t, s, "Packages", p)
	limited := *s
	limited.limits.MaxObjects = 1
	_, err = limited.packageObjects(context.Background(), []Object{o})
	if err == nil || !strings.Contains(err.Error(), "object limit") {
		t.Fatalf("index object limit: %v", err)
	}
}
func TestRepositoryRedirectAndSignatureFailures(t *testing.T) {
	f := newFixture(t, false)
	s := testStore(t, Limits{})
	first, _ := http.NewRequest("GET", f.server.URL+"/repo/start", nil)
	target, _ := http.NewRequest("GET", f.server.URL+"/repo/final", nil)
	if err := s.client.CheckRedirect(target, []*http.Request{first}); err != nil {
		t.Fatal(err)
	}
	dir, _ := filepath.EvalSymlinks(t.TempDir())
	sentinel := errors.New("redirect policy")
	custom, err := Open(dir, Limits{}, &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return sentinel }})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = custom.Close() }()
	if err := custom.client.CheckRedirect(target, []*http.Request{first}); !errors.Is(err, sentinel) {
		t.Fatal("custom redirect policy lost")
	}
	for _, signatureStatus := range []int{404, 500} {
		origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.HasSuffix(r.URL.Path, "Release.gpg") {
				w.WriteHeader(signatureStatus)
				return
			}
			p := strings.TrimPrefix(r.URL.Path, "/repo/")
			if b, ok := f.files[p]; ok {
				_, _ = w.Write(b)
			} else {
				w.WriteHeader(404)
			}
		}))
		selection := f.selection(false)
		selection.URL = origin.URL + "/repo"
		_, err := s.Prefetch(context.Background(), selection)
		if signatureStatus == 404 && err != nil {
			t.Fatal(err)
		}
		if signatureStatus == 500 && err == nil {
			t.Fatal("signature upstream failure ignored")
		}
		origin.Close()
	}
}

func TestRepositoryFailedObjectCommitAndTruncatedArchive(t *testing.T) {
	s := testStore(t, Limits{})
	data := []byte("cannot replace a directory")
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	if err := os.Mkdir(s.filename(digest), 0700); err != nil {
		t.Fatal(err)
	}
	_, err := s.saveObject(context.Background(), bytes.NewReader(data), nil)
	requireRepoError(t, err)
	f := newFixture(t, false)
	source := testStore(t, Limits{})
	if _, err := source.Prefetch(context.Background(), f.selection(false)); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	if err := source.Export(context.Background(), &archive); err != nil {
		t.Fatal(err)
	}
	for _, cut := range []int{600, 2000, 2500} {
		dest := testStore(t, Limits{})
		_, err := dest.Import(context.Background(), bytes.NewReader(archive.Bytes()[:cut]))
		requireRepoError(t, err)
		if _, err := dest.Current(); err == nil {
			t.Fatal("truncated archive published")
		}
	}
	var b bytes.Buffer
	tw := tar.NewWriter(&b)
	_ = tw.WriteHeader(&tar.Header{Name: "manifest.json", Size: 1, Mode: 0600})
	_, _ = tw.Write([]byte("{"))
	_ = tw.Close()
	_, err = s.Import(context.Background(), &b)
	requireRepoError(t, err)
}
func TestRepositoryMalformedSnapshotAndMissingDownload(t *testing.T) {
	f := newFixture(t, false)
	delete(f.files, "dists/stable/main/binary-amd64/Packages.gz")
	f.release()
	s := testStore(t, Limits{})
	m, err := s.Prefetch(context.Background(), f.selection(false))
	if err != nil {
		t.Fatal(err)
	}
	bad := storeBytes(t, s, "dists/stable/main/binary-amd64/Packages", []byte("bad\n"))
	release := storeBytes(t, s, "dists/stable/Release", []byte(fmt.Sprintf("SHA256:\n %s %d main/binary-amd64/Packages\n", bad.SHA256, bad.Size)))
	alias := bad
	alias.Path = "dists/stable/main/binary-amd64/by-hash/SHA256/" + bad.SHA256
	m.Objects = []Object{release, bad, alias}
	_, err = s.verify(context.Background(), m)
	requireRepoError(t, err)
	missing := Object{Path: "dists/stable/Release", SHA256: strings.Repeat("a", 64), Size: 1}
	_, err = s.selectedIndexes(context.Background(), &Manifest{Selection: m.Selection, Objects: []Object{missing}})
	requireRepoError(t, err)
	f.mu.Lock()
	delete(f.files, "dists/stable/main/binary-amd64/Packages")
	f.mu.Unlock()
	fresh := testStore(t, Limits{})
	_, err = fresh.Prefetch(context.Background(), f.selection(false))
	requireRepoError(t, err)
}
func TestRepositoryXZExpansionIsBounded(t *testing.T) {
	if _, err := exec.LookPath("xz"); err != nil {
		t.Skip("xz unavailable")
	}
	s := testStore(t, Limits{})
	cmd := exec.Command("xz", "--compress", "--stdout")
	cmd.Stdin = bytes.NewReader(bytes.Repeat([]byte("\n"), 4096))
	compressed, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	o := storeBytes(t, s, "Packages.xz", compressed)
	s.limits.MaxIndexBytes = 100
	_, err = s.decodedIndex(context.Background(), o)
	if err == nil || !strings.Contains(err.Error(), "exceeds limit") {
		t.Fatalf("xz expansion: %v", err)
	}
}

func TestRepositoryPrefetchFailureStagesNeverPublish(t *testing.T) {
	for _, stage := range []string{"release", "object-limit", "publish"} {
		t.Run(stage, func(t *testing.T) {
			f := newFixture(t, false)
			limits := Limits{}
			if stage == "object-limit" {
				limits.MaxObjects = 6
			}
			s := testStore(t, limits)
			if stage == "release" {
				f.files["dists/stable/Release"] = []byte("bad Release fields")
			}
			if stage == "publish" {
				if err := os.Mkdir(filepath.Join(s.dir, "current"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			_, err := s.Prefetch(context.Background(), f.selection(false))
			requireRepoError(t, err)
			if _, err := s.Current(); err == nil {
				t.Fatal("failed prefetch published")
			}
		})
	}
	s := testStore(t, Limits{})
	_, err := s.decodeManifest([]byte(`{"version":2}`))
	requireRepoError(t, err)
	f := newFixture(t, false)
	m, err := s.Prefetch(context.Background(), f.selection(false))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(s.filename(m.Objects[0].SHA256)); err != nil {
		t.Fatal(err)
	}
	requireRepoError(t, s.Export(context.Background(), io.Discard))
}
