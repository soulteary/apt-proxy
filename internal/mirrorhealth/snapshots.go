package mirrorhealth

import (
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

var suiteName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

func manifestPath(relative string) bool {
	parts := strings.Split(relative, "/")
	return len(parts) == 3 && parts[0] == "dists" && len(parts[1]) <= 128 && suiteName.MatchString(parts[1]) && (parts[2] == "InRelease" || parts[2] == "Release")
}

func (c *Controller) observe(req *http.Request, res *http.Response, repo *repository, selected int, relative string, epoch uint64) {
	if req.Method != http.MethodGet || req.URL.RawQuery != "" || req.Header.Get("Range") != "" || res.StatusCode != 200 || res.Body == nil || !manifestPath(relative) || privateResponse(res.Header) || res.Header.Get("Content-Range") != "" || res.Header.Get("Content-Encoding") != "" {
		return
	}
	source, _ := repo.match(req.URL)
	var version uint64
	res.Body = &observedBody{ReadCloser: res.Body, hash: sha256.New(), max: c.cfg.MaxManifestBytes, expected: res.ContentLength, begin: func() {
		repo.mu.Lock()
		defer repo.mu.Unlock()
		if epoch != c.Epoch() || repo.source != source || (selected != repo.source && selected != repo.active) {
			return
		}
		if _, exists := repo.observations[relative]; !exists && len(repo.observations) >= 64 {
			repo.limited, repo.reason = true, "snapshot-limit"
			return
		}
		repo.sequence++
		version = repo.sequence
		repo.observations[relative] = version
		// A new full response may describe a newer snapshot. Until its complete
		// body is known, the previous fingerprint cannot authorize a switch.
		delete(repo.snapshots, relative)
	}, complete: func(sum string) {
		repo.mu.Lock()
		defer repo.mu.Unlock()
		if epoch != c.Epoch() || repo.source != source || (selected != repo.source && selected != repo.active) {
			return
		}
		if version == 0 || repo.observations[relative] != version || repo.limited {
			return
		}
		repo.snapshots[relative] = snapshot{hash: sum, observed: time.Now()}
		if selected == repo.active {
			repo.verified[relative] = sum
		}
	}}
}

// Hash streaming does not pre-read or buffer metadata. Only a complete EOF can
// authorize failover; partial reads, aborts and over-limit bodies cannot do so.
type observedBody struct {
	io.ReadCloser
	hash                hash.Hash
	max, expected, read int64
	invalid             bool
	once                sync.Once
	started             sync.Once
	begin               func()
	complete            func(string)
}

func (b *observedBody) Read(p []byte) (int, error) {
	b.started.Do(b.begin)
	n, err := b.ReadCloser.Read(p)
	b.read += int64(n)
	if b.read > b.max {
		b.invalid = true
	}
	if !b.invalid {
		_, _ = b.hash.Write(p[:n])
	}
	if err != nil && err != io.EOF {
		b.invalid = true
	}
	if err == io.EOF && !b.invalid && b.read > 0 && (b.expected < 0 || b.read == b.expected) {
		b.once.Do(func() { b.complete(hex.EncodeToString(b.hash.Sum(nil))) })
	}
	return n, err
}

// relevantKeys is conservative for pool objects: their path contains no suite,
// so every suite observed in this repository must be known and consistent.
func relevantKeys(repo *repository, relative string) []string {
	var prefix string
	parts := strings.Split(relative, "/")
	if len(parts) >= 3 && parts[0] == "dists" && len(parts[1]) <= 128 && suiteName.MatchString(parts[1]) {
		prefix = "dists/" + parts[1] + "/"
	} else if !strings.HasPrefix(relative, "pool/") {
		return nil
	}
	var keys []string
	for key := range repo.observations {
		if prefix == "" || strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	return keys
}

// Caller holds repo.mu.
func (c *Controller) verifiedFor(repo *repository, relative string) bool {
	keys := relevantKeys(repo, relative)
	if repo.limited || len(keys) == 0 {
		return false
	}
	for _, key := range keys {
		snap := repo.snapshots[key]
		if time.Since(snap.observed) > time.Duration(c.cfg.SnapshotMaxAgeSeconds)*time.Second || repo.verified[key] != snap.hash {
			return false
		}
	}
	return true
}

// Caller holds repo.mu. Switching verifies the complete known repository view.
func (c *Controller) snapshotCopy(repo *repository, relative string) (map[string]snapshot, bool) {
	if repo.limited || len(relevantKeys(repo, relative)) == 0 {
		return nil, false
	}
	result := make(map[string]snapshot, len(repo.snapshots))
	for key := range repo.observations {
		snap, exists := repo.snapshots[key]
		if !exists || time.Since(snap.observed) > time.Duration(c.cfg.SnapshotMaxAgeSeconds)*time.Second {
			return nil, false
		}
		result[key] = snap
	}
	return result, true
}
