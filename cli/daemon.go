package cli

import (
	"context"
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
	apperrors "github.com/soulteary/apt-proxy/internal/errors"
	"github.com/soulteary/apt-proxy/internal/proxy"
	"github.com/soulteary/apt-proxy/pkg/httpcache"
)

// Server represents the main application server that handles HTTP requests,
// manages caching, and coordinates all server components.
type Server struct {
	config           *config.Config          // Application configuration
	cache            httpcache.ExtendedCache // HTTP cache implementation with management capabilities
	proxy            *proxy.PackageStruct    // Main proxy router (Handler is cache-wrapped)
	app              *fiber.App              // Fiber application
	log              *logger.Logger          // Structured logger
	healthAggregator *health.Aggregator      // Health check aggregator
	metricsRegistry  *metrics.Registry       // Prometheus metrics registry
	versionInfo      *version.Info           // Version information
	cacheHandler     *api.CacheHandler       // Cache API handler
	mirrorsHandler   *api.MirrorsHandler     // Mirrors API handler
	authMiddleware   *api.AuthMiddleware     // API authentication middleware
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
		return nil, wrapServerError("failed to initialize server", err)
	}

	// Initialize tracing (optional, only if OTLP endpoint is configured)
	// Must be called after initialize() because versionInfo is initialized there
	s.initTracing()

	return s, nil
}

// initLogger initializes the structured logger with configuration from environment
func (s *Server) initLogger() {
	// Determine log level from environment or debug flag
	level := logger.ParseLevelFromEnv(config.EnvLogLevel, logger.InfoLevel)
	if s.config.Debug {
		level = logger.DebugLevel
	}

	// Determine log format from environment
	formatStr := os.Getenv(config.EnvLogFormat)
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
	// Initialize version info
	s.versionInfo = version.Default()

	// Initialize cache with configuration
	cacheConfig := s.buildCacheConfig()
	cache, err := httpcache.NewDiskCacheWithConfig(s.config.CacheDir, cacheConfig)
	if err != nil {
		return wrapCacheError("failed to initialize cache", err)
	}
	s.cache = cache

	// Initialize metrics registry
	s.metricsRegistry = metrics.NewRegistry("apt_proxy")

	// Initialize cache metrics
	httpcache.NewCacheMetrics(s.metricsRegistry)

	// Initialize health check aggregator
	s.initHealthChecks()

	// Initialize proxy with async benchmark for faster startup
	// This uses default mirrors immediately and updates to the fastest mirror
	// in the background after benchmarking completes
	s.proxy = proxy.CreatePackageStructRouterAsync(s.config.CacheDir, s.log)

	// Wrap proxy with cache (request logging is done by logger-kit FiberMiddleware)
	cachedHandler := httpcache.NewHandler(s.cache, s.proxy.Handler)
	s.proxy.Handler = cachedHandler

	if s.config.Debug {
		s.log.Debug().Msg("debug mode enabled")
		httpcache.DebugLogging = true
	}
	httpcache.SetLogger(s.log)

	// Initialize API handlers
	s.cacheHandler = api.NewCacheHandler(s.cache, s.log)
	s.mirrorsHandler = api.NewMirrorsHandler(s.log)

	// Initialize API authentication middleware
	s.authMiddleware = api.NewAuthMiddleware(api.AuthConfig{
		APIKey: s.config.Security.APIKey,
		Logger: s.log,
	})

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
	// CustomFieldsFiber allocates one map per request; use capacity 2 for "cache" and "size".
	// Pooling the map is unsafe unless the logger copies fields before returning.
	logCfg.CustomFieldsFiber = func(c *fiber.Ctx) map[string]interface{} {
		m := make(map[string]interface{}, 2)
		if v := c.Response().Header.Peek("X-Cache"); len(v) > 0 {
			cache := strings.TrimSpace(string(v))
			switch {
			case strings.HasPrefix(cache, "MISS"):
				m["cache"] = "MISS"
			case strings.HasPrefix(cache, "HIT"):
				m["cache"] = "HIT"
			default:
				m["cache"] = cache
			}
		} else {
			m["cache"] = "SKIP"
		}
		m["size"] = len(c.Response().Body())
		return m
	}
	app.Use(logger.FiberMiddleware(logCfg))

	// Health check endpoints (Fiber native)
	app.Get("/healthz", health.FiberHandler(s.healthAggregator))
	app.Get("/livez", health.FiberLivenessHandler("apt-proxy"))
	app.Get("/readyz", health.FiberReadinessHandler(s.healthAggregator))

	// Version endpoint (Fiber native)
	app.Get("/version", version.FiberHandler(version.HandlerConfig{
		Info:   s.versionInfo,
		Pretty: true,
	}))

	// Metrics (wrap net/http handler via adaptor)
	app.Get("/metrics", adaptor.HTTPHandler(metrics.HandlerFor(s.metricsRegistry)))

	// Cache & mirrors API (wrap net/http handlers, auth applied inside)
	app.All("/api/cache/stats", adaptor.HTTPHandler(s.authMiddleware.WrapFunc(s.cacheHandler.HandleCacheStats)))
	app.All("/api/cache/purge", adaptor.HTTPHandler(s.authMiddleware.WrapFunc(s.cacheHandler.HandleCachePurge)))
	app.All("/api/cache/cleanup", adaptor.HTTPHandler(s.authMiddleware.WrapFunc(s.cacheHandler.HandleCacheCleanup)))
	app.All("/api/mirrors/refresh", adaptor.HTTPHandler(s.authMiddleware.WrapFunc(s.mirrorsHandler.HandleMirrorsRefresh)))

	// Ping (/_/ping and /_/ping/ and /_/ping/...)
	pingHandler := func(c *fiber.Ctx) error {
		c.Set("Content-Type", "text/plain; charset=utf-8")
		return c.SendString("pong")
	}
	app.All("/_/ping", pingHandler)
	app.All("/_/ping/*", pingHandler)

	// Root: exact "/" -> home page, everything else -> proxy+cache (request log via logger-kit FiberMiddleware)
	rootHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			s.proxy.Handler.ServeHTTP(w, r)
			return
		}
		proxy.HandleHomePage(w, r, s.config.CacheDir)
	})
	app.All("/*", adaptor.HTTPHandler(rootHandler))

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

	// Wait for shutdown signal, reload signal, or server error
	for {
		select {
		case err := <-serverErr:
			return wrapError("server error", err)
		case <-sighupChan:
			s.reload()
		case <-ctx.Done():
			return s.shutdown()
		}
	}
}

// reload handles configuration hot reload triggered by SIGHUP signal.
// It refreshes mirror configurations without restarting the proxy.
func (s *Server) reload() {
	s.log.Info().Msg("received SIGHUP, reloading configuration...")

	// Refresh mirror configurations
	proxy.RefreshMirrors()

	s.log.Info().Msg("configuration reload complete")
}

// shutdown performs a graceful server shutdown with a 5-second timeout.
// It allows in-flight requests to complete before closing the proxy.
// Returns an error if shutdown fails or times out.
func (s *Server) shutdown() error {
	s.log.Info().Msg("shutting down proxy...")

	if err := s.app.ShutdownWithTimeout(5 * time.Second); err != nil {
		return wrapError("failed to shutdown server gracefully", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Close cache to stop cleanup goroutines
	if s.cache != nil {
		if err := s.cache.Close(); err != nil {
			s.log.Warn().Err(err).Msg("failed to close cache")
		}
	}

	// Shutdown tracing
	if err := tracing.Shutdown(ctx); err != nil {
		s.log.Warn().Err(err).Msg("failed to shutdown tracing")
	}

	s.log.Info().Msg("server shutdown complete")
	return nil
}

// Daemon is the main entry point for starting the application daemon.
// It validates the configuration, creates and starts the server, and handles
// any startup errors. This function blocks until the server shuts down.
func Daemon(flags *config.Config) {
	// Use default logger for initial validation errors
	log := logger.Default()

	if flags == nil {
		log.Fatal().Msg("configuration cannot be nil")
	}

	if err := config.ValidateConfig(flags); err != nil {
		log.Fatal().Err(err).Msg("invalid configuration")
	}

	// Use the provided configuration directly (no need to copy)
	srv, err := NewServer(flags)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create server")
	}

	if err := srv.Start(); err != nil {
		log.Fatal().Err(err).Msg("server error")
	}
}

// Error handling helpers using the unified error system

var errNilConfig = apperrors.New(apperrors.ErrConfigInvalid, "configuration cannot be nil")

func wrapError(msg string, err error) error {
	return apperrors.Wrap(apperrors.ErrInternal, msg, err)
}

func wrapServerError(msg string, err error) error {
	return apperrors.Wrap(apperrors.ErrServerInit, msg, err)
}

func wrapCacheError(msg string, err error) error {
	return apperrors.Wrap(apperrors.ErrCacheInit, msg, err)
}
