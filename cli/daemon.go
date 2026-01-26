package cli

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	health "github.com/soulteary/health-kit"
	logger "github.com/soulteary/logger-kit"
	metrics "github.com/soulteary/metrics-kit"
	middleware "github.com/soulteary/middleware-kit"
	version "github.com/soulteary/version-kit"

	"github.com/soulteary/apt-proxy/internal/server"
	"github.com/soulteary/apt-proxy/pkg/httpcache"
	"github.com/soulteary/apt-proxy/pkg/httplog"
)

// Config holds all application configuration
type Config struct {
	Debug    bool
	CacheDir string
	Mode     int
	Listen   string
	Mirrors  MirrorConfig
	Cache    CacheConfig
	TLS      TLSConfig
}

// TLSConfig holds TLS/HTTPS configuration
type TLSConfig struct {
	// Enabled indicates whether TLS is enabled
	Enabled bool
	// CertFile is the path to the TLS certificate file
	CertFile string
	// KeyFile is the path to the TLS private key file
	KeyFile string
}

// MirrorConfig holds mirror-specific configuration
type MirrorConfig struct {
	Ubuntu      string
	UbuntuPorts string
	Debian      string
	CentOS      string
	Alpine      string
}

// CacheConfig holds cache-specific configuration
type CacheConfig struct {
	// MaxSize is the maximum cache size in bytes (default: 10GB)
	MaxSize int64
	// TTL is the time-to-live for cached items (default: 7 days)
	TTL time.Duration
	// CleanupInterval is the interval between cleanup runs (default: 1 hour)
	CleanupInterval time.Duration
}

// Environment variable names for logging configuration
const (
	EnvLogLevel  = "LOG_LEVEL"
	EnvLogFormat = "LOG_FORMAT"
)

// Server represents the main application server that handles HTTP requests,
// manages caching, and coordinates all server components.
type Server struct {
	config           *Config                 // Application configuration
	cache            httpcache.ExtendedCache // HTTP cache implementation with management capabilities
	proxy            *server.PackageStruct   // Main proxy router
	responseLogger   *httplog.ResponseLogger // Request/response logger
	router           http.Handler            // HTTP router with orthodox routing
	server           *http.Server            // HTTP server instance
	log              *logger.Logger          // Structured logger
	healthAggregator *health.Aggregator      // Health check aggregator
	metricsRegistry  *metrics.Registry       // Prometheus metrics registry
	versionInfo      *version.Info           // Version information
}

// NewServer creates and initializes a new Server instance with the provided
// configuration. It sets up caching, proxy routing, logging, and HTTP server.
// Returns an error if initialization fails.
func NewServer(cfg *Config) (*Server, error) {
	if cfg == nil {
		return nil, fmt.Errorf("configuration cannot be nil")
	}

	s := &Server{
		config: cfg,
	}

	// Initialize structured logger first
	s.initLogger()

	if err := s.initialize(); err != nil {
		return nil, fmt.Errorf("failed to initialize server: %w", err)
	}

	return s, nil
}

// initLogger initializes the structured logger with configuration from environment
func (s *Server) initLogger() {
	// Determine log level from environment or debug flag
	level := logger.ParseLevelFromEnv(EnvLogLevel, logger.InfoLevel)
	if s.config.Debug {
		level = logger.DebugLevel
	}

	// Determine log format from environment
	formatStr := os.Getenv(EnvLogFormat)
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
		return fmt.Errorf("failed to initialize cache: %w", err)
	}
	s.cache = cache

	// Initialize metrics registry
	s.metricsRegistry = metrics.NewRegistry("apt_proxy")

	// Initialize cache metrics
	httpcache.NewCacheMetrics(s.metricsRegistry)

	// Initialize health check aggregator
	s.initHealthChecks()

	// Initialize proxy
	s.proxy = server.CreatePackageStructRouter(s.config.CacheDir, s.log)

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
	config := health.DefaultConfig().
		WithServiceName("apt-proxy").
		WithTimeout(2 * time.Second)

	s.healthAggregator = health.NewAggregator(config)

	// Add cache directory check
	s.healthAggregator.AddChecker(health.NewCustomChecker("cache", func(ctx context.Context) error {
		_, err := os.Stat(s.config.CacheDir)
		return err
	}).WithTimeout(1 * time.Second))
}

// buildCacheConfig creates a cache configuration from the application config
func (s *Server) buildCacheConfig() *httpcache.CacheConfig {
	config := httpcache.DefaultCacheConfig()

	// Apply custom settings if provided
	if s.config.Cache.MaxSize > 0 {
		config.WithMaxSize(s.config.Cache.MaxSize)
	}
	if s.config.Cache.TTL > 0 {
		config.WithTTL(s.config.Cache.TTL)
	}
	if s.config.Cache.CleanupInterval > 0 {
		config.WithCleanupInterval(s.config.Cache.CleanupInterval)
	}

	return config
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

	// Register cache management API endpoints
	mux.HandleFunc("/api/cache/stats", s.handleCacheStats)
	mux.HandleFunc("/api/cache/purge", s.handleCachePurge)
	mux.HandleFunc("/api/cache/cleanup", s.handleCacheCleanup)

	// Register mirror management API endpoints
	mux.HandleFunc("/api/mirrors/refresh", s.handleMirrorsRefresh)

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
		server.HandleHomePage(rw, r, s.config.CacheDir)
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
			return fmt.Errorf("server error: %w", err)
		case <-sighupChan:
			s.reload()
		case <-ctx.Done():
			return s.shutdown()
		}
	}
}

// reload handles configuration hot reload triggered by SIGHUP signal.
// It refreshes mirror configurations without restarting the server.
func (s *Server) reload() {
	s.log.Info().Msg("received SIGHUP, reloading configuration...")

	// Refresh mirror configurations
	server.RefreshMirrors()

	s.log.Info().Msg("configuration reload complete")
}

// shutdown performs a graceful server shutdown with a 5-second timeout.
// It allows in-flight requests to complete before closing the server.
// Returns an error if shutdown fails or times out.
func (s *Server) shutdown() error {
	s.log.Info().Msg("shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.server.Shutdown(ctx); err != nil {
		return fmt.Errorf("failed to shutdown server gracefully: %w", err)
	}

	// Close cache to stop cleanup goroutines
	if s.cache != nil {
		if err := s.cache.Close(); err != nil {
			s.log.Warn().Err(err).Msg("failed to close cache")
		}
	}

	s.log.Info().Msg("server shutdown complete")
	return nil
}

// Daemon is the main entry point for starting the application daemon.
// It validates the configuration, creates and starts the server, and handles
// any startup errors. This function blocks until the server shuts down.
func Daemon(flags *Config) {
	// Use default logger for initial validation errors
	log := logger.Default()

	if flags == nil {
		log.Fatal().Msg("configuration cannot be nil")
	}

	if err := ValidateConfig(flags); err != nil {
		log.Fatal().Err(err).Msg("invalid configuration")
	}

	cfg := &Config{
		Debug:    flags.Debug,
		CacheDir: flags.CacheDir,
		Mode:     flags.Mode,
		Listen:   flags.Listen,
		Mirrors: MirrorConfig{
			Ubuntu:      flags.Mirrors.Ubuntu,
			UbuntuPorts: flags.Mirrors.UbuntuPorts,
			Debian:      flags.Mirrors.Debian,
			CentOS:      flags.Mirrors.CentOS,
			Alpine:      flags.Mirrors.Alpine,
		},
		Cache: CacheConfig{
			MaxSize:         flags.Cache.MaxSize,
			TTL:             flags.Cache.TTL,
			CleanupInterval: flags.Cache.CleanupInterval,
		},
		TLS: TLSConfig{
			Enabled:  flags.TLS.Enabled,
			CertFile: flags.TLS.CertFile,
			KeyFile:  flags.TLS.KeyFile,
		},
	}

	srv, err := NewServer(cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to create server")
	}

	if err := srv.Start(); err != nil {
		log.Fatal().Err(err).Msg("server error")
	}
}

// Cache management API handlers

// handleCacheStats returns cache statistics as JSON
func (s *Server) handleCacheStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	stats := s.cache.Stats()

	// Update Prometheus metrics
	if httpcache.DefaultMetrics != nil {
		httpcache.DefaultMetrics.UpdateCacheStats(stats)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{
  "total_size_bytes": %d,
  "total_size_human": "%s",
  "item_count": %d,
  "stale_count": %d,
  "hit_count": %d,
  "miss_count": %d,
  "hit_rate": %.4f
}`,
		stats.TotalSize,
		formatBytes(stats.TotalSize),
		stats.ItemCount,
		stats.StaleCount,
		stats.HitCount,
		stats.MissCount,
		calculateHitRate(stats.HitCount, stats.MissCount),
	)
}

// handleCachePurge clears all cached items
func (s *Server) handleCachePurge(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get stats before purge
	statsBefore := s.cache.Stats()

	if err := s.cache.Purge(); err != nil {
		s.log.Error().Err(err).Msg("failed to purge cache")
		http.Error(w, "Failed to purge cache", http.StatusInternalServerError)
		return
	}

	s.log.Info().
		Int("items_removed", statsBefore.ItemCount).
		Int64("bytes_freed", statsBefore.TotalSize).
		Msg("cache purged")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{
  "success": true,
  "items_removed": %d,
  "bytes_freed": %d
}`,
		statsBefore.ItemCount,
		statsBefore.TotalSize,
	)
}

// handleCacheCleanup triggers a manual cleanup cycle
func (s *Server) handleCacheCleanup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	result := s.cache.Cleanup()

	s.log.Info().
		Int("items_removed", result.RemovedItems).
		Int64("bytes_freed", result.RemovedBytes).
		Int("stale_entries_removed", result.RemovedStaleEntries).
		Dur("duration", result.Duration).
		Msg("manual cache cleanup completed")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{
  "success": true,
  "items_removed": %d,
  "bytes_freed": %d,
  "stale_entries_removed": %d,
  "duration_ms": %d
}`,
		result.RemovedItems,
		result.RemovedBytes,
		result.RemovedStaleEntries,
		result.Duration.Milliseconds(),
	)
}

// handleMirrorsRefresh triggers a mirror benchmark refresh
func (s *Server) handleMirrorsRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	start := time.Now()

	// Refresh mirrors using the server reload mechanism
	server.RefreshMirrors()

	duration := time.Since(start)

	s.log.Info().
		Dur("duration", duration).
		Msg("mirrors refresh completed")

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{
  "success": true,
  "message": "Mirror configurations refreshed",
  "duration_ms": %d
}`,
		duration.Milliseconds(),
	)
}

// calculateHitRate calculates the cache hit rate
func calculateHitRate(hits, misses int64) float64 {
	total := hits + misses
	if total == 0 {
		return 0
	}
	return float64(hits) / float64(total)
}

// formatBytes formats bytes into a human-readable string
func formatBytes(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
		TB = GB * 1024
	)

	switch {
	case bytes >= TB:
		return fmt.Sprintf("%.2f TB", float64(bytes)/TB)
	case bytes >= GB:
		return fmt.Sprintf("%.2f GB", float64(bytes)/GB)
	case bytes >= MB:
		return fmt.Sprintf("%.2f MB", float64(bytes)/MB)
	case bytes >= KB:
		return fmt.Sprintf("%.2f KB", float64(bytes)/KB)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
