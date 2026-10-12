package repository

import (
	"archive/tar"
	"context"
	"encoding/json"
	"errors"
	"io"
	"sort"
)

// Export writes a portable archive after checking the entire selected set.
func (s *Store) Export(ctx context.Context, writer io.Writer) error {
	m, err := s.Current()
	if err != nil {
		return err
	}
	if _, err := s.verify(ctx, m); err != nil {
		return err
	}
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	tw := tar.NewWriter(writer)
	defer func() { _ = tw.Close() }()
	if err := tw.WriteHeader(&tar.Header{Name: "manifest.json", Mode: 0o600, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
		return err
	}
	if _, err := tw.Write(data); err != nil {
		return err
	}
	unique := make(map[string]Object)
	for _, o := range m.Objects {
		unique[o.SHA256] = o
	}
	digests := make([]string, 0, len(unique))
	for digest := range unique {
		digests = append(digests, digest)
	}
	sort.Strings(digests)
	for _, digest := range digests {
		o := unique[digest]
		if err := tw.WriteHeader(&tar.Header{Name: "objects/" + digest, Mode: 0o600, Size: o.Size, Typeflag: tar.TypeReg}); err != nil {
			return err
		}
		f, err := s.openObject(o)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(tw, &contextReader{ctx: ctx, r: f})
		closeErr := f.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return tw.Close()
}

// Import accepts only a manifest-first archive containing exactly its immutable
// objects. Every digest and the complete metadata closure is checked before publish.
func (s *Store) Import(ctx context.Context, reader io.Reader) (*Manifest, error) {
	tr := tar.NewReader(&contextReader{ctx: ctx, r: reader})
	header, err := tr.Next()
	if err != nil {
		return nil, err
	}
	if header.Name != "manifest.json" || header.Typeflag != tar.TypeReg || header.Size < 0 || header.Size > s.limits.MaxManifestBytes {
		return nil, errors.New("archive must begin with a bounded regular manifest.json")
	}
	data, err := io.ReadAll(io.LimitReader(tr, s.limits.MaxManifestBytes+1))
	if err != nil {
		return nil, err
	}
	m, err := s.decodeManifest(data)
	if err != nil {
		return nil, err
	}
	expected := make(map[string]Object)
	for _, o := range m.Objects {
		expected["objects/"+o.SHA256] = o
	}
	seen := make(map[string]bool)
	for {
		header, err = tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		o, ok := expected[header.Name]
		if !ok || seen[header.Name] || header.Typeflag != tar.TypeReg || header.Size != o.Size || len(header.PAXRecords) != 0 {
			return nil, errors.New("archive contains an unexpected, duplicate or non-regular object")
		}
		seen[header.Name] = true
		if _, err := s.saveObject(ctx, tr, &o); err != nil {
			return nil, err
		}
	}
	if len(seen) != len(expected) {
		return nil, errors.New("archive is missing required objects")
	}
	if _, err := s.verify(ctx, m); err != nil {
		return nil, err
	}
	if err := s.publish(m); err != nil {
		return nil, err
	}
	return m, nil
}
