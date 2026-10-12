package mirrorhealth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"sort"
	"time"
)

func (c *Controller) failover(r *http.Request, repo *repository, source, selected int, relative string, epoch uint64, failedResponse *http.Response, failedError error) (*http.Response, error) {
	// Bound waiting, all manifest probes, and candidate replay through headers
	// under one selection deadline. Body transfer keeps the download context.
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(c.cfg.ProbeTimeoutSeconds)*time.Second)
	defer cancel()
	if !acquire(ctx, repo.selecting) {
		if r.Context().Err() != nil {
			closeResponse(failedResponse)
			return nil, r.Context().Err()
		}
		return failedResponse, failedError
	}
	defer func() { <-repo.selecting }()
	repo.mu.Lock()
	if epoch != c.Epoch() || repo.source != source {
		repo.mu.Unlock()
		return failedResponse, failedError
	}
	// Another request may already have selected a verified replacement.
	if repo.active >= 0 && repo.active != selected && c.verifiedFor(repo, relative) {
		next := repo.active
		repo.mu.Unlock()
		res, err := c.replay(ctx, r, repo, next, relative, epoch)
		if successfulReplay(res, err) {
			closeResponse(failedResponse)
			return res, nil
		}
		closeResponse(res)
		return failedResponse, failedError
	}
	if time.Now().Before(repo.nextProbe) {
		repo.reason = "cooldown"
		repo.mu.Unlock()
		return failedResponse, failedError
	}
	snapshots, known := c.snapshotCopy(repo, relative)
	if !known {
		repo.reason = "unknown-or-expired-snapshot"
		repo.mu.Unlock()
		return failedResponse, failedError
	}
	repo.nextProbe = time.Now().Add(time.Duration(c.cfg.CooldownSeconds) * time.Second)
	repo.reason = "probing"
	repo.mu.Unlock()
	for next := range repo.bases {
		if next == selected {
			continue
		}
		verified, reason := c.verify(ctx, repo, next, snapshots)
		if !verified {
			c.decision(repo, epoch, reason)
			continue
		}
		if epoch != c.Epoch() || r.Context().Err() != nil {
			break
		}
		res, err := c.replay(ctx, r, repo, next, relative, epoch)
		if !successfulReplay(res, err) {
			closeResponse(res)
			c.decision(repo, epoch, "candidate-response-rejected")
			continue
		}
		repo.mu.Lock()
		current := epoch == c.Epoch() && repo.source == source && !repo.limited && len(repo.snapshots) == len(snapshots) && len(repo.observations) == len(snapshots)
		for key, snap := range snapshots {
			if repo.snapshots[key].hash != snap.hash || time.Since(snap.observed) > time.Duration(c.cfg.SnapshotMaxAgeSeconds)*time.Second {
				current = false
			}
		}
		if !current {
			repo.mu.Unlock()
			closeResponse(res)
			break
		}
		repo.active, repo.verified, repo.reason = next, map[string]string{}, "switched"
		// Empty responses finish before the prospective mirror becomes active,
		// so observeHealth's active-mirror guard correctly ignored that earlier
		// completion. Commit its recovery together with the verified selection;
		// body-bearing responses still establish recovery only at complete EOF.
		if r.Method == http.MethodHead || res.StatusCode == http.StatusNoContent || res.StatusCode == http.StatusNotModified ||
			((res.Body == nil || res.Body == http.NoBody) && res.ContentLength <= 0) {
			repo.failures[next], repo.bodyFailures[next] = 0, false
		}
		for key, snap := range snapshots {
			repo.verified[key] = snap.hash
		}
		repo.mu.Unlock()
		c.log.Info().Str("repository", repo.id).Str("from", repo.bases[selected].String()).Str("to", repo.bases[next].String()).Msg("switched to snapshot-verified mirror")
		closeResponse(failedResponse)
		return res, nil
	}
	if r.Context().Err() != nil {
		closeResponse(failedResponse)
		return nil, r.Context().Err()
	}
	return failedResponse, failedError
}

// replay drops the selection cancellation hook only after headers arrive. The
// request context remains a child of the original download context, and the
// returned body owns its cancellation until Close. Canceling selection on
// return must not truncate a successful package that outlives the probe budget.
func (c *Controller) replay(selection context.Context, r *http.Request, repo *repository, next int, relative string, epoch uint64) (*http.Response, error) {
	ctx, cancel := context.WithCancel(r.Context())
	stop := context.AfterFunc(selection, cancel)
	res, err := c.attempt(r.WithContext(ctx), repo, next, relative, epoch)
	if !stop() || selection.Err() != nil {
		cancel()
		closeResponse(res)
		return nil, selection.Err()
	}
	if err != nil || res == nil {
		cancel()
		return res, err
	}
	res.Request = r
	if res.Body == nil || res.Body == http.NoBody {
		cancel()
	} else {
		res.Body = &replayBody{ReadCloser: res.Body, cancel: cancel}
	}
	return res, nil
}

type replayBody struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *replayBody) Close() error {
	defer b.cancel()
	return b.ReadCloser.Close()
}

func successfulReplay(res *http.Response, err error) bool {
	return err == nil && res != nil && ((res.StatusCode >= 200 && res.StatusCode < 300) || res.StatusCode == http.StatusNotModified)
}

func (c *Controller) verify(ctx context.Context, repo *repository, next int, snapshots map[string]snapshot) (bool, string) {
	keys := make([]string, 0, len(snapshots))
	for key := range snapshots {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		u := mappedURL(repo.bases[next], key, repo.bases[next])
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return false, "candidate-url-rejected"
		}
		req.Header.Set("Accept-Encoding", "identity")
		res, err := c.probe.RoundTrip(req)
		if err != nil {
			closeResponse(res)
			return false, "candidate-unreachable"
		}
		if res == nil || res.StatusCode != 200 || res.Body == nil || privateResponse(res.Header) || res.Header.Get("Content-Encoding") != "" || res.Header.Get("Content-Range") != "" {
			closeResponse(res)
			return false, "candidate-manifest-rejected"
		}
		body, err := io.ReadAll(io.LimitReader(res.Body, c.cfg.MaxManifestBytes+1))
		closeResponse(res)
		if err != nil || int64(len(body)) > c.cfg.MaxManifestBytes || (res.ContentLength >= 0 && res.ContentLength != int64(len(body))) {
			return false, "candidate-manifest-incomplete-or-large"
		}
		sum := sha256.Sum256(body)
		if hex.EncodeToString(sum[:]) != snapshots[key].hash {
			return false, "candidate-snapshot-mismatch"
		}
	}
	return true, "verified"
}
