package cli

import (
	"context"
	"sync"
)

// A middleware context can be shared by many requests. Reuse its cancellation
// channel rather than attaching a server-shutdown callback to every request.
// Each request still retains its own values and deadline, and asynchronous
// cache publication remains cancellable after the HTTP handler returns.
type repositoryContextOwner struct {
	lifecycle context.Context
	mu        sync.Mutex
	domains   map[<-chan struct{}]*repositoryCancellationDomain
	closed    bool
}

type repositoryCancellationDomain struct {
	signal     context.Context
	cancel     context.CancelCauseFunc
	setCause   context.CancelCauseFunc
	once       sync.Once
	err        error
	stopParent func() bool
}

func newRepositoryContextOwner(lifecycle context.Context) *repositoryContextOwner {
	owner := &repositoryContextOwner{lifecycle: lifecycle, domains: make(map[<-chan struct{}]*repositoryCancellationDomain)}
	if lifecycle.Done() != nil {
		context.AfterFunc(lifecycle, owner.shutdown)
	}
	return owner
}

func (o *repositoryContextOwner) forRequest(user context.Context) context.Context {
	if o.lifecycle.Done() == nil || user.Err() != nil {
		return user
	}
	done := user.Done()
	if done == nil || done == o.lifecycle.Done() || o.lifecycle.Err() != nil {
		return repositoryLifecycleContext{Context: user, signal: o.lifecycle}
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.closed {
		return repositoryLifecycleContext{Context: user, signal: o.lifecycle}
	}
	domain := o.domains[done]
	if domain == nil {
		signal, cancel := context.WithCancelCause(context.Background())
		causeIdentity, setCause := context.WithCancelCause(context.Background())
		domain = &repositoryCancellationDomain{cancel: cancel, setCause: setCause}
		domain.signal = repositoryDomainContext{Context: signal, causeIdentity: causeIdentity, domain: domain}
		o.domains[done] = domain
		domain.stopParent = context.AfterFunc(user, func() {
			o.cancelDomain(done, domain, user.Err(), context.Cause(user))
		})
	}
	return repositoryLifecycleContext{Context: user, signal: domain.signal}
}

func (o *repositoryContextOwner) cancelDomain(done <-chan struct{}, domain *repositoryCancellationDomain, err, cause error) {
	o.mu.Lock()
	if o.domains[done] == domain {
		delete(o.domains, done)
	}
	o.mu.Unlock()
	domain.finish(err, cause)
}

func (o *repositoryContextOwner) shutdown() {
	o.mu.Lock()
	domains := o.domains
	o.domains = make(map[<-chan struct{}]*repositoryCancellationDomain)
	o.closed = true
	o.mu.Unlock()
	for _, domain := range domains {
		domain.stopParent()
		domain.finish(o.lifecycle.Err(), context.Cause(o.lifecycle))
	}
}

func (d *repositoryCancellationDomain) finish(err, cause error) {
	d.once.Do(func() {
		d.err = err
		d.setCause(cause)
		d.cancel(cause)
	})
}

type repositoryDomainContext struct {
	context.Context
	causeIdentity context.Context
	domain        *repositoryCancellationDomain
}

func (c repositoryDomainContext) Err() error {
	if c.Context.Err() == nil {
		return nil
	}
	return c.domain.err
}

// Keep cause identity separate from Done. A WithCancel child must use our
// Err (which can be DeadlineExceeded), rather than the cancellation signal's
// Canceled error. AfterFunc provides cancellation without a bridge goroutine.
func (c repositoryDomainContext) Value(key any) any { return c.causeIdentity.Value(key) }

func (c repositoryDomainContext) AfterFunc(f func()) func() bool {
	return context.AfterFunc(c.Context, f)
}

// The signal contains only cancellation identity, so consult it first for
// Go's private cancellation key. This preserves context.Cause on shutdown
// while all application values and deadlines come from the user's context.
type repositoryLifecycleContext struct {
	context.Context
	signal context.Context
}

func (c repositoryLifecycleContext) Done() <-chan struct{} { return c.signal.Done() }

func (c repositoryLifecycleContext) Err() error {
	return c.signal.Err()
}

func (c repositoryLifecycleContext) Value(key any) any {
	if value := c.signal.Value(key); value != nil {
		return value
	}
	return c.Context.Value(key)
}

func (c repositoryLifecycleContext) AfterFunc(f func()) func() bool {
	return context.AfterFunc(c.signal, f)
}
