package cli

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

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
	"github.com/soulteary/apt-proxy/pkg/httplog"
)

// Server represents the main application server that handles HTTP requests,
// manages caching, and coordinates all server components.
type Server struct {
	config           *config.Config          // Application configuration
	cache            httpcache.ExtendedCache // HTTP cache implementation with management capabilities
	proxy            *proxy.PackageStruct    // Main proxy router
	responseLogger   *httplog.ResponseLogger // Request/response logger
	router           http.Handler            // HTTP router with orthodox routing
	server           *http.Server            // HTTP server instance
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

	// Wrap proxy with cache
	cachedHandler := httpcache.NewHandler(s.cache, s.proxy.Handler)
	s.proxy.Handler = cachedHandler

	// Initialize response logger
	if s.config.Debug {
		s.log.Debug().Msg("debug mode enabled")
		httpcache.DebugLogging = true
	}
	// Set httpcache logger to use the same logger instance
	httpcache.SetLogger(s.log)
	s.responseLogger = httplog.NewResponseLogger(cachedHandler, s.log)
	s.responseLogger.DumpRequests = s.config.Debug
	s.responseLogger.DumpResponses = s.config.Debug
	s.responseLogger.DumpErrors = s.config.Debug

	// Initialize API handlers
	s.cacheHandler = api.NewCacheHandler(s.cache, s.log)
	s.mirrorsHandler = api.NewMirrorsHandler(s.log)

	// Initialize API authentication middleware
	s.authMiddleware = api.NewAuthMiddleware(api.AuthConfig{
		APIKey: s.config.Security.APIKey,
		Logger: s.log,
	})

	// Create router with orthodox routing (home, ping, health, version, metrics handlers)
	s.router = s.createRouter()

	// Initialize HTTP server
	s.server = &http.Server{
		Addr:              s.config.Listen,
		Handler:           s.router,
		ReadHeaderTimeout: 50 * time.Second,
		ReadTimeout:       50 * time.Second,
		WriteTimeout:      100 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

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

// createRouter creates the main HTTP router with all endpoints
func (s *Server) createRouter() http.Handler {
	mux := http.NewServeMux()

	// Register health check endpoints
	mux.HandleFunc("/healthz", health.Handler(s.healthAggregator))
	mux.HandleFunc("/livez", health.LivenessHandler("apt-proxy"))
	mux.HandleFunc("/readyz", health.ReadinessHandler(s.healthAggregator))

	// Register version endpoint
	mux.HandleFunc("/version", version.Handler(version.HandlerConfig{
		Info:   s.versionInfo,
		Pretty: true,
	}))

	// Register metrics endpoint
	mux.Handle("/metrics", metrics.HandlerFor(s.metricsRegistry))

	// Register cache management API endpoints (protected by API key if configured)
	mux.HandleFunc("/api/cache/stats", s.authMiddleware.WrapFunc(s.cacheHandler.HandleCacheStats))
	mux.HandleFunc("/api/cache/purge", s.authMiddleware.WrapFunc(s.cacheHandler.HandleCachePurge))
	mux.HandleFunc("/api/cache/cleanup", s.authMiddleware.WrapFunc(s.cacheHandler.HandleCacheCleanup))

	// Register mirror management API endpoints (protected by API key if configured)
	mux.HandleFunc("/api/mirrors/refresh", s.authMiddleware.WrapFunc(s.mirrorsHandler.HandleMirrorsRefresh))

	// Register ping endpoint handler
	mux.HandleFunc("/_/ping/", func(rw http.ResponseWriter, r *http.Request) {
		rw.WriteHeader(http.StatusOK)
		rw.Header().Set("Content-Type", "text/plain; charset=utf-8")
		rw.Write([]byte("pong"))
	})

	// Register home page handler for exact "/" path
	mux.HandleFunc("/", func(rw http.ResponseWriter, r *http.Request) {
		// Only handle exact "/" path, delegate everything else to package proxy
		if r.URL.Path != "/" {
			s.responseLogger.ServeHTTP(rw, r)
			return
		}
		proxy.HandleHomePage(rw, r, s.config.CacheDir)
	})

	// Apply security headers middleware
	handler := middleware.SecurityHeadersStd(middleware.DefaultSecurityHeadersConfig())(mux)

	// Apply version headers middleware
	handler = version.Middleware(s.versionInfo, "X-")(handler)

	return handler
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

	// Start server in goroutine
	serverErr := make(chan error, 1)
	go func() {
		var err error
		if s.config.TLS.Enabled {
			s.log.Info().
				Str("cert", s.config.TLS.CertFile).
				Str("key", s.config.TLS.KeyFile).
				Msg("starting HTTPS server with TLS")
			err = s.server.ListenAndServeTLS(s.config.TLS.CertFile, s.config.TLS.KeyFile)
		} else {
			err = s.server.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
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

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.server.Shutdown(ctx); err != nil {
		return wrapError("failed to shutdown server gracefully", err)
	}

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
