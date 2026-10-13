package proxy

import (
	"net/http"
	"strings"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
	"github.com/valyala/fasthttp/fasthttpadaptor"
)

// FiberHTTPHandler preserves apt-cacher-ng's HTTPS/// request syntax before
// Fiber's ordinary net/http adaptor rejects its empty path segments. Only
// recognized rewrite markers use the raw bridge; ordinary requests retain
// the adaptor's routed-path checks. PackageStruct still validates the marker
// origin against the passthrough allowlist before contacting it.
func FiberHTTPHandler(handler http.Handler) fiber.Handler {
	adapted := adaptor.HTTPHandlerWithContext(handler)
	return func(c fiber.Ctx) error {
		if !fiberTLSRewriteMarker(c) {
			return adapted(c)
		}
		ctx := c.Context()
		raw := fasthttpadaptor.NewFastHTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handler.ServeHTTP(w, r.WithContext(ctx))
		}))
		raw(c.RequestCtx())
		return nil
	}
}

func fiberTLSRewriteMarker(c fiber.Ctx) bool {
	// Ordinary repository paths need no second net/http conversion.
	if !strings.Contains(strings.ToUpper(c.OriginalURL()), "HTTPS") && !strings.EqualFold(c.Hostname(), "HTTPS") {
		return false
	}
	var request http.Request
	if err := fasthttpadaptor.ConvertRequest(c.RequestCtx(), &request, true); err != nil {
		return false
	}
	_, _, found := parseTLSRewriteMarker(&request)
	return found
}
