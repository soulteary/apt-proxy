package cli

import (
	"context"
	stderrors "errors"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/adaptor"
	health "github.com/soulteary/health-kit"
	logger "github.com/soulteary/logger-kit"
	metrics "github.com/soulteary/metrics-kit"
	middleware "github.com/soulteary/middleware-kit"
	tracing "github.com/soulteary/tracing-kit"
	version "github.com/soulteary/version-kit"

	"github.com/soulteary/apt-proxy/internal/api"
	"github.com/soulteary/apt-proxy/internal/config"
	"github.com/soulteary/apt-proxy/internal/distro"
	apperrors "github.com/soulteary/apt-proxy/internal/errors"
	"github.com/soulteary/apt-proxy/internal/proxy"
	httpcache "github.com/soulteary/httpcache-kit"
	vfs "github.com/soulteary/vfs-kit"
)

// Server represents the main application server that handles HTTP requests,
// manages caching, and coordinates all server components.
type Server struct {
	config              *config.Config           // Application configuration
	cache               httpcache.ExtendedCache  // HTTP cache implementation with management capabilities
	proxy               *proxy.PackageStruct     // Main proxy router (Handler is cache-wrapped)
	app                 *fiber.App               // Fiber application
	log                 *logger.Logger           // Structured logger
	healthAggregator    *health.Aggregator       // Health check aggregator
	metricsRegistry     *metrics.Registry        // Prometheus metrics registry
	versionInfo         *version.Info            // Version information
	cacheHandler        *api.CacheHandler        // Cache API handler
	mirrorsHandler      *api.MirrorsHandler      // Mirrors API handler
	authMiddleware      *api.AuthMiddleware      // API authentication middleware
	rateLimitMiddleware *api.RateLimitMiddleware // API rate limit (per IP)
}

// NewServer creates and initializes a new Server instance with the provided
// configuration. It sets up caching, proxy routing, logging, and HTTP proxy.
// Returns an error if initialization fails.
func NewServer(cfg *config.Config) (*Server, error) {
	if cfg == nil {
		return nil, errNilConfig
	}

	s := &Server{
		config: cfg,
	}

	// Initialize structured logger first
	s.initLogger()

	if err := s.initialize(); err != nil {
		return nil, wrapErr(apperrors.ErrServerInit, "failed to initialize server", err)
	}

	// Initialize tracing (optional, only if OTLP endpoint is configured)
	// Must be called after initialize() because versionInfo is initialized there
	s.initTracing()

	return s, nil
}

// initLogger initializes the structured logger with configuration from environment
func (s *Server) initLogger() {
	// Determine log level from environment or debug flag. Honour the
	// canonical APT_PROXY_LOG_LEVEL first, fall back to the legacy LOG_LEVEL
	// for backwards compatibility.
	levelEnv := config.EnvLogLevel
	if os.Getenv(levelEnv) == "" && os.Getenv(config.EnvLogLevelLegacy) != "" {
		levelEnv = config.EnvLogLevelLegacy
	}
	level := logger.ParseLevelFromEnv(levelEnv, logger.InfoLevel)
	if s.config.Debug {
		level = logger.DebugLevel
	}

	// Determine log format from environment (same legacy fallback as level).
	formatStr := os.Getenv(config.EnvLogFormat)
	if formatStr == "" {
		formatStr = os.Getenv(config.EnvLogFormatLegacy)
	}
	format := logger.ParseFormat(formatStr)

	// Create logger with configuration
	s.log = logger.New(logger.Config{
		Level:       level,
		Output:      os.Stdout,
		Format:      format,
		ServiceName: "apt-proxy",
	})

	// Set as default logger
	logger.SetDefault(s.log)
}

// initTracing initializes OpenTelemetry tracing if OTLP endpoint is configured
func (s *Server) initTracing() {
	otlpEndpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if otlpEndpoint == "" {
		s.log.Debug().Msg("tracing disabled: OTEL_EXPORTER_OTLP_ENDPOINT not set")
		return
	}

	serviceVersion := s.versionInfo.String()
	if serviceVersion == "" {
		serviceVersion = "unknown"
	}

	tp, err := tracing.InitTracer("apt-proxy", serviceVersion, otlpEndpoint)
	if err != nil {
		s.log.Warn().Err(err).Msg("failed to initialize tracing, continuing without tracing")
		return
	}

	if tp != nil {
		s.log.Info().
			Str("endpoint", otlpEndpoint).
			Str("version", serviceVersion).
			Msg("tracing initialized successfully")
	}
}

// initialize sets up all server components including cache, proxy router,
// logging, and HTTP server configuration. This method is called automatically
// by NewServer and should not be called directly.
func (s *Server) initialize() error {
	// Optional: log vfs file-close errors (e.g. from finalizer)
	if s.log != nil {
		vfs.LogCloseError = func(err error) { s.log.Error().Err(err).Msg("vfs: error closing file") }
	}

	// Initialize version info using ldflags-injected build metadata when set,
	// otherwise fall back to the version-kit defaults.
	if v, c, d := BuildInfo(); v != "dev" || c != "none" || d != "unknown" {
		s.versionInfo = version.New(v, c, d)
	} else {
		s.versionInfo = version.Default()
	}

	// Initialize cache with configuration
	cacheConfig := s.buildCacheConfig()
	cache, err := httpcache.NewDiskCacheWithConfig(s.config.CacheDir, cacheConfig)
	if err != nil {
		return wrapErr(apperrors.ErrCacheInit, "failed to initialize cache", err)
	}
	s.cache = cache

	// Initialize metrics registry
	s.metricsRegistry = metrics.NewRegistry("apt_proxy")

	// Initialize cache metrics
	httpcache.NewCacheMetrics(s.metricsRegistry)

	// Initialize health check aggregator
	s.initHealthChecks()

	// Load distributions config (distributions.yaml) if path set; overlays built-in
	if err := distro.ReloadDistributionsConfig(s.config.DistributionsConfigPath); err != nil {
		s.log.Warn().
			Err(err).
			Str("path", s.config.DistributionsConfigPath).
			Msg("failed to load distributions config; using built-in defaults")
	}

	// Configure upstream transport (keep-alive to mirrors; default true)
	proxy.InitUpstreamTransport(s.config.UpstreamKeepAlive)

	// Initialize proxy with async benchmark for faster startup
	// This uses default mirrors immediately and updates to the fastest mirror
	// in the background after benchmarking completes
	s.proxy = proxy.CreatePackageStructRouterAsync(s.config.CacheDir, s.log)

	// Wrap proxy with cache (request logging is done by logger-kit FiberMiddleware)
	cachedHandler := httpcache.NewHandlerWithOptions(s.cache, s.proxy.Handler, &httpcache.HandlerOptions{Logger: s.log})
	s.proxy.Handler = cachedHandler

	if s.config.Debug {
		s.log.Debug().Msg("debug mode enabled")
		httpcache.SetDebugLogging(true)
	}

	// Initialize API handlers (mirrors refresh also reloads distributions config when path set)
	s.cacheHandler = api.NewCacheHandler(s.cache, s.log)
	s.mirrorsHandler = api.NewMirrorsHandler(s.log, func() {
		if err := distro.ReloadDistributionsConfig(s.config.DistributionsConfigPath); err != nil {
			s.log.Warn().
				Err(err).
				Str("path", s.config.DistributionsConfigPath).
				Msg("failed to reload distributions config")
		}
		// Prefer the per-instance refresh path; falls back to the package
		// global only if proxy isn't initialized yet (shouldn't happen here).
		if s.proxy != nil {
			s.proxy.RefreshMirrors()
		} else {
			proxy.RefreshMirrors()
		}
	})

	// Both middlewares need to agree on what counts as the "real" client
	// IP. Construct the extractor once and share it; otherwise auth logs
	// could attribute a request to r.RemoteAddr (the proxy) while
	// rate-limit logs attributed it to the XFF left-most IP, making it
	// impossible to correlate forensic events.
	clientIP := api.NewClientIPExtractor(s.config.Security.TrustedProxies)

	s.authMiddleware = api.NewAuthMiddleware(api.AuthConfig{
		APIKey:   s.config.Security.APIKey,
		Logger:   s.log,
		ClientIP: clientIP,
	})

	s.rateLimitMiddleware = api.NewRateLimitMiddleware(
		s.config.Security.APIRateLimitPerMinute,
		s.log,
		s.config.Security.TrustedProxies...,
	)

	// Create Fiber app with all routes
	s.app = s.createFiberApp()

	return nil
}

// initHealthChecks initializes the health check aggregator
func (s *Server) initHealthChecks() {
	cfg := health.DefaultConfig().
		WithServiceName("apt-proxy").
		WithTimeout(2 * time.Second)

	s.healthAggregator = health.NewAggregator(cfg)

	// Add cache directory check
	s.healthAggregator.AddChecker(health.NewCustomChecker("cache", func(ctx context.Context) error {
		_, err := os.Stat(s.config.CacheDir)
		return err
	}).WithTimeout(1 * time.Second))
}

// buildCacheConfig creates a cache configuration from the application config
func (s *Server) buildCacheConfig() *httpcache.CacheConfig {
	cacheConfig := httpcache.DefaultCacheConfig()

	// Apply custom settings if provided
	if s.config.Cache.MaxSize > 0 {
		cacheConfig.WithMaxSize(s.config.Cache.MaxSize)
	}
	if s.config.Cache.TTL > 0 {
		cacheConfig.WithTTL(s.config.Cache.TTL)
	}
	if s.config.Cache.CleanupInterval > 0 {
		cacheConfig.WithCleanupInterval(s.config.Cache.CleanupInterval)
	}

	return cacheConfig
}

// Default Fiber server timeouts and buffer sizes.
const (
	defaultReadTimeout  = 50 * time.Second
	defaultWriteTimeout = 100 * time.Second
	defaultIdleTimeout  = 120 * time.Second
	defaultReadBufSize  = 4096 * 4 // 16KB, align with former ReadHeaderTimeout behavior
)

// cacheLabelFromHeader normalizes X-Cache header to HIT/MISS/SKIP for logging.
func cacheLabelFromHeader(h string) string {
	h = strings.TrimSpace(h)
	if h == "" {
		return "SKIP"
	}
	if strings.HasPrefix(h, "HIT") {
		return "HIT"
	}
	if strings.HasPrefix(h, "MISS") {
		return "MISS"
	}
	return h
}

// createFiberApp creates the Fiber application with all routes and middleware.
func (s *Server) createFiberApp() *fiber.App {
	app := fiber.New(fiber.Config{
		DisableStartupMessage: true,
		ReadTimeout:           defaultReadTimeout,
		WriteTimeout:          defaultWriteTimeout,
		IdleTimeout:           defaultIdleTimeout,
		ReadBufferSize:        defaultReadBufSize,
	})

	// Version headers for all responses
	app.Use(version.FiberMiddleware(s.versionInfo, "X-"))
	// Security headers
	app.Use(middleware.SecurityHeaders(middleware.DefaultSecurityHeadersConfig()))

	// Request logging: logger-kit FiberMiddleware, unified with request_id and cache/size for proxy
	logCfg := logger.DefaultMiddlewareConfig()
	logCfg.Logger = s.log
	logCfg.SkipPaths = []string{"/healthz", "/livez", "/readyz"} // skip health noise
	if s.config.Debug {
		logCfg.IncludeHeaders = true
		logCfg.IncludeBody = true
	}
	logCfg.CustomFieldsFiber = func(c *fiber.Ctx) map[string]interface{} {
		// Use Content-Length header when available so we don't pull the
		// (potentially streamed) body into memory just to record its size.
		size := c.Response().Header.ContentLength()
		if size <= 0 {
			size = len(c.Response().Body())
		}
		return map[string]interface{}{
			"cache": cacheLabelFromHeader(string(c.Response().Header.Peek("X-Cache"))),
			"size":  size,
		}
	}
	app.Use(logger.FiberMiddleware(logCfg))

	// Health check endpoints (Fiber native)
	// We deliberately use a local handler instead of health.FiberHandler /
	// health.FiberReadinessHandler: the upstream helpers feed the fasthttp
	// *RequestCtx into context.WithTimeout, which spawns a propagateCancel
	// goroutine that races with fiber/fasthttp's ShutdownWithContext during
	// graceful shutdown (see internal/cli/health.go). Liveness has no
	// aggregator and is safe to use as-is.
	app.Get("/healthz", fiberHealthHandler(s.healthAggregator))
	app.Get("/livez", health.FiberLivenessHandler("apt-proxy"))
	app.Get("/readyz", fiberHealthHandler(s.healthAggregator))

	// Version endpoint (Fiber native)
	app.Get("/version", version.FiberHandler(version.HandlerConfig{
		Info:   s.versionInfo,
		Pretty: true,
	}))

	// Metrics (wrap net/http handler via adaptor)
	app.Get("/metrics", adaptor.HTTPHandler(metrics.HandlerFor(s.metricsRegistry)))

	// Cache & mirrors API (rate limit then auth)
	apiHandler := func(h http.HandlerFunc) http.Handler {
		return s.rateLimitMiddleware.Wrap(s.authMiddleware.WrapFunc(h))
	}
	app.All("/api/cache/stats", adaptor.HTTPHandler(apiHandler(s.cacheHandler.HandleCacheStats)))
	app.All("/api/cache/purge", adaptor.HTTPHandler(apiHandler(s.cacheHandler.HandleCachePurge)))
	app.All("/api/cache/cleanup", adaptor.HTTPHandler(apiHandler(s.cacheHandler.HandleCacheCleanup)))
	app.All("/api/mirrors/refresh", adaptor.HTTPHandler(apiHandler(s.mirrorsHandler.HandleMirrorsRefresh)))

	// Ping (/_/ping and /_/ping/ and /_/ping/...)
	pingHandler := func(c *fiber.Ctx) error {
		c.Set("Content-Type", "text/plain; charset=utf-8")
		return c.SendString("pong")
	}
	app.All("/_/ping", pingHandler)
	app.All("/_/ping/*", pingHandler)

	// Root "/" -> home page (Fiber native)
	app.Get("/", func(c *fiber.Ctx) error {
		tpl, status := proxy.RenderInternalUrls("/", s.config.CacheDir)
		c.Set("Content-Type", "text/html; charset=utf-8")
		c.Status(status)
		return c.SendString(tpl)
	})
	// All other paths -> proxy + cache
	app.All("/*", adaptor.HTTPHandler(s.proxy.Handler))

	return app
}

// Start begins serving HTTP requests and handles graceful shutdown on SIGINT or SIGTERM.
// It also handles SIGHUP for configuration hot reload.
// The server runs in a goroutine while the main goroutine waits for shutdown signals.
// Returns an error if the server fails to start or encounters a fatal error.
func (s *Server) Start() error {
	protocol := "http"
	if s.config.TLS.Enabled {
		protocol = "https"
	}
	s.log.Info().
		Str("version", s.versionInfo.String()).
		Str("listen", s.config.Listen).
		Str("protocol", protocol).
		Msg("starting apt-proxy")

	// Setup graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Setup SIGHUP for configuration reload
	sighupChan := make(chan os.Signal, 1)
	signal.Notify(sighupChan, syscall.SIGHUP)
	defer signal.Stop(sighupChan)

	// Start Fiber in goroutine
	serverErr := make(chan error, 1)
	go func() {
		var err error
		if s.config.TLS.Enabled {
			s.log.Info().
				Str("cert", s.config.TLS.CertFile).
				Str("key", s.config.TLS.KeyFile).
				Msg("starting HTTPS server with TLS")
			err = s.app.ListenTLS(s.config.Listen, s.config.TLS.CertFile, s.config.TLS.KeyFile)
		} else {
			err = s.app.Listen(s.config.Listen)
		}
		if err != nil {
			serverErr <- err
		}
	}()

	s.log.Info().Msg("server started successfully")
	s.log.Info().Msg("send SIGHUP to reload mirror configurations")

	// Run reload work asynchronously and debounce bursts (e.g. systemd
	// occasionally fires SIGHUP twice in quick succession). reloadPending
	// coalesces all signals received while a previous reload is still
	// running into a single follow-up reload. reloadDone signals completion
	// and is consumed only by the main loop.
	const reloadDebounce = 500 * time.Millisecond
	var (
		reloadInFlight bool
		reloadPending  bool
		reloadTimer    *time.Timer
		reloadDone     = make(chan struct{}, 1)
	)
	scheduleReload := func() {
		if reloadInFlight {
			reloadPending = true
			return
		}
		if reloadTimer != nil {
			reloadTimer.Stop()
		}
		reloadInFlight = true
		reloadTimer = time.AfterFunc(reloadDebounce, func() {
			s.reload()
			reloadDone <- struct{}{}
		})
	}

	// Wait for shutdown signal, reload signal, or server error
	for {
		select {
		case err := <-serverErr:
			return wrapErr(apperrors.ErrInternal, "server error", err)
		case <-sighupChan:
			scheduleReload()
		case <-reloadDone:
			reloadInFlight = false
			if reloadPending {
				reloadPending = false
				scheduleReload()
			}
		case <-ctx.Done():
			return s.shutdown()
		}
	}
}

// reload handles configuration hot reload triggered by SIGHUP signal.
// It reloads distributions config (if path set) and refreshes mirror configurations.
func (s *Server) reload() {
	s.log.Info().Msg("received SIGHUP, reloading configuration...")

	if err := distro.ReloadDistributionsConfig(s.config.DistributionsConfigPath); err != nil {
		s.log.Warn().
			Err(err).
			Str("path", s.config.DistributionsConfigPath).
			Msg("failed to reload distributions config")
	}
	if s.proxy != nil {
		s.proxy.RefreshMirrors()
	} else {
		proxy.RefreshMirrors()
	}

	s.log.Info().Msg("configuration reload complete")
}

// shutdown performs a graceful server shutdown with a 5-second timeout.
// It allows in-flight requests to complete before closing the proxy.
// All cleanup steps run unconditionally so a Fiber-shutdown failure does
// not leak the cache file lock or skip the tracing flush.
func (s *Server) shutdown() error {
	s.log.Info().Msg("shutting down proxy...")

	var errs []error

	if err := s.app.ShutdownWithTimeout(5 * time.Second); err != nil {
		s.log.Warn().Err(err).Msg("failed to shutdown server gracefully")
		errs = append(errs, wrapErr(apperrors.ErrInternal, "failed to shutdown server gracefully", err))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Close cache to stop cleanup goroutines and release file locks.
	if s.cache != nil {
		if err := s.cache.Close(); err != nil {
			s.log.Warn().Err(err).Msg("failed to close cache")
			errs = append(errs, wrapErr(apperrors.ErrCacheInit, "failed to close cache", err))
		}
	}

	// Shutdown tracing (flush spans). Always attempt even on prior errors.
	if err := tracing.Shutdown(ctx); err != nil {
		s.log.Warn().Err(err).Msg("failed to shutdown tracing")
		errs = append(errs, wrapErr(apperrors.ErrInternal, "failed to shutdown tracing", err))
	}

	if len(errs) > 0 {
		return stderrors.Join(errs...)
	}

	s.log.Info().Msg("server shutdown complete")
	return nil
}

// Daemon is the main entry point for starting the application daemon.
// It validates the configuration, creates and starts the server, and handles
// any startup errors. This function blocks until the server shuts down.
// Returns an error so the caller can decide how to exit; callers should not
// use log.Fatal here because that bypasses tracing flushers and signal-driven
// cleanup paths inside Server.
func Daemon(flags *config.Config) error {
	log := logger.Default()

	if flags == nil {
		return errNilConfig
	}

	if err := config.ValidateConfig(flags); err != nil {
		return apperrors.Wrap(apperrors.ErrConfigInvalid, "invalid configuration", err)
	}

	srv, err := NewServer(flags)
	if err != nil {
		return wrapErr(apperrors.ErrServerInit, "failed to create server", err)
	}

	// Surface a Warn when API auth is configured-off so operators noticing
	// "/api/* is wide open" in audits aren't surprised.
	if srv.authMiddleware != nil && !srv.authMiddleware.IsEnabled() {
		log.Warn().Msg("API authentication is disabled (no API key configured)")
	}

	if err := srv.Start(); err != nil {
		return wrapErr(apperrors.ErrInternal, "server error", err)
	}
	return nil
}

// Error handling helpers using the unified error system

var errNilConfig = apperrors.New(apperrors.ErrConfigInvalid, "configuration cannot be nil")

// wrapErr wraps err with the given AppError code and message. The previous
// codebase had three near-identical helpers (wrapError/wrapServerError/
// wrapCacheError) that differed only in the code they passed in; collapsing
// them keeps callers honest about which error class they're emitting.
func wrapErr(code apperrors.Code, msg string, err error) error {
	return apperrors.Wrap(code, msg, err)
}
