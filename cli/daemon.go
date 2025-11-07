package cli

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/soulteary/apt-proxy/internal/server"
	"github.com/soulteary/apt-proxy/pkg/httpcache"
	"github.com/soulteary/apt-proxy/pkg/httplog"
)

// Config holds all application configuration
type Config struct {
	Debug    bool
	Version  string
	CacheDir string
	Mode     int
	Listen   string
	Mirrors  MirrorConfig
}

// MirrorConfig holds mirror-specific configuration
type MirrorConfig struct {
	Ubuntu      string
	UbuntuPorts string
	Debian      string
	CentOS      string
	Alpine      string
}

// Server represents the main application server that handles HTTP requests,
// manages caching, and coordinates all server components.
type Server struct {
	config *Config                 // Application configuration
	cache  httpcache.Cache         // HTTP cache implementation
	proxy  *server.PackageStruct   // Main proxy router
	logger *httplog.ResponseLogger // Request/response logger
	server *http.Server            // HTTP server instance
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

	if err := s.initialize(); err != nil {
		return nil, fmt.Errorf("failed to initialize server: %w", err)
	}

	return s, nil
}

// initialize sets up all server components including cache, proxy router,
// logging, and HTTP server configuration. This method is called automatically
// by NewServer and should not be called directly.
func (s *Server) initialize() error {
	// Initialize cache
	cache, err := httpcache.NewDiskCache(s.config.CacheDir)
	if err != nil {
		return fmt.Errorf("failed to initialize cache: %w", err)
	}
	s.cache = cache

	// Initialize proxy
	s.proxy = server.CreatePackageStructRouter()
	s.proxy.Handler = httpcache.NewHandler(s.cache, s.proxy.Handler)

	// Initialize logger
	s.initLogger()

	// Initialize HTTP server
	s.server = &http.Server{
		Addr:              s.config.Listen,
		Handler:           s.proxy,
		ReadHeaderTimeout: 50 * time.Second,
		ReadTimeout:       50 * time.Second,
		WriteTimeout:      100 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	return nil
}

// initLogger configures the HTTP request/response logger based on debug settings.
// If debug mode is enabled, it enables verbose logging for requests, responses, and errors.
func (s *Server) initLogger() {
	if s.config.Debug {
		log.Printf("debug mode enabled")
		httpcache.DebugLogging = true
	}

	s.logger = httplog.NewResponseLogger(s.proxy.Handler)
	s.logger.DumpRequests = s.config.Debug
	s.logger.DumpResponses = s.config.Debug
	s.logger.DumpErrors = s.config.Debug
	s.proxy.Handler = s.logger
}

// Start begins serving HTTP requests and handles graceful shutdown on SIGINT or SIGTERM.
// The server runs in a goroutine while the main goroutine waits for shutdown signals.
// Returns an error if the server fails to start or encounters a fatal error.
func (s *Server) Start() error {
	log.Printf("starting apt-proxy %s", s.config.Version)
	log.Printf("listening on %s", s.config.Listen)

	// Setup graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Start server in goroutine
	serverErr := make(chan error, 1)
	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
		}
	}()

	log.Println("server started successfully 🚀")

	// Wait for shutdown signal or server error
	select {
	case err := <-serverErr:
		return fmt.Errorf("server error: %w", err)
	case <-ctx.Done():
		return s.shutdown()
	}
}

// shutdown performs a graceful server shutdown with a 5-second timeout.
// It allows in-flight requests to complete before closing the server.
// Returns an error if shutdown fails or times out.
func (s *Server) shutdown() error {
	log.Println("shutting down server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.server.Shutdown(ctx); err != nil {
		return fmt.Errorf("failed to shutdown server gracefully: %w", err)
	}

	log.Println("server shutdown complete")
	return nil
}

// Daemon is the main entry point for starting the application daemon.
// It validates the configuration, creates and starts the server, and handles
// any startup errors. This function blocks until the server shuts down.
func Daemon(flags *Config) {
	if flags == nil {
		log.Fatalf("configuration cannot be nil")
	}

	if err := ValidateConfig(flags); err != nil {
		log.Fatalf("invalid configuration: %v", err)
	}

	cfg := &Config{
		Debug:    flags.Debug,
		Version:  flags.Version,
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
	}

	server, err := NewServer(cfg)
	if err != nil {
		log.Fatalf("failed to create server: %v", err)
	}

	if err := server.Start(); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
