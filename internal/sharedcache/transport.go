package sharedcache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"

	httpcache "github.com/soulteary/httpcache-kit/v4"
)

// StageByHash accounts for the verified upstream copy held until response close.
const StageByHash httpcache.StageKind = "shared-byhash"

type verifiedTransport struct {
	next    http.RoundTripper
	policy  *Policy
	tempDir string
	budget  httpcache.StagingBudget
}

func (p *Policy) WrapTransport(next http.RoundTripper, tempDir string) http.RoundTripper {
	return p.WrapTransportWithBudget(next, tempDir, nil)
}

// WrapTransportWithBudget charges the verified by-hash temporary copy until
// its response body closes, including time spent waiting for cache publication.
func (p *Policy) WrapTransportWithBudget(next http.RoundTripper, tempDir string, budget httpcache.StagingBudget) http.RoundTripper {
	if p == nil {
		return next
	}
	return &verifiedTransport{next: next, policy: p, tempDir: tempDir, budget: budget}
}

type verifiedBody struct {
	*os.File
	lease httpcache.StagingLease
	once  sync.Once
	err   error
}

func (b *verifiedBody) Close() error {
	b.once.Do(func() {
		b.err = b.File.Close()
		if b.lease != nil {
			b.lease.Close()
		}
	})
	return b.err
}

type chargedWriter struct {
	ctx   context.Context
	f     *os.File
	lease httpcache.StagingLease
}

func (w *chargedWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	if w.lease != nil {
		if err := w.lease.Reserve(int64(len(p))); err != nil {
			return 0, errors.Join(httpcache.ErrStagingBudgetExceeded, err)
		}
	}
	n, err := w.f.Write(p)
	if w.lease != nil && n < len(p) {
		w.lease.Release(int64(len(p) - n))
	}
	return n, err
}
func (t *verifiedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	_, _, digest, shared := t.policy.content(req)
	if !shared || req.Method != http.MethodGet {
		return t.next.RoundTrip(req)
	}
	out := req.Clone(req.Context())
	out.Header.Set("Accept-Encoding", "identity")
	res, err := t.next.RoundTrip(out)
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return res, nil
	}
	fail := func(err error) (*http.Response, error) { _ = res.Body.Close(); return nil, err }
	limit := t.policy.maxBytes
	if owner, ok := t.budget.(interface{ MaxObjectBytes() int64 }); ok {
		if objectLimit := owner.MaxObjectBytes(); objectLimit > 0 && objectLimit < limit {
			limit = objectLimit
		}
	}
	if res.ContentLength > limit || (res.Header.Get("Content-Encoding") != "" && res.Header.Get("Content-Encoding") != "identity") {
		return fail(fmt.Errorf("shared by-hash representation exceeds limits or changes encoding"))
	}
	var lease httpcache.StagingLease
	if t.budget != nil {
		lease, err = t.budget.NewLease(req.Context(), StageByHash)
		if err != nil || lease == nil {
			if lease != nil {
				lease.Close()
			}
			return fail(errors.Join(httpcache.ErrStagingBudgetExceeded, err))
		}
	}
	f, err := os.CreateTemp(t.tempDir, "apt-byhash-*")
	if err != nil {
		if lease != nil {
			lease.Close()
		}
		return fail(err)
	}
	b := &verifiedBody{File: f, lease: lease}
	if err = os.Remove(f.Name()); err != nil {
		_ = b.Close()
		return fail(err)
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(&chargedWriter{ctx: req.Context(), f: f, lease: lease}, h), io.LimitReader(res.Body, limit+1))
	_ = res.Body.Close()
	if err != nil {
		_ = b.Close()
		return nil, err
	}
	if n > limit || (res.ContentLength >= 0 && res.ContentLength != n) || hex.EncodeToString(h.Sum(nil)) != digest {
		_ = b.Close()
		return nil, fmt.Errorf("shared by-hash body failed complete SHA256 verification")
	}
	if _, err = f.Seek(0, io.SeekStart); err != nil {
		_ = b.Close()
		return nil, err
	}
	res.Body = b
	res.ContentLength = n
	return res, nil
}
