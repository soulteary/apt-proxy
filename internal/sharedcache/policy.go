// Package sharedcache provides explicit content identity and shared-disk coordination.
package sharedcache

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"

	httpcache "github.com/soulteary/httpcache-kit/v4"
)

// Config is YAML-only; changes require a restart on every sharing instance.
type Config struct {
	Repositories    []Repository `yaml:"repositories"`
	CoordinationDir string       `yaml:"coordination_dir"`
	MaxIndexMB      int64        `yaml:"max_index_mb"`
}
type Repository struct {
	ID    string   `yaml:"id"`
	Bases []string `yaml:"bases"`
}
type mapping struct {
	base       *url.URL
	id, policy string
}
type Policy struct {
	mappings    []mapping
	process     string
	maxBytes    int64
	coordinated bool
}

func New(cfg *Config) (*Policy, error) {
	if cfg == nil {
		return nil, nil
	}
	if len(cfg.Repositories) == 0 {
		return nil, fmt.Errorf("shared_cache requires explicit repositories")
	}
	if cfg.MaxIndexMB < 0 || cfg.MaxIndexMB > 1024 {
		return nil, fmt.Errorf("shared_cache max_index_mb is invalid")
	}
	limit := cfg.MaxIndexMB
	if limit == 0 {
		limit = 32
	}
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return nil, err
	}
	p := &Policy{process: hex.EncodeToString(token[:]), maxBytes: limit * (1 << 20), coordinated: cfg.CoordinationDir != ""}
	ids := map[string]bool{}
	bases := map[string]bool{}
	for _, repo := range cfg.Repositories {
		if repo.ID == "" || strings.ContainsRune(repo.ID, '\x00') || len(repo.ID) > 128 || len(repo.Bases) == 0 || ids[repo.ID] {
			return nil, fmt.Errorf("shared_cache repository IDs must be unique and nonempty, with bases")
		}
		ids[repo.ID] = true
		var normalized []*url.URL
		var names []string
		for _, raw := range repo.Bases {
			u, err := url.Parse(raw)
			if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
				return nil, fmt.Errorf("shared_cache bases must be absolute public HTTP(S) URLs without credentials or query")
			}
			if !strings.HasSuffix(u.Path, "/") {
				u.Path += "/"
				if u.RawPath != "" {
					u.RawPath += "/"
				}
			}
			u.Host = strings.ToLower(u.Host)
			if bases[u.String()] {
				return nil, fmt.Errorf("shared_cache bases cannot overlap repositories")
			}
			bases[u.String()] = true
			normalized = append(normalized, u)
			names = append(names, u.String())
		}
		sort.Strings(names)
		fingerprint := sha256.Sum256([]byte("apt-proxy-shared-v1\x00" + repo.ID + "\x00" + strings.Join(names, "\x00")))
		for _, u := range normalized {
			p.mappings = append(p.mappings, mapping{base: u, id: repo.ID, policy: hex.EncodeToString(fingerprint[:])})
		}
	}
	// Longest path wins; nested mappings belonging to different IDs are invalid.
	for i, a := range p.mappings {
		for j, b := range p.mappings {
			if i != j && a.id != b.id && a.base.Scheme == b.base.Scheme && a.base.Host == b.base.Host && strings.HasPrefix(a.base.EscapedPath(), b.base.EscapedPath()) {
				return nil, fmt.Errorf("shared_cache bases cannot overlap repositories")
			}
		}
	}
	sort.SliceStable(p.mappings, func(i, j int) bool {
		return len(p.mappings[i].base.EscapedPath()) > len(p.mappings[j].base.EscapedPath())
	})
	return p, nil
}

func publicRequest(req *http.Request) bool {
	if req.URL.User != nil {
		return false
	}
	for name, values := range req.Header {
		switch strings.ToLower(name) {
		case "accept", "accept-encoding", "accept-language", "user-agent", "cache-control", "pragma", "range", "if-range", "if-match", "if-none-match", "if-modified-since", "if-unmodified-since", "host", "x-request-id":
		case "content-length":
			for _, value := range values {
				if value != "0" {
					return false
				}
			}
		case "connection", "proxy-connection", "keep-alive":
			for _, value := range values {
				if !strings.EqualFold(value, "close") && !strings.EqualFold(value, "keep-alive") {
					return false
				}
			}
		default:
			return false
		}
	}
	return req.ContentLength == 0 && len(req.TransferEncoding) == 0 && len(req.Trailer) == 0 && (req.Method == http.MethodGet || req.Method == http.MethodHead)
}

// WrapHandler bypasses cache reads and writes for any unclassified header.
// Request IDs and adapter framing are local operational metadata and are not
// forwarded into a shared representation's upstream response.
func (p *Policy) WrapHandler(cached, uncached http.Handler) http.Handler {
	if p == nil {
		return cached
	}
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if !publicRequest(req) {
			uncached.ServeHTTP(&privateWriter{ResponseWriter: w}, req)
			return
		}
		clean := req.Clone(req.Context())
		for name := range clean.Header {
			switch strings.ToLower(name) {
			case "x-request-id", "content-length", "connection", "proxy-connection", "keep-alive", "host":
				delete(clean.Header, name)
			}
		}
		cached.ServeHTTP(w, clean)
	})
}

type privateWriter struct{ http.ResponseWriter }

func (w *privateWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *privateWriter) WriteHeader(status int) {
	w.Header().Set("Cache-Control", "no-store")
	w.ResponseWriter.WriteHeader(status)
}
func (w *privateWriter) Write(b []byte) (int, error) {
	w.Header().Set("Cache-Control", "no-store")
	return w.ResponseWriter.Write(b)
}
func (w *privateWriter) Flush() {
	w.Header().Set("Cache-Control", "no-store")
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}

func (p *Policy) content(req *http.Request) (*mapping, string, string, bool) {
	if p == nil || !publicRequest(req) {
		return nil, "", "", false
	}
	for i := range p.mappings {
		m := &p.mappings[i]
		if req.URL.Scheme != m.base.Scheme || !strings.EqualFold(req.URL.Host, m.base.Host) || !strings.HasPrefix(req.URL.EscapedPath(), m.base.EscapedPath()) {
			continue
		}
		rel := strings.TrimPrefix(req.URL.EscapedPath(), m.base.EscapedPath())
		parts := strings.Split(rel, "/")
		if len(parts) < 6 || parts[0] != "dists" || parts[1] == "" || parts[len(parts)-3] != "by-hash" || parts[len(parts)-2] != "SHA256" {
			return nil, "", "", false
		}
		for _, part := range parts {
			if part == "" || part == "." || part == ".." || strings.Contains(part, "%") {
				return nil, "", "", false
			}
		}
		digest := parts[len(parts)-1]
		decoded, err := hex.DecodeString(digest)
		if err != nil || len(decoded) != sha256.Size {
			return nil, "", "", false
		}
		return m, rel, strings.ToLower(digest), true
	}
	return nil, "", "", false
}

// RequestKey retains method, relative path, query, and normal Vary handling.
// Only explicitly mapped SHA256 by-hash indexes can cross origins.
func (p *Policy) RequestKey(req *http.Request) (httpcache.Key, error) {
	if m, rel, _, ok := p.content(req); ok {
		u := &url.URL{Scheme: "apt-content", Host: m.policy, Path: "/" + rel, RawQuery: req.URL.RawQuery, ForceQuery: req.URL.ForceQuery}
		return httpcache.NewKey(req.Method, u, req.Header), nil
	}
	if p != nil && p.coordinated {
		// Mutable and private traffic never share an entry across processes.
		u := *req.URL
		u.Scheme = "apt-instance-" + p.process + "-" + u.Scheme
		return httpcache.NewKey(req.Method, &u, req.Header), nil
	}
	return httpcache.NewRequestKey(req), nil
}
