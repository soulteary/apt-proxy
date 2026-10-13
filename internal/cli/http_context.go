package cli

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"

	"github.com/soulteary/apt-proxy/internal/proxy"
)

// repositoryHTTPHandler replaces the adaptor's pooled fasthttp RequestCtx
// before repository work can retain it during asynchronous cache publication.
// Fiber's user context preserves middleware values, deadlines and cancellation
// without referring back to a RequestCtx that is reset after handler return.
// The app-owned lifecycle also cancels retained work before server shutdown.
func repositoryHTTPHandler(lifecycle context.Context, handler http.Handler) fiber.Handler {
	contexts := newRepositoryContextOwner(lifecycle)
	return proxy.FiberHTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, _ := adaptor.LocalContextFromHTTPRequest(r)
		if ctx == nil {
			ctx = r.Context()
		}
		handler.ServeHTTP(w, detachedRepositoryRequest(contexts.forRequest(ctx), r))
	}))
}

// The fasthttp adaptor borrows strings from request buffers. Background cache
// publication retains selection metadata, so cloning only the maps and URL
// struct would still let a later request change an earlier request's Vary key.
// Request bodies retain normal synchronous handler ownership.
func detachedRepositoryRequest(ctx context.Context, r *http.Request) *http.Request {
	owned := r.Clone(ctx)
	owned.Method = strings.Clone(r.Method)
	owned.Proto = strings.Clone(r.Proto)
	owned.Host = strings.Clone(r.Host)
	owned.RemoteAddr = strings.Clone(r.RemoteAddr)
	owned.RequestURI = strings.Clone(r.RequestURI)
	owned.Header = ownedRepositoryHeaders(r.Header)
	owned.Trailer = ownedRepositoryHeaders(r.Trailer)
	for i, encoding := range owned.TransferEncoding {
		owned.TransferEncoding[i] = strings.Clone(encoding)
	}
	if owned.URL != nil {
		owned.URL.Scheme = strings.Clone(owned.URL.Scheme)
		owned.URL.Opaque = strings.Clone(owned.URL.Opaque)
		owned.URL.Host = strings.Clone(owned.URL.Host)
		owned.URL.Path = strings.Clone(owned.URL.Path)
		owned.URL.RawPath = strings.Clone(owned.URL.RawPath)
		owned.URL.RawQuery = strings.Clone(owned.URL.RawQuery)
		owned.URL.Fragment = strings.Clone(owned.URL.Fragment)
		owned.URL.RawFragment = strings.Clone(owned.URL.RawFragment)
		if user := owned.URL.User; user != nil {
			username := strings.Clone(user.Username())
			if password, ok := user.Password(); ok {
				owned.URL.User = url.UserPassword(username, strings.Clone(password))
			} else {
				owned.URL.User = url.User(username)
			}
		}
	}
	return owned
}

func ownedRepositoryHeaders(header http.Header) http.Header {
	if header == nil {
		return nil
	}
	owned := make(http.Header, len(header))
	for name, values := range header {
		copied := make([]string, len(values))
		for i, value := range values {
			copied[i] = strings.Clone(value)
		}
		owned[strings.Clone(name)] = copied
	}
	return owned
}
