package proxy

import (
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/valyala/fasthttp"
)

func TestFiberTLSRewriteMarkerReachesOnlyAllowlistedOrigin(t *testing.T) {
	for _, tc := range []struct {
		name   string
		target string
		origin string
	}{
		{"absolute", "http://HTTPS///get.docker.com/linux/ubuntu/dists/jammy/InRelease", "get.docker.com"},
		{"rewrite", "http://proxy:3142/HTTPS///get.docker.com/linux/ubuntu/dists/jammy/InRelease", "get.docker.com"},
		{"lowercase", "http://https///get.docker.com/linux/ubuntu/dists/jammy/InRelease", "get.docker.com"},
		{"explicit-port", "http://HTTPS:3142///get.docker.com:80/linux/ubuntu/dists/jammy/InRelease", "get.docker.com:80"},
		{"ipv6", "http://HTTPS///[2001:db8::1]/linux/ubuntu/dists/jammy/InRelease", "[2001:db8::1]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ps, seen := passthroughProxy(t, tc.origin)
			const query = "ticket=a%20b&proof=c%2Fd"
			req, err := http.NewRequest(http.MethodGet, tc.target+"?"+query, nil)
			if err != nil {
				t.Fatal(err)
			}
			res, err := fiberApp(ps).Test(req)
			if err != nil {
				t.Fatal(err)
			}
			body, readErr := io.ReadAll(res.Body)
			_ = res.Body.Close()
			if readErr != nil || res.StatusCode != http.StatusOK || string(body) != "InRelease" {
				t.Fatalf("response=%d/%q err=%v", res.StatusCode, body, readErr)
			}
			if seen.count != 1 || seen.scheme != "https" || seen.host != tc.origin || seen.path != "/linux/ubuntu/dists/jammy/InRelease" || seen.query != query {
				t.Fatalf("marker changed upstream selection: %+v", *seen)
			}
		})
	}
}

func TestFiberTLSRewriteMarkerPreservesUserContext(t *testing.T) {
	type contextKey struct{}
	user, cancel := context.WithCancel(context.WithValue(context.Background(), contextKey{}, "original"))
	defer cancel()
	var request fasthttp.RequestCtx
	request.Request.SetRequestURI("http://HTTPS///get.docker.com/repo/InRelease")
	app := fiber.New()
	c := app.AcquireCtx(&request)
	c.SetContext(user)
	var retained context.Context
	handler := FiberHTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		retained = r.Context()
		w.WriteHeader(http.StatusNoContent)
	}))
	if err := handler(c); err != nil {
		t.Fatal(err)
	}
	app.ReleaseCtx(c)
	request.Request.Reset()
	request.SetUserValue(contextKey{}, "another-request")
	if retained != user || retained.Value(contextKey{}) != "original" {
		t.Fatal("raw marker bridge lost the stable middleware context")
	}
	cancel()
	if retained.Err() != context.Canceled {
		t.Fatal("raw marker bridge lost middleware cancellation")
	}
}

func TestFiberRepositoryHandlerKeepsRoutedPathProtection(t *testing.T) {
	var calls int
	app := fiber.New()
	app.All("/*", FiberHTTPHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusNoContent)
	})))
	for _, path := range []string{"//admin", "/public/..%2Fadmin", "/repo/https-everywhere//package.deb"} {
		req, err := http.NewRequest(http.MethodGet, "http://proxy.example"+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		res, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = res.Body.Close()
		if res.StatusCode != http.StatusNotFound || calls != 0 {
			t.Fatalf("ordinary ambiguous path %q reached handler: status=%d calls=%d", path, res.StatusCode, calls)
		}
	}
}
