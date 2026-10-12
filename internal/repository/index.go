package repository

import (
	"bufio"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path"
	"strconv"
	"strings"
)

func (s *Store) objectBytes(o Object, limit int64) ([]byte, error) {
	if o.Size > limit {
		return nil, errors.New("metadata exceeds parsing limit")
	}
	f, err := s.openObject(o)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return io.ReadAll(io.LimitReader(f, limit+1))
}

// controlStanzas parses Debian control fields, including folded multiline fields.
func controlStanzas(data string) ([]map[string]string, error) {
	scanner := bufio.NewScanner(strings.NewReader(data))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	var stanzas []map[string]string
	fields := make(map[string]string)
	var last string
	for scanner.Scan() {
		line := strings.TrimSuffix(scanner.Text(), "\r")
		if line == "" {
			if len(fields) > 0 {
				stanzas = append(stanzas, fields)
				fields = make(map[string]string)
			}
			last = ""
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			if last == "" {
				return nil, errors.New("control continuation without field")
			}
			fields[last] += "\n" + strings.TrimSpace(line)
			continue
		}
		name, value, ok := strings.Cut(line, ":")
		name = strings.ToLower(name)
		if !ok || name == "" || strings.ContainsAny(name, " \t") {
			return nil, errors.New("invalid control field")
		}
		if _, duplicate := fields[name]; duplicate {
			return nil, errors.New("duplicate control field")
		}
		fields[name] = strings.TrimSpace(value)
		last = name
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(fields) > 0 {
		stanzas = append(stanzas, fields)
	}
	return stanzas, nil
}

func releasePayload(data string) (string, error) {
	if !strings.HasPrefix(data, "-----BEGIN PGP SIGNED MESSAGE-----") {
		return data, nil
	}
	data = strings.ReplaceAll(data, "\r\n", "\n")
	_, rest, ok := strings.Cut(data, "\n\n")
	if !ok {
		return "", errors.New("invalid InRelease cleartext header")
	}
	text, signature, ok := strings.Cut(rest, "-----BEGIN PGP SIGNATURE-----")
	if !ok || !strings.Contains(signature, "-----END PGP SIGNATURE-----") {
		return "", errors.New("invalid InRelease signature framing")
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimPrefix(line, "- ")
	}
	return strings.Join(lines, "\n"), nil
}

func checksumLines(value, prefix string) ([]Object, error) {
	var result []Object
	for _, line := range strings.Split(value, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) != 3 || !validDigest(parts[0]) || !safePath(parts[2]) {
			return nil, errors.New("invalid SHA256 reference")
		}
		size, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || size < 0 {
			return nil, errors.New("invalid SHA256 object size")
		}
		result = append(result, Object{Path: prefix + parts[2], SHA256: parts[0], Size: size})
	}
	if len(result) == 0 {
		return nil, errors.New("missing SHA256 references")
	}
	return uniqueObjects(result)
}

func (s *Store) selectedIndexes(ctx context.Context, m *Manifest) ([]Object, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	base := "dists/" + m.Selection.Suite + "/"
	byPath := objectMap(m)
	release, ok := byPath[base+"InRelease"]
	if !ok {
		release, ok = byPath[base+"Release"]
	}
	if !ok {
		return nil, errors.New("snapshot is missing Release metadata")
	}
	data, err := s.objectBytes(release, s.limits.MaxIndexBytes)
	if err != nil {
		return nil, err
	}
	payload, err := releasePayload(string(data))
	if err != nil {
		return nil, err
	}
	stanzas, err := controlStanzas(payload)
	if err != nil || len(stanzas) != 1 {
		return nil, errors.New("invalid Release control fields")
	}
	refs, err := checksumLines(stanzas[0]["sha256"], base)
	if err != nil {
		return nil, err
	}
	available := make(map[string]Object, len(refs))
	for _, ref := range refs {
		available[ref.Path] = ref
	}
	var result []Object
	for _, component := range m.Selection.Components {
		for _, arch := range m.Selection.Architectures {
			indexes := indexVariants(available, base+component+"/binary-"+arch+"/Packages")
			if len(indexes) == 0 {
				return nil, fmt.Errorf("release has no Packages index for %s/%s", component, arch)
			}
			result = append(result, indexes...)
		}
		result = append(result, indexVariants(available, base+component+"/binary-all/Packages")...)
		if m.Selection.Sources {
			indexes := indexVariants(available, base+component+"/source/Sources")
			if len(indexes) == 0 {
				return nil, fmt.Errorf("release has no Sources index for %s", component)
			}
			result = append(result, indexes...)
		}
	}
	return uniqueObjects(result)
}

func indexVariants(available map[string]Object, p string) []Object {
	var result []Object
	for _, suffix := range []string{"", ".gz", ".xz"} {
		if o, ok := available[p+suffix]; ok {
			result = append(result, o)
		}
	}
	return result
}

func (s *Store) decodedIndex(ctx context.Context, o Object) ([]byte, error) {
	f, err := s.openObject(o)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var reader io.Reader = f
	if strings.HasSuffix(o.Path, ".gz") {
		gz, err := gzip.NewReader(f)
		if err != nil {
			return nil, err
		}
		defer func() { _ = gz.Close() }()
		reader = gz
	} else if strings.HasSuffix(o.Path, ".xz") {
		// xz is optional. No shell is involved, and memory/output are bounded.
		decodeCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		cmd := exec.CommandContext(decodeCtx, "xz", "--decompress", "--stdout", "--memlimit-decompress=256MiB")
		cmd.Stdin = f
		pipe, err := cmd.StdoutPipe()
		if err != nil {
			return nil, err
		}
		if err := cmd.Start(); err != nil {
			return nil, errors.New("selected xz index requires the xz executable")
		}
		data, readErr := io.ReadAll(io.LimitReader(pipe, s.limits.MaxIndexBytes+1))
		if int64(len(data)) > s.limits.MaxIndexBytes || readErr != nil {
			cancel()
		}
		waitErr := cmd.Wait()
		if readErr != nil {
			return nil, readErr
		}
		if int64(len(data)) > s.limits.MaxIndexBytes {
			return nil, errors.New("decompressed index exceeds limit")
		}
		if waitErr != nil {
			return nil, errors.New("xz index decompression failed")
		}
		return data, nil
	}
	data, err := io.ReadAll(&contextReader{ctx: ctx, r: io.LimitReader(reader, s.limits.MaxIndexBytes+1)})
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > s.limits.MaxIndexBytes {
		return nil, errors.New("decompressed index exceeds limit")
	}
	return data, nil
}

func (s *Store) packageObjects(ctx context.Context, indexes []Object) ([]Object, error) {
	// Every advertised supported variant is parsed. Divergent variants cannot
	// cause APT's compression choice to reveal an unprefetched dependency.
	var result []Object
	for _, o := range indexes {
		data, err := s.decodedIndex(ctx, o)
		if err != nil {
			return nil, err
		}
		stanzas, err := controlStanzas(string(data))
		if err != nil {
			return nil, err
		}
		for _, fields := range stanzas {
			if path.Base(strings.TrimSuffix(strings.TrimSuffix(o.Path, ".gz"), ".xz")) == "Sources" {
				directory := fields["directory"]
				if !safePath(directory) {
					return nil, errors.New("unsafe source directory")
				}
				refs, err := checksumLines(fields["checksums-sha256"], directory+"/")
				if err != nil {
					return nil, err
				}
				result = append(result, refs...)
			} else {
				p, digest := fields["filename"], fields["sha256"]
				size, err := strconv.ParseInt(fields["size"], 10, 64)
				if err != nil || size < 0 || !safePath(p) || !validDigest(digest) {
					return nil, errors.New("invalid Packages object reference")
				}
				result = append(result, Object{Path: p, SHA256: digest, Size: size})
			}
			if len(result) > s.limits.MaxObjects {
				return nil, errors.New("index exceeds object limit")
			}
		}
	}
	return uniqueObjects(result)
}

func objectMap(m *Manifest) map[string]Object {
	result := make(map[string]Object, len(m.Objects))
	for _, o := range m.Objects {
		result[o.Path] = o
	}
	return result
}

// Verify checks both content integrity and closure against Release and indexes.
// Damaged contains repository paths, never untrusted local filesystem paths.
func (s *Store) Verify(ctx context.Context) (damaged []string, err error) {
	m, err := s.Current()
	if err != nil {
		return nil, err
	}
	return s.verify(ctx, m)
}

func (s *Store) verify(ctx context.Context, m *Manifest) ([]string, error) {
	if err := s.validateManifest(m); err != nil {
		return nil, err
	}
	var damaged []string
	seen := make(map[string]bool)
	for _, o := range m.Objects {
		if err := ctx.Err(); err != nil {
			return damaged, err
		}
		if seen[o.SHA256] {
			continue
		}
		seen[o.SHA256] = true
		if !s.hasObject(o) {
			damaged = append(damaged, o.Path)
		}
	}
	if len(damaged) > 0 {
		return damaged, errors.New("snapshot contains missing or damaged objects")
	}
	indexes, err := s.selectedIndexes(ctx, m)
	if err != nil {
		return nil, err
	}
	byPath := objectMap(m)
	expected := append([]Object(nil), indexes...)
	for _, index := range indexes {
		alias := index
		alias.Path = path.Dir(index.Path) + "/by-hash/SHA256/" + index.SHA256
		expected = append(expected, alias)
	}
	objects, err := s.packageObjects(ctx, indexes)
	if err != nil {
		return nil, err
	}
	expected = append(expected, objects...)
	base := "dists/" + m.Selection.Suite + "/"
	if release, ok := byPath[base+"InRelease"]; ok {
		expected = append(expected, release)
	} else {
		expected = append(expected, byPath[base+"Release"])
		if signature, ok := byPath[base+"Release.gpg"]; ok {
			expected = append(expected, signature)
		}
	}
	expected, err = uniqueObjects(expected)
	if err != nil {
		return nil, err
	}
	if len(expected) != len(m.Objects) {
		return nil, errors.New("manifest includes paths outside the selected repository set")
	}
	for _, want := range expected {
		got, ok := byPath[want.Path]
		if !ok || got.SHA256 != want.SHA256 || got.Size != want.Size {
			return []string{want.Path}, errors.New("snapshot is not a complete selected repository set")
		}
	}
	return nil, nil
}

// Repair refetches only damaged objects in the published set. Paths optionally
// restrict repair to manifest paths; the entire set must pass before success.
func (s *Store) Repair(ctx context.Context, paths []string) error {
	m, err := s.Current()
	if err != nil {
		return err
	}
	byPath := objectMap(m)
	selected := make(map[string]bool)
	for _, p := range paths {
		o, ok := byPath[p]
		if !ok {
			return fmt.Errorf("repair path is outside the snapshot: %s", p)
		}
		selected[o.SHA256] = true
	}
	seen := make(map[string]bool)
	preferred := make(map[string]Object)
	for _, o := range m.Objects {
		if _, ok := preferred[o.SHA256]; !ok || !strings.Contains(o.Path, "/by-hash/") {
			preferred[o.SHA256] = o
		}
	}
	for _, o := range m.Objects {
		if seen[o.SHA256] || (len(paths) > 0 && !selected[o.SHA256]) {
			continue
		}
		seen[o.SHA256] = true
		if !s.hasObject(o) {
			// Prefer the conventional path over its by-hash alias.
			o = preferred[o.SHA256]
			if _, err := s.download(ctx, m.Selection, o.Path, &o); err != nil {
				return err
			}
		}
	}
	_, err = s.verify(ctx, m)
	return err
}
