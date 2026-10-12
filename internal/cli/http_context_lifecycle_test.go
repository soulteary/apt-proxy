package cli

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestRepositoryLifecycleUsesSharedDefaultSignal(t *testing.T) {
	lifecycle, shutdown := context.WithCancel(context.Background())
	defer shutdown()
	owner := newRepositoryContextOwner(lifecycle)
	user := context.WithValue(context.Background(), repositoryTraceKey{}, "request-trace")
	retained := owner.forRequest(user)
	if retained.Done() != lifecycle.Done() || retained.Value(repositoryTraceKey{}) != "request-trace" {
		t.Fatal("value-only context did not reuse the server lifecycle signal")
	}
	assertRepositoryDomainCount(t, owner, 0)
	shutdown()
	assertRepositoryCancellation(retained, t, context.Canceled, context.Canceled)
}

func TestRepositoryLifecycleBoundsSharedMiddlewareDomain(t *testing.T) {
	lifecycle, shutdown := context.WithCancel(context.Background())
	defer shutdown()
	parent, cancel := context.WithCancelCause(context.Background())
	owner := newRepositoryContextOwner(lifecycle)
	var retained context.Context
	for i := 0; i < 10000; i++ {
		user := context.WithValue(parent, repositoryTraceKey{}, i)
		next := owner.forRequest(user)
		if next.Value(repositoryTraceKey{}) != i || (retained != nil && next.Done() != retained.Done()) {
			t.Fatalf("request %d lost its value or created a separate cancellation domain", i)
		}
		retained = next
	}
	assertRepositoryDomainCount(t, owner, 1)
	cause := errors.New("middleware stopped")
	cancel(cause)
	assertRepositoryCancellation(retained, t, context.Canceled, cause)
	assertRepositoryDomainCount(t, owner, 0)
}

func TestRepositoryLifecyclePropagatesDeadlineAndCauseToDescendants(t *testing.T) {
	lifecycle, shutdown := context.WithCancel(context.Background())
	defer shutdown()
	cause := errors.New("middleware deadline")
	deadline := time.Now().Add(20 * time.Millisecond)
	parent, cancel := context.WithDeadlineCause(context.Background(), deadline, cause)
	defer cancel()
	owner := newRepositoryContextOwner(lifecycle)
	retained := owner.forRequest(context.WithValue(parent, repositoryTraceKey{}, "original"))
	child, release := context.WithCancel(retained)
	defer release()
	if got, ok := child.Deadline(); !ok || !got.Equal(deadline) {
		t.Fatalf("descendant deadline = %v/%v, want %v", got, ok, deadline)
	}
	assertRepositoryCancellation(retained, t, context.DeadlineExceeded, cause)
	assertRepositoryCancellation(child, t, context.DeadlineExceeded, cause)
	shutdown()
	assertRepositoryCancellation(retained, t, context.DeadlineExceeded, cause)
	assertRepositoryDomainCount(t, owner, 0)
}

func TestRepositoryLifecycleFreezesServerCancellationBeforeLaterDeadline(t *testing.T) {
	lifecycle, shutdown := context.WithCancel(context.Background())
	defer shutdown()
	parent, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	owner := newRepositoryContextOwner(lifecycle)
	retained := owner.forRequest(parent)
	child, release := context.WithCancel(retained)
	defer release()
	shutdown()
	assertRepositoryCancellation(retained, t, context.Canceled, context.Canceled)
	assertRepositoryCancellation(child, t, context.Canceled, context.Canceled)
	<-parent.Done()
	assertRepositoryCancellation(retained, t, context.Canceled, context.Canceled)
	assertRepositoryCancellation(child, t, context.Canceled, context.Canceled)
	assertRepositoryDomainCount(t, owner, 0)
}

func TestRepositoryLifecycleConcurrentCancellationIsStable(t *testing.T) {
	for i := 0; i < 200; i++ {
		lifecycle, shutdown := context.WithCancel(context.Background())
		parent, cancel := context.WithCancelCause(context.Background())
		owner := newRepositoryContextOwner(lifecycle)
		retained := owner.forRequest(parent)
		var finish sync.WaitGroup
		finish.Add(2)
		go func() { defer finish.Done(); shutdown() }()
		cause := errors.New("user cancellation")
		go func() { defer finish.Done(); cancel(cause) }()
		finish.Wait()
		waitRepositoryContext(retained, t)
		firstCause := context.Cause(retained)
		if firstCause != cause && firstCause != context.Canceled {
			t.Fatalf("invalid cancellation winner: %v", firstCause)
		}
		assertRepositoryCancellation(retained, t, context.Canceled, firstCause)
		assertRepositoryDomainCount(t, owner, 0)
	}
}

func waitRepositoryContext(ctx context.Context, t *testing.T) {
	t.Helper()
	select {
	case <-ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("repository context was not canceled")
	}
}

func assertRepositoryCancellation(ctx context.Context, t *testing.T, wantErr, wantCause error) {
	t.Helper()
	waitRepositoryContext(ctx, t)
	if ctx.Err() != wantErr || context.Cause(ctx) != wantCause {
		t.Fatalf("Err=%v Cause=%v, want %v/%v", ctx.Err(), context.Cause(ctx), wantErr, wantCause)
	}
}

func assertRepositoryDomainCount(t *testing.T, owner *repositoryContextOwner, want int) {
	t.Helper()
	owner.mu.Lock()
	count := len(owner.domains)
	owner.mu.Unlock()
	if count != want {
		t.Fatalf("retained cancellation domains = %d, want %d", count, want)
	}
}
