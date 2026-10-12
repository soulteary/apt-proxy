package mirrorhealth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/soulteary/apt-proxy/internal/access"
	logger "github.com/soulteary/logger-kit/v3"
)

type snapshot struct {
	hash     string
	observed time.Time
}
type repository struct {
	id             string
	bases          []*url.URL
	queries        map[string]bool
	mu             sync.Mutex
	selecting      chan struct{}
	source, active int
	failures       []int
	bodyFailures   []bool
	nextProbe      time.Time
	reason         string
	snapshots      map[string]snapshot
	verified       map[string]string
	observations   map[string]uint64
	sequence       uint64
	limited        bool
}

// Controller owns immutable mappings and synchronized, process-local health.
type Controller struct {
	base, probe http.RoundTripper
	log         *logger.Logger
	cfg         Config
	repos       map[string]*repository
	epoch       atomic.Uint64
}

// New returns nil when disabled. Both transports must enforce upstream policy;
// probe must not follow redirects or recursively enter this controller.
func New(cfg *Config, base, probe http.RoundTripper, log *logger.Logger) (*Controller, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if cfg == nil || !cfg.Enabled {
		return nil, nil
	}
	if base == nil || probe == nil {
		return nil, fmt.Errorf("mirror_health: transports are required")
	}
	if log == nil {
		log = logger.Default()
	}
	c := &Controller{base: base, probe: probe, log: log, cfg: cfg.defaults(), repos: map[string]*repository{}}
	c.epoch.Store(1)
	for _, entry := range cfg.Repositories {
		repo := &repository{id: entry.ID, source: -1, active: -1, selecting: make(chan struct{}, 1), queries: map[string]bool{}, snapshots: map[string]snapshot{}, verified: map[string]string{}, observations: map[string]uint64{}}
		for _, raw := range entry.Mirrors {
			u, _ := parseBase(raw)
			repo.bases = append(repo.bases, u)
		}
		repo.failures = make([]int, len(repo.bases))
		repo.bodyFailures = make([]bool, len(repo.bases))
		for _, key := range entry.PublicQueryKeys {
			repo.queries[key] = true
		}
		c.repos[repo.id] = repo
	}
	return c, nil
}

// Epoch identifies the routing generation permitted to learn or switch.
func (c *Controller) Epoch() uint64 {
	if c == nil {
		return 0
	}
	return c.epoch.Load()
}

// Reset forgets health and fingerprints after a successful explicit reload.
// Requests carrying an old routing epoch can finish but cannot change health.
func (c *Controller) Reset() uint64 {
	if c == nil {
		return 0
	}
	epoch := c.epoch.Add(1)
	for _, repo := range c.repos {
		repo.mu.Lock()
		repo.source, repo.active = -1, -1
		repo.snapshots, repo.verified = map[string]snapshot{}, map[string]string{}
		repo.observations, repo.limited = map[string]uint64{}, false
		clear(repo.failures)
		clear(repo.bodyFailures)
		repo.reason, repo.nextProbe = "", time.Time{}
		repo.mu.Unlock()
	}
	return epoch
}

func (repo *repository) match(u *url.URL) (int, string) {
	for i, base := range repo.bases {
		if sameOrigin(u, base) && strings.HasPrefix(u.EscapedPath(), base.EscapedPath()) {
			return i, strings.TrimPrefix(u.EscapedPath(), base.EscapedPath())
		}
	}
	return -1, ""
}

// RoundTrip observes public metadata and replays only verified read requests.
// id and epoch come from the matched distribution's immutable routing snapshot.
func (c *Controller) RoundTrip(r *http.Request, id string, epoch uint64) (*http.Response, error) {
	repo := c.repos[id]
	if repo == nil || epoch != c.Epoch() || !publicRequest(r, repo) {
		return c.base.RoundTrip(r)
	}
	source, relative := repo.match(r.URL)
	if source < 0 {
		return c.base.RoundTrip(r)
	}
	if manifestPath(relative) && r.URL.RawQuery != "" {
		return c.base.RoundTrip(r)
	}
	repo.mu.Lock()
	if epoch != c.Epoch() {
		repo.mu.Unlock()
		return c.base.RoundTrip(r)
	}
	if repo.source != source {
		repo.source, repo.active = source, -1
		repo.snapshots, repo.verified = map[string]snapshot{}, map[string]string{}
		repo.observations, repo.limited = map[string]uint64{}, false
	}
	selected := source
	if repo.active >= 0 && c.verifiedFor(repo, relative) {
		selected = repo.active
	}
	// A failed body can only be detected after the response has been returned.
	// The next request may switch before exposing another incomplete response.
	ready := repo.bodyFailures[selected] && repo.failures[selected] >= c.cfg.FailureThreshold
	repo.mu.Unlock()
	if ready {
		res, err := c.failover(r, repo, source, selected, relative, epoch, nil, errors.New("mirror response body incomplete"))
		if res != nil || r.Context().Err() != nil {
			return res, err
		}
		// Unknown/divergent snapshots cannot authorize a switch. Keep using the
		// configured route so a complete response can establish recovery.
	}
	res, err := c.attempt(r, repo, selected, relative, epoch)
	if !failed(res, err) || r.Context().Err() != nil || errors.Is(err, access.ErrUpstreamDenied) {
		return res, err
	}
	repo.mu.Lock()
	ready = false
	if epoch == c.Epoch() {
		repo.failures[selected]++
		ready = repo.failures[selected] >= c.cfg.FailureThreshold
	}
	repo.mu.Unlock()
	if !ready {
		return res, err
	}
	return c.failover(r, repo, source, selected, relative, epoch, res, err)
}

func failed(res *http.Response, err error) bool {
	return err != nil || (res != nil && res.StatusCode >= 500 && res.StatusCode <= 599)
}

func (c *Controller) attempt(r *http.Request, repo *repository, selected int, relative string, epoch uint64) (*http.Response, error) {
	req := r
	if !sameOrigin(r.URL, repo.bases[selected]) || !strings.HasPrefix(r.URL.EscapedPath(), repo.bases[selected].EscapedPath()) {
		req = r.Clone(r.Context())
		req.URL = mappedURL(repo.bases[selected], relative, r.URL)
		req.Host = req.URL.Host
		// Operational IDs and hop-by-hop fields may be present on the source
		// request, but only explicitly public read headers cross mirror roots.
		for key := range req.Header {
			if !publicHeaders[http.CanonicalHeaderKey(key)] {
				delete(req.Header, key)
			}
		}
	}
	res, err := c.base.RoundTrip(req)
	if res != nil {
		res.Request = r
	}
	if !failed(res, err) && res != nil {
		c.observe(r, res, repo, selected, relative, epoch)
		c.observeHealth(r, res, repo, selected, epoch)
	}
	return res, err
}

func mappedURL(base *url.URL, relative string, original *url.URL) *url.URL {
	u := *base
	u.RawPath = base.EscapedPath() + relative
	u.Path, _ = url.PathUnescape(u.RawPath)
	u.RawQuery, u.ForceQuery = original.RawQuery, original.ForceQuery
	return &u
}

// An unknown header can carry a vendor token; cross-origin requests therefore
// require this read-only allowlist, as well as explicitly public query keys.
var publicHeaders = map[string]bool{
	"Accept": true, "Accept-Encoding": true, "Accept-Language": true,
	"User-Agent": true, "Cache-Control": true, "Pragma": true,
	"Range": true, "If-Range": true, "If-None-Match": true, "If-Modified-Since": true,
	"If-Match": true, "If-Unmodified-Since": true,
	"Traceparent": true, "Tracestate": true,
}

func publicRequest(r *http.Request, repo *repository) bool {
	if r.URL == nil || r.URL.User != nil || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
		return false
	}
	if r.ContentLength > 0 || r.ContentLength < 0 || len(r.TransferEncoding) > 0 || len(r.Trailer) > 0 {
		return false
	}
	for key := range r.Header {
		// Fiber's adapter retains framing fields in Header; net/http later
		// consumes them separately. They are stripped from replacement headers.
		switch http.CanonicalHeaderKey(key) {
		case "X-Request-Id", "Connection", "Host", "Content-Length":
			continue
		}
		if !publicHeaders[http.CanonicalHeaderKey(key)] {
			return false
		}
	}
	query, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return false
	}
	for key := range query {
		if !repo.queries[key] {
			return false
		}
	}
	return true
}

func privateResponse(h http.Header) bool {
	if len(h.Values("Set-Cookie")) > 0 {
		return true
	}
	for _, value := range h.Values("Cache-Control") {
		for _, directive := range strings.Split(value, ",") {
			name := strings.ToLower(strings.TrimSpace(strings.SplitN(directive, "=", 2)[0]))
			if name == "private" || name == "no-store" {
				return true
			}
		}
	}
	return false
}

// State reports configured public endpoints and bounded process-local health.
type State struct {
	Enabled      bool              `json:"enabled"`
	Repositories []RepositoryState `json:"repositories"`
}
type RepositoryState struct {
	ID            string    `json:"id"`
	Source        string    `json:"source"`
	Active        string    `json:"active"`
	Failures      []int     `json:"failures"`
	Mirrors       []string  `json:"mirrors"`
	SnapshotCount int       `json:"snapshot_count"`
	CooldownUntil time.Time `json:"cooldown_until"`
	LastDecision  string    `json:"last_decision"`
}

// State returns an independent, deterministically ordered status snapshot.
func (c *Controller) State() State {
	result := State{Enabled: c != nil, Repositories: []RepositoryState{}}
	if c == nil {
		return result
	}
	ids := make([]string, 0, len(c.repos))
	for id := range c.repos {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		repo := c.repos[id]
		repo.mu.Lock()
		entry := RepositoryState{ID: id, Failures: append([]int(nil), repo.failures...), SnapshotCount: len(repo.snapshots), CooldownUntil: repo.nextProbe, LastDecision: repo.reason}
		for _, base := range repo.bases {
			entry.Mirrors = append(entry.Mirrors, base.String())
		}
		if repo.source >= 0 {
			entry.Source = repo.bases[repo.source].String()
		}
		if repo.active >= 0 {
			entry.Active = repo.bases[repo.active].String()
		} else {
			entry.Active = entry.Source
		}
		repo.mu.Unlock()
		result.Repositories = append(result.Repositories, entry)
	}
	return result
}

func (c *Controller) decision(repo *repository, epoch uint64, reason string) {
	repo.mu.Lock()
	if epoch == c.Epoch() {
		repo.reason = reason
	}
	repo.mu.Unlock()
	c.log.Debug().Str("repository", repo.id).Str("decision", reason).Msg("mirror health decision")
}

func closeResponse(res *http.Response) {
	if res != nil && res.Body != nil {
		_ = res.Body.Close()
	}
}

func acquire(ctx context.Context, lock chan struct{}) bool {
	select {
	case lock <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}
