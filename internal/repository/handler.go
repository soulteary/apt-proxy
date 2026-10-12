package repository

import (
	"context"
	"net/http"
	"path"
	"strings"
)

// Handler pins one verified complete snapshot. It has no upstream/network path.
// Each object is checked again before serving, including HEAD and range requests.
func (s *Store) Handler(ctx context.Context) (http.Handler, error) {
	m, err := s.Current()
	if err != nil {
		return nil, err
	}
	if _, err := s.verify(ctx, m); err != nil {
		return nil, err
	}
	objects := objectMap(m)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		p := strings.TrimPrefix(r.URL.Path, "/")
		if r.URL.RawPath != "" || r.URL.RawQuery != "" || !safePath(p) {
			http.Error(w, "invalid repository path", http.StatusBadRequest)
			return
		}
		o, ok := objects[p]
		if !ok {
			http.NotFound(w, r)
			return
		}
		f, err := s.openObject(o)
		if err != nil {
			http.Error(w, "snapshot object failed verification", http.StatusServiceUnavailable)
			return
		}
		defer func() { _ = f.Close() }()
		w.Header().Set("ETag", `"sha256-`+o.SHA256+`"`)
		w.Header().Set("Cache-Control", "public, max-age=0, must-revalidate")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeContent(w, r, path.Base(p), m.Created, f)
	}), nil
}
