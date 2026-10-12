// Package repository creates verified, complete snapshots of selected APT indexes
// and the binary/source objects they reference. APT clients verify signatures.
package repository

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Selection identifies one repository and the indexes to include.
type Selection struct {
	URL           string   `json:"url"`
	Suite         string   `json:"suite"`
	Components    []string `json:"components"`
	Architectures []string `json:"architectures"`
	Sources       bool     `json:"sources"`
}

// Limits bounds a snapshot and decompressed index parsing. Zero selects defaults.
type Limits struct {
	MaxFileBytes     int64 `json:"max_file_bytes"`
	MaxTotalBytes    int64 `json:"max_total_bytes"`
	MaxIndexBytes    int64 `json:"max_index_bytes"`
	MaxManifestBytes int64 `json:"max_manifest_bytes"`
	MaxObjects       int   `json:"max_objects"`
}

// Object maps a repository-relative path to immutable verified content.
type Object struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// Manifest records the complete selected set, including by-hash aliases.
type Manifest struct {
	Version   int       `json:"version"`
	Selection Selection `json:"selection"`
	Created   time.Time `json:"created"`
	Objects   []Object  `json:"objects"`
}

// Store keeps content-addressed objects and atomically published manifests.
type Store struct {
	dir    string
	root   *os.Root
	limits Limits
	client *http.Client
}

const manifestVersion = 1

// Open prepares a snapshot directory. Symlinks are rejected in its ancestry.
func Open(dir string, limits Limits, client *http.Client) (*Store, error) {
	limits = defaultLimits(limits)
	if limits.MaxFileBytes <= 0 || limits.MaxTotalBytes <= 0 || limits.MaxIndexBytes <= 0 || limits.MaxManifestBytes <= 0 || limits.MaxObjects <= 0 {
		return nil, errors.New("repository limits must be positive")
	}
	if limits.MaxFileBytes == math.MaxInt64 || limits.MaxIndexBytes == math.MaxInt64 || limits.MaxManifestBytes == math.MaxInt64 {
		return nil, errors.New("repository read limits are too large")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if err := safeDirectory(abs); err != nil {
		return nil, err
	}
	for _, name := range []string{"objects", "manifests"} {
		if err := safeDirectory(filepath.Join(abs, name)); err != nil {
			return nil, err
		}
	}
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Minute}
	}
	// Redirects must stay within the repository authority, without credentials.
	clone := *client
	previous := clone.CheckRedirect
	clone.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 || req.URL.User != nil || req.URL.RawQuery != "" || req.URL.Fragment != "" || req.URL.RawPath != "" || req.URL.Scheme != via[0].URL.Scheme || req.URL.Host != via[0].URL.Host {
			return errors.New("repository redirect left its origin")
		}
		if previous != nil {
			return previous(req, via)
		}
		return nil
	}
	root, err := os.OpenRoot(abs)
	if err != nil {
		return nil, err
	}
	return &Store{dir: abs, root: root, limits: limits, client: &clone}, nil
}

// Close releases the directory handle. Handlers must stop before closing a store.
func (s *Store) Close() error { return s.root.Close() }

func (s *Store) checkDirectories() error {
	for _, name := range []string{"objects", "manifests"} {
		info, err := s.root.Lstat(name)
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("snapshot subdirectory is not a regular directory")
		}
	}
	return nil
}

func (s *Store) temporaryFile(dir, prefix string) (*os.File, string, error) {
	if err := s.checkDirectories(); err != nil {
		return nil, "", err
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, "", err
	}
	name := path.Join(dir, prefix+hex.EncodeToString(random[:]))
	f, err := s.root.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o600)
	return f, name, err
}

func defaultLimits(l Limits) Limits {
	if l.MaxFileBytes == 0 {
		l.MaxFileBytes = 2 << 30
	}
	if l.MaxTotalBytes == 0 {
		l.MaxTotalBytes = 128 << 30
	}
	if l.MaxIndexBytes == 0 {
		l.MaxIndexBytes = 128 << 20
	}
	if l.MaxManifestBytes == 0 {
		l.MaxManifestBytes = 16 << 20
	}
	if l.MaxObjects == 0 {
		l.MaxObjects = 1000000
	}
	return l
}

func safeDirectory(dir string) error {
	parts := strings.Split(filepath.Clean(dir), string(filepath.Separator))
	current := string(filepath.Separator)
	for _, part := range parts {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			if err := os.Mkdir(current, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
				return err
			}
			info, err = os.Lstat(current)
		}
		if err != nil {
			return err
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("snapshot directory contains a symlink or non-directory")
		}
	}
	return nil
}

func validateSelection(s Selection) error {
	u, err := url.Parse(s.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.RawPath != "" {
		return errors.New("repository URL must be HTTP(S) without credentials, escapes, query or fragment")
	}
	if u.Path != "" && u.Path != "/" && !safePath(strings.Trim(u.Path, "/")) {
		return errors.New("unsafe repository URL path")
	}
	if !safePath(s.Suite) || len(s.Components) == 0 || len(s.Architectures) == 0 {
		return errors.New("suite, components and architectures are required")
	}
	for _, values := range [][]string{s.Components, s.Architectures} {
		seen := make(map[string]bool)
		for _, v := range values {
			if !safePath(v) || strings.Contains(v, "/") || seen[v] {
				return errors.New("invalid or duplicate component/architecture")
			}
			seen[v] = true
		}
	}
	return nil
}

func safePath(p string) bool {
	if p == "" || strings.HasPrefix(p, "/") || strings.ContainsAny(p, "\\%?#\x00\r\n\t ") || path.Clean(p) != p {
		return false
	}
	for _, part := range strings.Split(p, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func validDigest(d string) bool {
	if len(d) != 64 || d != strings.ToLower(d) {
		return false
	}
	_, err := hex.DecodeString(d)
	return err == nil
}

func (s *Store) filename(digest string) string { return filepath.Join(s.dir, "objects", digest) }

func (s *Store) openObject(o Object) (*os.File, error) {
	if !validDigest(o.SHA256) || o.Size < 0 || o.Size > s.limits.MaxFileBytes {
		return nil, errors.New("invalid object descriptor")
	}
	if err := s.checkDirectories(); err != nil {
		return nil, err
	}
	info, err := s.root.Lstat("objects/" + o.SHA256)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() != o.Size {
		return nil, errors.New("object is not a regular file of the expected size")
	}
	f, err := s.root.Open("objects/" + o.SHA256)
	if err != nil {
		return nil, err
	}
	actual, err := f.Stat()
	if err != nil || !os.SameFile(info, actual) {
		_ = f.Close()
		return nil, errors.New("object changed while opening")
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, o.Size+1))
	if err != nil || n != o.Size || hex.EncodeToString(h.Sum(nil)) != o.SHA256 {
		_ = f.Close()
		return nil, errors.New("object checksum mismatch")
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

func (s *Store) hasObject(o Object) bool {
	f, err := s.openObject(o)
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}

func (s *Store) saveObject(ctx context.Context, reader io.Reader, expected *Object) (Object, error) {
	f, name, err := s.temporaryFile("objects", ".download-")
	if err != nil {
		return Object{}, err
	}
	defer func() { _ = f.Close(); _ = s.root.Remove(name) }()
	h := sha256.New()
	limit := s.limits.MaxFileBytes
	if expected == nil {
		// Bootstrap Release/signature files are metadata, not arbitrary payloads.
		if s.limits.MaxIndexBytes < limit {
			limit = s.limits.MaxIndexBytes
		}
		if s.limits.MaxTotalBytes < limit {
			limit = s.limits.MaxTotalBytes
		}
	}
	if expected != nil && expected.Size < limit {
		limit = expected.Size
	}
	n, err := io.Copy(io.MultiWriter(f, h), &contextReader{ctx: ctx, r: io.LimitReader(reader, limit+1)})
	if err != nil {
		return Object{}, err
	}
	if n > limit {
		return Object{}, errors.New("object exceeds its size limit")
	}
	o := Object{SHA256: hex.EncodeToString(h.Sum(nil)), Size: n}
	if expected != nil {
		o.Path = expected.Path
		if n != expected.Size || o.SHA256 != expected.SHA256 {
			return Object{}, fmt.Errorf("size or SHA256 mismatch: %s", expected.Path)
		}
	}
	if err := f.Sync(); err != nil {
		return Object{}, err
	}
	if err := f.Close(); err != nil {
		return Object{}, err
	}
	if err := s.root.Rename(name, "objects/"+o.SHA256); err != nil {
		return Object{}, err
	}
	dir, err := s.root.Open("objects")
	if err != nil {
		return Object{}, err
	}
	defer func() { _ = dir.Close() }()
	if err := dir.Sync(); err != nil {
		return Object{}, err
	}
	return o, nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

func (s *Store) download(ctx context.Context, selection Selection, p string, expected *Object) (Object, error) {
	if !safePath(p) {
		return Object{}, errors.New("unsafe repository object path")
	}
	u, err := url.Parse(strings.TrimRight(selection.URL, "/") + "/" + p)
	if err != nil {
		return Object{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Object{}, err
	}
	// Release checksums describe the wire object, never HTTP-decoded content.
	req.Header.Set("Accept-Encoding", "identity")
	res, err := s.client.Do(req)
	if err != nil {
		return Object{}, fmt.Errorf("download failed: %s", p)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode == http.StatusNotFound {
		return Object{}, errNotFound
	}
	encoding := strings.TrimSpace(res.Header.Get("Content-Encoding"))
	if res.StatusCode != http.StatusOK || (encoding != "" && !strings.EqualFold(encoding, "identity")) {
		return Object{}, fmt.Errorf("unexpected response for %s: %d", p, res.StatusCode)
	}
	o, err := s.saveObject(ctx, res.Body, expected)
	o.Path = p
	return o, err
}

var errNotFound = errors.New("repository object not found")

// Prefetch downloads every object referenced by the selected indexes. Publication
// happens only after a second closure/integrity check of the complete set.
func (s *Store) Prefetch(ctx context.Context, selection Selection) (*Manifest, error) {
	if err := validateSelection(selection); err != nil {
		return nil, err
	}
	m := &Manifest{Version: manifestVersion, Selection: selection, Created: time.Now().UTC()}
	base := "dists/" + selection.Suite + "/"
	o, err := s.download(ctx, selection, base+"InRelease", nil)
	if errors.Is(err, errNotFound) {
		o, err = s.download(ctx, selection, base+"Release", nil)
		if err == nil {
			sig, sigErr := s.download(ctx, selection, base+"Release.gpg", nil)
			if sigErr == nil {
				m.Objects = append(m.Objects, sig)
			} else if !errors.Is(sigErr, errNotFound) {
				return nil, sigErr
			}
		}
	}
	if err != nil {
		return nil, err
	}
	m.Objects = append(m.Objects, o)
	if err := s.validateManifest(m); err != nil {
		return nil, err
	}
	indexes, err := s.selectedIndexes(ctx, m)
	if err != nil {
		return nil, err
	}
	for _, index := range indexes {
		m.Objects = append(m.Objects, index)
		alias := index
		alias.Path = path.Dir(index.Path) + "/by-hash/SHA256/" + index.SHA256
		m.Objects = append(m.Objects, alias)
	}
	if err := s.validateManifest(m); err != nil {
		return nil, err
	}
	for _, index := range indexes {
		if !s.hasObject(index) {
			if _, err := s.download(ctx, selection, index.Path, &index); err != nil {
				return nil, err
			}
		}
	}
	objects, err := s.packageObjects(ctx, indexes)
	if err != nil {
		return nil, err
	}
	m.Objects = append(m.Objects, objects...)
	if err := s.validateManifest(m); err != nil {
		return nil, err
	}
	for _, object := range objects {
		if !s.hasObject(object) {
			if _, err := s.download(ctx, selection, object.Path, &object); err != nil {
				return nil, err
			}
		}
	}
	m.Objects, err = uniqueObjects(m.Objects)
	if err != nil {
		return nil, err
	}
	if _, err := s.verify(ctx, m); err != nil {
		return nil, err
	}
	if err := s.publish(m); err != nil {
		return nil, err
	}
	return m, nil
}

func uniqueObjects(objects []Object) ([]Object, error) {
	byPath := make(map[string]Object, len(objects))
	for _, o := range objects {
		if prev, ok := byPath[o.Path]; ok && (prev.SHA256 != o.SHA256 || prev.Size != o.Size) {
			return nil, fmt.Errorf("conflicting object descriptor: %s", o.Path)
		}
		byPath[o.Path] = o
	}
	result := make([]Object, 0, len(byPath))
	for _, o := range byPath {
		result = append(result, o)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Path < result[j].Path })
	return result, nil
}

func (s *Store) validateManifest(m *Manifest) error {
	if m.Version != manifestVersion {
		return errors.New("unsupported manifest version")
	}
	if err := validateSelection(m.Selection); err != nil {
		return err
	}
	if len(m.Objects) > s.limits.MaxObjects {
		return errors.New("snapshot exceeds object limit")
	}
	seen := make(map[string]int64)
	var total int64
	header := *m
	header.Objects = nil
	headerData, err := json.Marshal(header)
	if err != nil {
		return err
	}
	manifestBytes := int64(len(headerData) - 2) // Replace null with [].
	for i, o := range m.Objects {
		if !safePath(o.Path) || !validDigest(o.SHA256) || o.Size < 0 || o.Size > s.limits.MaxFileBytes {
			return errors.New("invalid manifest object")
		}
		encoded, err := json.Marshal(o)
		if err != nil {
			return err
		}
		additional := int64(len(encoded))
		if i > 0 {
			additional++
		}
		if additional > s.limits.MaxManifestBytes-manifestBytes {
			return errors.New("manifest exceeds size limit")
		}
		manifestBytes += additional
		if previous, ok := seen[o.SHA256]; ok {
			if previous != o.Size {
				return errors.New("inconsistent object sizes")
			}
			continue
		}
		if o.Size > s.limits.MaxTotalBytes-total {
			return errors.New("snapshot exceeds total byte limit")
		}
		total += o.Size
		seen[o.SHA256] = o.Size
	}
	if manifestBytes > s.limits.MaxManifestBytes {
		return errors.New("manifest exceeds size limit")
	}
	return nil
}

func (s *Store) publish(m *Manifest) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if int64(len(data)) > s.limits.MaxManifestBytes {
		return errors.New("manifest exceeds size limit")
	}
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	if err := s.atomicFile("manifests/"+digest+".json", data); err != nil {
		return err
	}
	return s.atomicFile("current", []byte(digest+"\n"))
}

func (s *Store) atomicFile(filename string, data []byte) error {
	f, name, err := s.temporaryFile(path.Dir(filename), ".publish-")
	if err != nil {
		return err
	}
	defer func() { _ = f.Close(); _ = s.root.Remove(name) }()
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := s.root.Rename(name, filename); err != nil {
		return err
	}
	dir, err := s.root.Open(path.Dir(filename))
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	return dir.Sync()
}

func (s *Store) readRegular(filename string, limit int64) ([]byte, error) {
	if err := s.checkDirectories(); err != nil {
		return nil, err
	}
	info, err := s.root.Lstat(filename)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, errors.New("metadata is not a bounded regular file")
	}
	f, err := s.root.Open(filename)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	actual, err := f.Stat()
	if err != nil || !os.SameFile(info, actual) {
		return nil, errors.New("metadata changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if int64(len(data)) > limit {
		return nil, errors.New("metadata grew beyond size limit")
	}
	return data, err
}

// Current loads the published, hash-checked manifest, without trusting its completeness.
func (s *Store) Current() (*Manifest, error) {
	pointer, err := s.readRegular("current", 65)
	if err != nil {
		return nil, err
	}
	digest := strings.TrimSuffix(string(pointer), "\n")
	if !validDigest(digest) {
		return nil, errors.New("invalid current snapshot pointer")
	}
	data, err := s.readRegular("manifests/"+digest+".json", s.limits.MaxManifestBytes)
	if err != nil {
		return nil, err
	}
	if fmt.Sprintf("%x", sha256.Sum256(data)) != digest {
		return nil, errors.New("manifest checksum mismatch")
	}
	return s.decodeManifest(data)
}

func (s *Store) decodeManifest(data []byte) (*Manifest, error) {
	if int64(len(data)) > s.limits.MaxManifestBytes {
		return nil, errors.New("manifest exceeds size limit")
	}
	var m Manifest
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&m); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return nil, errors.New("trailing manifest data")
	}
	if err := s.validateManifest(&m); err != nil {
		return nil, err
	}
	unique, err := uniqueObjects(m.Objects)
	if err != nil || len(unique) != len(m.Objects) {
		return nil, errors.New("duplicate manifest paths")
	}
	return &m, nil
}
