package cli

import (
	"context"

	"github.com/gofiber/fiber/v2"
	health "github.com/soulteary/health-kit"
)

// fiberHealthHandler is a Fiber-native replacement for health.FiberHandler that
// avoids passing the fasthttp *RequestCtx down into health-kit. The upstream
// helper calls aggregator.Check(c.Context()), and aggregator.Check then calls
// context.WithTimeout(parent, ...) which spawns a goroutine reading
// parent.Done(). When the request finishes, fasthttp recycles the *RequestCtx
// and ServerShutdown writes its internal state — the race detector flags those
// two accesses even though they are benign (the child cancel has fired).
//
// We use context.Background() because aggregator.Check creates its own
// timeout via Aggregator.config.Timeout; the request lifetime is irrelevant
// to a health probe.
func fiberHealthHandler(aggregator *health.Aggregator) fiber.Handler {
	return func(c *fiber.Ctx) error {
		cfg := aggregator.Config()

		if len(cfg.IPWhitelist) > 0 {
			clientIP := c.IP()
			if !cfg.IsIPAllowed(clientIP) {
				return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
					"error": "Forbidden",
				})
			}
		}

		result := aggregator.Check(context.Background())

		status := health.HTTPStatusCode(result.Status)
		if !cfg.IncludeDetails {
			return c.Status(status).JSON(fiber.Map{
				"status":  result.Status,
				"service": result.Service,
			})
		}

		if !cfg.IncludeChecks {
			result.Checks = nil
		}
		return c.Status(status).JSON(result)
	}
}
