// Copyright 2022 Su Yang
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package cli

import (
	"context"
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	health "github.com/soulteary/health-kit"
)

// healthCheckerFunc adapts a closure into the health.Checker interface so
// tests can construct cheap, synchronous probes without dragging in
// db/redis fakes.
type healthCheckerFunc struct {
	name string
	fn   func(ctx context.Context) health.CheckResult
}

func (h healthCheckerFunc) Name() string                                 { return h.name }
func (h healthCheckerFunc) Check(ctx context.Context) health.CheckResult { return h.fn(ctx) }

// TestFiberHealthHandlerBranches exercises the three branches of
// fiberHealthHandler: IP forbidden, summary-only response (IncludeDetails
// off), and full response (IncludeDetails on with checks). The previous
// tests only ran the full-response path implicitly via daemon_e2e tests.
func TestFiberHealthHandlerBranches(t *testing.T) {
	okChecker := healthCheckerFunc{
		name: "ok",
		fn: func(_ context.Context) health.CheckResult {
			return health.CheckResult{Name: "ok", Status: health.StatusHealthy}
		},
	}

	t.Run("ip-forbidden", func(t *testing.T) {
		// Whitelist a host the test client will never appear from.
		// fiber.Ctx.IP() returns the request RemoteAddr by default, which
		// httptest sets to 192.0.2.1 for in-memory tests.
		cfg := health.DefaultConfig().
			WithServiceName("apt-proxy").
			WithIPWhitelist([]string{"10.255.255.255"})
		agg := health.NewAggregator(cfg).AddChecker(okChecker)

		app := fiber.New()
		app.Get("/healthz", fiberHealthHandler(agg))
		req := httptest.NewRequest("GET", "/healthz", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("app.Test: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != fiber.StatusForbidden {
			t.Errorf("status = %d, want 403", resp.StatusCode)
		}
	})

	t.Run("summary-only", func(t *testing.T) {
		cfg := health.DefaultConfig().
			WithServiceName("apt-proxy").
			WithDetails(false)
		agg := health.NewAggregator(cfg).AddChecker(okChecker)

		app := fiber.New()
		app.Get("/healthz", fiberHealthHandler(agg))
		req := httptest.NewRequest("GET", "/healthz", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("app.Test: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("status = %d, want 200", resp.StatusCode)
		}
		body, _ := io.ReadAll(resp.Body)
		// Summary path drops the "checks" array entirely.
		if strings.Contains(string(body), "\"checks\"") {
			t.Errorf("summary response should omit checks; got %s", body)
		}
	})

	t.Run("details-without-checks", func(t *testing.T) {
		cfg := health.DefaultConfig().
			WithServiceName("apt-proxy").
			WithChecks(false)
		agg := health.NewAggregator(cfg).AddChecker(okChecker)

		app := fiber.New()
		app.Get("/healthz", fiberHealthHandler(agg))
		req := httptest.NewRequest("GET", "/healthz", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("app.Test: %v", err)
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("status = %d, want 200", resp.StatusCode)
		}
	})
}
