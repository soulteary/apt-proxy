// Copyright 2026 Su Yang
// SPDX-License-Identifier: Apache-2.0

package metalink

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/soulteary/apt-proxy/internal/access"
	"github.com/soulteary/apt-proxy/internal/resource"
	httpcache "github.com/soulteary/httpcache-kit/v4"
)

var ErrCapacity = errors.New("metalink repository capacity reached")
var queryPart = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

type repository struct {
	done     chan struct{}
	snapshot *snapshot
	expires  time.Time
	err      error
	epoch    uint64
}
type snapshot struct {
	token           string
	repositoryToken string
	metadata        *metadata
	expires         time.Time
	freshness       time.Duration
}
type route struct {
	snapshot *snapshot
	relative string
}
type routeKey struct{}

// Controller owns bounded current repository lists and issued snapshot routes.
// Lists have a short refresh TTL; immutable issued routes expire after 24 hours.
// At most MaxRepositories lists and 4*MaxRepositories snapshot generations live
// at once. Capacity pressure fails closed instead of evicting a running client.
type Controller struct {
	cfg            Config
	public, source *url.URL
	allowed        map[string]bool
	outer          *access.Upstream
	transport      http.RoundTripper
	resources      *resource.Manager
	policy         string
	now            func() time.Time
	mu             sync.Mutex
	epoch          uint64
	repos          map[string]*repository
	snapshots      map[string]*snapshot
}

func parseQuery(raw string) (url.Values, error) {
	if len(raw) > 512 {
		return nil, fmt.Errorf("query is too long")
	}
	q, err := url.ParseQuery(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid Metalink query")
	}
	for k, values := range q {
		if k != "repo" && k != "arch" && k != "protocol" && k != "countme" {
			return nil, fmt.Errorf("unsupported Metalink query parameter")
		}
		if len(values) != 1 || values[0] == "" {
			return nil, fmt.Errorf("duplicate or empty Metalink query parameter")
		}
	}
	if len(q.Get("repo")) > 128 || len(q.Get("arch")) > 32 || !queryPart.MatchString(q.Get("repo")) || !queryPart.MatchString(q.Get("arch")) {
		return nil, fmt.Errorf("repo and arch are required")
	}
	if value := q.Get("protocol"); value != "" {
		seen := map[string]bool{}
		for _, protocol := range strings.Split(value, ",") {
			if (protocol != "http" && protocol != "https") || seen[protocol] {
				return nil, fmt.Errorf("protocol must contain unique http/https values")
			}
			seen[protocol] = true
		}
	}
	if value := q.Get("countme"); value != "" && value != "1" && value != "2" && value != "3" && value != "4" {
		return nil, fmt.Errorf("countme must be 1 to 4")
	}
	return q, nil
}

func publicRequest(r *http.Request) bool {
	if r.URL.User != nil || r.ContentLength != 0 || len(r.TransferEncoding) != 0 || len(r.Trailer) != 0 {
		return false
	}
	for name, values := range r.Header {
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
	// The Fiber adapter supplies an empty reader for bodyless GET requests.
	// Verify EOF before cache lookup instead of trusting Content-Length alone.
	if r.Body != nil && r.Body != http.NoBody {
		var one [1]byte
		n, err := io.ReadFull(r.Body, one[:])
		if n != 0 || err != io.EOF {
			return false
		}
		_ = r.Body.Close()
		r.Body = http.NoBody
	}
	return true
}

func fail(w http.ResponseWriter, err error) {
	w.Header().Set("Cache-Control", "no-store")
	status := http.StatusBadGateway
	message := "no verified Metalink snapshot is available"
	if errors.Is(err, ErrCapacity) || errors.Is(err, resource.ErrAdmission) {
		status = http.StatusServiceUnavailable
		message = "Metalink capacity is temporarily unavailable"
		w.Header().Set("Retry-After", "60")
	}
	http.Error(w, message, status)
}

// Handle handles the explicit Metalink routes before ordinary distro routing.
// The supplied next is the cache-wrapped raw reverse proxy, without another
// distro URL rewrite. Requests are canonicalized before the cache lookup.
func (c *Controller) Handle(w http.ResponseWriter, r *http.Request, next http.Handler) bool {
	if c == nil || (r.URL.Path != "/metalink" && !strings.HasPrefix(r.URL.Path, "/metalink/")) {
		return false
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return true
	}
	if !publicRequest(r) || r.URL.Opaque != "" || r.URL.Fragment != "" || r.URL.RawPath != "" || !safePath(r.URL.Path) {
		http.Error(w, "invalid public Metalink request", http.StatusBadRequest)
		return true
	}
	if r.URL.Path == "/metalink" {
		q, err := parseQuery(r.URL.RawQuery)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return true
		}
		s, err := c.get(r.Context(), q)
		if err != nil {
			fail(w, err)
			return true
		}
		body := c.render(s)
		w.Header().Set("Content-Type", "application/metalink+xml; charset=utf-8")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		if r.Method != http.MethodHead {
			_, _ = w.Write(body)
		}
		return true
	}
	if r.URL.RawQuery != "" || r.URL.ForceQuery {
		http.Error(w, "repository query parameters are not supported", http.StatusBadRequest)
		return true
	}
	parts := strings.SplitN(strings.TrimPrefix(r.URL.Path, "/metalink/repos/"), "/", 2)
	if !strings.HasPrefix(r.URL.Path, "/metalink/repos/") || len(parts) != 2 || len(parts[0]) != 64 || parts[1] == "" || strings.HasSuffix(parts[1], "/") {
		http.Error(w, "unknown Metalink route", http.StatusNotFound)
		return true
	}
	c.mu.Lock()
	s := c.snapshots[parts[0]]
	if s != nil && !s.expires.After(c.now()) {
		delete(c.snapshots, parts[0])
		s = nil
	}
	c.mu.Unlock()
	if s == nil {
		http.Error(w, "Metalink snapshot is unknown or expired; refresh metadata", http.StatusGone)
		return true
	}
	ctx := context.WithValue(r.Context(), routeKey{}, &route{s, parts[1]})
	forward := r.Clone(ctx)
	for name := range forward.Header {
		switch strings.ToLower(name) {
		case "x-request-id", "content-length", "connection", "proxy-connection", "keep-alive", "host":
			delete(forward.Header, name)
		}
	}
	forward.URL = &url.URL{Scheme: "http", Host: canonicalHost, Path: "/" + c.policy + "/" + s.repositoryToken + "/" + s.metadata.digest + "/" + parts[1]}
	forward.Host = canonicalHost
	forward.RequestURI = ""
	w.Header().Del("Cache-Control")
	next.ServeHTTP(w, forward)
	return true
}

// Refresh invalidates current lists, including in-progress fetch publication,
// without changing the meaning of an already-issued immutable snapshot token.
func (c *Controller) Refresh() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.epoch++
	for _, entry := range c.repos {
		entry.expires = time.Time{}
	}
	c.mu.Unlock()
}

func (c *Controller) cleanLocked(now time.Time) {
	for token, s := range c.snapshots {
		if !s.expires.After(now) {
			delete(c.snapshots, token)
		}
	}
	for key, r := range c.repos {
		if r.done == nil && !r.expires.After(now) {
			delete(c.repos, key)
		}
	}
}

func (c *Controller) get(ctx context.Context, q url.Values) (*snapshot, error) {
	identityQuery := q.Clone()
	identityQuery.Del("countme")
	key := identityQuery.Encode()
	c.mu.Lock()
	now := c.now()
	c.cleanLocked(now)
	if old := c.repos[key]; old != nil {
		if old.done != nil {
			done := old.done
			c.mu.Unlock()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-done:
				return old.snapshot, old.err
			}
		}
		c.mu.Unlock()
		return old.snapshot, old.err
	}
	if len(c.repos) >= c.cfg.MaxRepositories {
		c.mu.Unlock()
		return nil, ErrCapacity
	}
	entry := &repository{done: make(chan struct{}), epoch: c.epoch}
	c.repos[key] = entry
	c.mu.Unlock()

	s, err := c.fetch(ctx, q)
	c.mu.Lock()
	defer c.mu.Unlock()
	if err == nil && entry.epoch != c.epoch {
		err = fmt.Errorf("metalink refresh interrupted snapshot fetch")
	}
	if err == nil {
		freshness := s.freshness
		if old := c.snapshots[s.token]; old != nil {
			s = old
		} else if len(c.snapshots) >= 4*c.cfg.MaxRepositories {
			err = ErrCapacity
		} else {
			c.snapshots[s.token] = s
		}
		entry.expires = c.now().Add(freshness)
		if entry.expires.After(s.expires) {
			entry.expires = s.expires
		}
	}
	entry.snapshot, entry.err = s, err
	if err != nil && c.repos[key] == entry {
		delete(c.repos, key)
	}
	close(entry.done)
	entry.done = nil
	return s, err
}

func (c *Controller) fetch(ctx context.Context, q url.Values) (*snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Duration(c.cfg.TimeoutSeconds)*time.Second)
	defer cancel()
	if c.resources != nil {
		admission, err := c.resources.AcquireDownload(ctx)
		if err != nil {
			return nil, err
		}
		defer admission.Close()
	}
	u := *c.source
	u.RawQuery = q.Encode()
	freshness := time.Duration(c.cfg.RefreshSeconds) * time.Second
	data, release, err := c.read(ctx, &u, c.cfg.MaxXMLBytes, readOptions{sourceTTL: &freshness})
	if err != nil {
		return nil, err
	}
	defer release()
	m, err := c.parse(data)
	release()
	if err != nil {
		return nil, err
	}
	if protocol := q.Get("protocol"); protocol != "" {
		allowedProtocols := "," + protocol + ","
		filtered := m.mirrors[:0]
		for _, candidate := range m.mirrors {
			if strings.Contains(allowedProtocols, ","+candidate.base.Scheme+",") {
				filtered = append(filtered, candidate)
			}
		}
		m.mirrors = filtered
	}
	selected := -1
	for i, candidate := range m.mirrors {
		if err := c.verifyMirror(ctx, candidate, m); err == nil {
			selected = i
			break
		} else if errors.Is(err, resource.ErrAdmission) {
			return nil, err
		}
	}
	if selected < 0 {
		return nil, fmt.Errorf("no mirror matches Metalink repomd.xml")
	}
	// Keep the selected verified mirror first; every fallback is reverified at
	// use time, and only the same declared snapshot can be served.
	if selected != 0 {
		m.mirrors[0], m.mirrors[selected] = m.mirrors[selected], m.mirrors[0]
	}
	identityQuery := q.Clone()
	identityQuery.Del("countme")
	repoSum := sha256.Sum256([]byte(c.policy + "\x00" + identityQuery.Encode()))
	repoToken := hex.EncodeToString(repoSum[:])
	var identity strings.Builder
	identity.WriteString(repoToken)
	for _, h := range m.hashes {
		identity.WriteString("\x00" + h.Type + ":" + h.Value)
	}
	// Route generation reflects its verified candidate list; equivalent
	// snapshots still use the same canonical artifact cache namespace.
	for _, candidate := range m.mirrors {
		identity.WriteString("\x00" + candidate.base.String() + ":" + strconv.Itoa(candidate.preference))
	}
	tokenSum := sha256.Sum256([]byte(identity.String()))
	return &snapshot{token: hex.EncodeToString(tokenSum[:]), repositoryToken: repoToken, metadata: m, expires: c.now().Add(snapshotLifetime), freshness: freshness}, nil
}

type readOptions struct {
	sourceTTL *time.Duration
	headers   *http.Header
}

// read charges bounded buffering until its caller releases the resulting data.
func (c *Controller) read(ctx context.Context, u *url.URL, limit int64, options ...readOptions) ([]byte, func(), error) {
	if c.resources != nil && limit > c.resources.MaxObjectBytes() {
		limit = c.resources.MaxObjectBytes()
	}
	var lease httpcache.StagingLease
	if c.resources != nil {
		var err error
		lease, err = c.resources.NewLease(ctx, httpcache.StageKind("metalink-metadata"))
		if err != nil {
			return nil, nil, err
		}
		if err := lease.Reserve(limit + 1); err != nil {
			lease.Close()
			return nil, nil, err
		}
	}
	release := func() {
		if lease != nil {
			lease.Close()
		}
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	req.Header.Set("Accept-Encoding", "identity")
	if c.outer != nil {
		if err := c.outer.CheckURL(u); err != nil {
			release()
			return nil, nil, err
		}
	}
	res, err := c.transport.RoundTrip(req)
	if err != nil {
		release()
		return nil, nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK || res.Header.Get("Content-Encoding") != "" && res.Header.Get("Content-Encoding") != "identity" || res.Header.Get("Content-Range") != "" || res.ContentLength > limit {
		release()
		return nil, nil, fmt.Errorf("metadata must be a bounded full HTTP 200 response")
	}
	if !publicMetadata(res.Header) {
		release()
		return nil, nil, fmt.Errorf("metadata response is not public")
	}
	if len(options) > 0 {
		if options[0].headers != nil {
			*options[0].headers = res.Header.Clone()
		}
		if options[0].sourceTTL != nil {
			capSourceFreshness(res.Header, options[0].sourceTTL, c.now())
		}
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil || int64(len(data)) > limit {
		release()
		return nil, nil, fmt.Errorf("metadata exceeds size limit or is incomplete")
	}
	return data, release, nil
}

func artifactURL(base *url.URL, relative string) *url.URL { u := *base; u.Path += relative; return &u }
func (c *Controller) verifyMirror(ctx context.Context, candidate mirror, m *metadata) error {
	data, release, err := c.read(ctx, artifactURL(candidate.base, "repodata/repomd.xml"), c.cfg.MaxRepomdBytes)
	if err != nil {
		return err
	}
	defer release()
	if !m.verify(data) {
		return fmt.Errorf("mirror repomd.xml does not match Metalink")
	}
	return nil
}

type routingTransport struct {
	controller *Controller
	next       http.RoundTripper
}

// WrapTransport intercepts only authenticated internal snapshot contexts. Other
// requests use next unchanged; naked synthetic-host requests are denied.
func (c *Controller) WrapTransport(next http.RoundTripper) http.RoundTripper {
	if c == nil {
		return next
	}
	return &routingTransport{c, next}
}

type leasedBody struct {
	io.ReadCloser
	release func()
	once    sync.Once
}

func (b *leasedBody) Close() error { err := b.ReadCloser.Close(); b.once.Do(b.release); return err }

func publicMetadata(h http.Header) bool {
	if len(h.Values("Set-Cookie")) > 0 || len(h.Values("Vary")) > 0 {
		return false
	}
	cc, err := httpcache.ParseCacheControlHeaders(h)
	if err != nil {
		return false
	}
	for directive := range cc {
		if strings.EqualFold(directive, "private") {
			return false
		}
	}
	return true
}

func publicResponse(h http.Header) bool {
	if !publicMetadata(h) {
		return false
	}
	cc, _ := httpcache.ParseCacheControlHeaders(h)
	for directive := range cc {
		if strings.EqualFold(directive, "no-store") {
			return false
		}
	}
	return true
}

func capSourceFreshness(h http.Header, ttl *time.Duration, now time.Time) {
	cc, _ := httpcache.ParseCacheControlHeaders(h)
	for directive, values := range cc {
		if strings.EqualFold(directive, "no-store") || strings.EqualFold(directive, "no-cache") {
			*ttl = 0
			return
		}
		if strings.EqualFold(directive, "max-age") || strings.EqualFold(directive, "s-maxage") {
			for _, value := range values {
				seconds, err := strconv.ParseInt(value, 10, 32)
				if err != nil || seconds < 0 {
					*ttl = 0
					return
				}
				age, _ := strconv.ParseInt(h.Get("Age"), 10, 32)
				if age > 0 {
					seconds -= age
				}
				if seconds < 0 {
					seconds = 0
				}
				if freshness := time.Duration(seconds) * time.Second; freshness < *ttl {
					*ttl = freshness
				}
			}
		}
	}
	if expires, err := http.ParseTime(h.Get("Expires")); err == nil {
		freshness := expires.Sub(now)
		if freshness < 0 {
			freshness = 0
		}
		if freshness < *ttl {
			*ttl = freshness
		}
	}
}

func cacheHeaders(h http.Header) {
	// Public mirror routing cannot turn an explicitly private response public.
	if !publicResponse(h) {
		h.Set("Cache-Control", "no-store")
		return
	}
	if len(h.Values("Cache-Control")) == 0 && len(h.Values("Expires")) == 0 {
		h.Set("Cache-Control", "public, max-age=3600")
	}
}

func (t *routingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	c := t.controller
	route, ok := r.Context().Value(routeKey{}).(*route)
	if r.URL.Hostname() != canonicalHost {
		if ok {
			return nil, fmt.Errorf("metalink canonical request was unexpectedly rewritten")
		}
		return t.next.RoundTrip(r)
	}
	if !ok || route == nil || r.URL.Path != "/"+c.policy+"/"+route.snapshot.repositoryToken+"/"+route.snapshot.metadata.digest+"/"+route.relative || r.URL.RawQuery != "" || r.URL.User != nil {
		return nil, fmt.Errorf("%w: invalid internal Metalink route", access.ErrUpstreamDenied)
	}
	ctx, cancel := context.WithCancel(r.Context())
	timer := time.AfterFunc(time.Duration(c.cfg.TimeoutSeconds)*time.Second, cancel)
	// Package response bodies remain governed by the outer download wrapper;
	// this deadline applies to validation and response headers, not RPM streaming.
	streaming := false
	defer func() {
		timer.Stop()
		if !streaming {
			cancel()
		}
	}()
	for _, candidate := range route.snapshot.metadata.mirrors {
		if route.relative == "repodata/repomd.xml" {
			var h http.Header
			data, release, err := c.read(ctx, artifactURL(candidate.base, route.relative), c.cfg.MaxRepomdBytes, readOptions{headers: &h})
			if err != nil {
				if errors.Is(err, resource.ErrAdmission) {
					return nil, err
				}
				continue
			}
			if !route.snapshot.metadata.verify(data) {
				release()
				continue
			}
			if h.Get("Content-Type") == "" {
				h.Set("Content-Type", "application/xml")
			}
			cacheHeaders(h)
			body := io.NopCloser(bytes.NewReader(data))
			if r.Method == http.MethodHead {
				release()
				body = http.NoBody
			} else {
				body = &leasedBody{ReadCloser: body, release: release}
			}
			return &http.Response{StatusCode: http.StatusOK, Status: "200 OK", Header: h, Body: body, ContentLength: int64(len(data)), Request: r}, nil
		}
		if err := c.verifyMirror(ctx, candidate, route.snapshot.metadata); err != nil {
			if errors.Is(err, resource.ErrAdmission) {
				return nil, err
			}
			continue
		}
		forward := r.Clone(ctx)
		forward.URL = artifactURL(candidate.base, route.relative)
		forward.Host = forward.URL.Host
		forward.RequestURI = ""
		if c.outer != nil && c.outer.CheckURL(forward.URL) != nil {
			continue
		}
		res, err := c.transport.RoundTrip(forward)
		if err != nil {
			continue
		}
		if res.StatusCode >= 300 && res.StatusCode < 400 && res.StatusCode != http.StatusNotModified || res.StatusCode == http.StatusNotFound || res.StatusCode >= 500 {
			_ = res.Body.Close()
			continue
		}
		cacheHeaders(res.Header)
		if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusPartialContent && res.StatusCode != http.StatusNotModified {
			res.Header.Set("Cache-Control", "no-store")
		}
		res.Request = r
		timer.Stop()
		res.Body = &leasedBody{ReadCloser: res.Body, release: cancel}
		streaming = true
		return res, nil
	}
	return nil, fmt.Errorf("no mirror can serve the verified Metalink snapshot")
}
