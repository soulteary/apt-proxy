package mirrorhealth

import (
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
)

func (c *Controller) observeHealth(req *http.Request, res *http.Response, repo *repository, selected int, epoch uint64) {
	source, _ := repo.match(req.URL)
	finish := func(complete bool) {
		if !complete && errors.Is(req.Context().Err(), context.Canceled) {
			return // Client cancellation does not establish an upstream failure.
		}
		repo.mu.Lock()
		defer repo.mu.Unlock()
		if epoch != c.Epoch() || repo.source != source ||
			(repo.active >= 0 && selected != repo.active) || (repo.active < 0 && selected != repo.source) {
			return
		}
		if complete {
			repo.failures[selected], repo.bodyFailures[selected] = 0, false
		} else {
			repo.failures[selected]++
			repo.bodyFailures[selected] = true
		}
	}
	if req.Method == http.MethodHead || res.StatusCode == 204 || res.StatusCode == 304 {
		finish(true)
		return
	}
	if res.Body == nil || res.Body == http.NoBody {
		finish(res.ContentLength <= 0)
		return
	}
	res.Body = &healthBody{ReadCloser: res.Body, ctx: req.Context(), expected: res.ContentLength, finish: finish}
}

// Receiving successful headers is insufficient evidence of mirror recovery.
// Count a terminal body failure once, and reset only at a complete EOF.
type healthBody struct {
	io.ReadCloser
	ctx            context.Context
	expected, read int64
	finish         func(bool)
	once           sync.Once
}

func (b *healthBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.read += int64(n)
	if err != nil {
		complete := err == io.EOF && (b.expected < 0 || b.read == b.expected)
		b.once.Do(func() { b.finish(complete) })
		if err == io.EOF && !complete {
			err = io.ErrUnexpectedEOF
		}
	}
	return n, err
}

func (b *healthBody) Close() error {
	if errors.Is(b.ctx.Err(), context.DeadlineExceeded) {
		b.once.Do(func() { b.finish(false) })
	}
	return b.ReadCloser.Close()
}
