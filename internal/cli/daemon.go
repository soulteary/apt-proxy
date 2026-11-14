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
	stderrors "errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	health "github.com/soulteary/health-kit/v4"
	logger "github.com/soulteary/logger-kit/v3"
	metrics "github.com/soulteary/metrics-kit/v3"
	tracing "github.com/soulteary/tracing-kit/v2"
	otlp "github.com/soulteary/tracing-kit/v2/otlp"
	version "github.com/soulteary/version-kit/v4"

	"github.com/soulteary/apt-proxy/internal/access"
	"github.com/soulteary/apt-proxy/internal/api"
	"github.com/soulteary/apt-proxy/internal/config"
	"github.com/soulteary/apt-proxy/internal/distro"
	apperrors "github.com/soulteary/apt-proxy/internal/errors"
	"github.com/soulteary/apt-proxy/internal/logsafe"
	"github.com/soulteary/apt-proxy/internal/metalink"
	"github.com/soulteary/apt-proxy/internal/passthrough"
	"github.com/soulteary/apt-proxy/internal/proxy"
	"github.com/soulteary/apt-proxy/internal/resource"
	"github.com/soulteary/apt-proxy/internal/sharedcache"
	"github.com/soulteary/apt-proxy/internal/state"
	"github.com/soulteary/apt-proxy/internal/storage"
	"github.com/soulteary/apt-proxy/internal/storage/s3vfs"
	httpcache "github.com/soulteary/httpcache-kit/v4"
	prometheusmetrics "github.com/soulteary/httpcache-kit/v4/prometheusmetrics"
	vfs "github.com/soulteary/vfs-kit"
)

// Server represents the main application server that handles HTTP requests,
// manages caching, and coordinates all server components.
type Server struct {
	// mirrorRefreshMu serializes registry/state updates with routing publication.
	mirrorRefreshMu     sync.Mutex
	resources           *resource.Manager
	downloadClients     *access.Clients
	config              *config.Config           // Application configuration
	cache               httpcache.ExtendedCache  // HTTP cache implementation with management capabilities
	cacheProxy          *httpcache.Handler       // Shared HTTP cache wrapper; drained before backend close
	s3fs                *s3vfs.S3VFS             // Active S3 backend (only set when storage backend == "s3")
	state               *state.AppState          // Per-server runtime state (proxy mode, mirror URLs)
	registry            *distro.Registry         // Per-server distribution registry
	proxy               *proxy.PackageStruct     // Main proxy router (Handler is cache-wrapped)
	handler             http.Handler             // Every route: operational endpoints, management API, repository proxy
	httpServer          *http.Server             // Serves handler; listeners are attached in serve
	stopRequests        context.CancelFunc       // Cancels the context every request inherits
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

	downloadClients, err := access.NewClients(cfg.Security.DownloadAllowCIDRs, cfg.Security.DownloadDenyCIDRs)
	if err != nil {
		return nil, err
	}
	if _, err := access.NewUpstream(cfg.UpstreamAccess); err != nil {
		return nil, err
	}
	if err := config.ValidateResourceBackend(cfg.Resources, cfg.Storage.Backend); err != nil {
		return nil, err
	}
	if err := config.ValidateCacheIntegrity(cfg.Cache.Integrity); err != nil {
		return nil, err
	}
	if _, err := metalink.New(cfg.Metalink, cfg.UpstreamAccess); err != nil {
		return nil, err
	}
	s := &Server{
		downloadClients: downloadClients,
		config:          cfg,
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

	tp, err := otlp.InitTracer("apt-proxy", serviceVersion, otlpEndpoint)
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
	if s.config.Security.EnableAPIAuth && strings.TrimSpace(s.config.Security.APIKey) == "" {
		return wrapErr(apperrors.ErrConfigInvalid, "API authentication is enabled but no API key is configured", nil)
	}

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

	// Initialize cache with configuration. Storage backend is selected by
	// config.Storage.Backend; "disk" (default) keeps the historical
	// behaviour, "s3" plugs an S3-compatible bucket in via the s3vfs VFS.
	resources, err := s.buildResourceManager()
	if err != nil {
		return err
	}
	s.resources = resources
	cacheConfig := s.buildCacheConfig()
	cacheConfig.StagingBudget = s.resources
	identity, err := sharedcache.New(s.config.SharedCache)
	if err != nil {
		return err
	}
	var coordinator *sharedcache.FileCoordinator
	if s.config.SharedCache != nil && s.config.SharedCache.CoordinationDir != "" {
		if s.config.Storage.Backend != "" && s.config.Storage.Backend != config.StorageBackendDisk {
			return fmt.Errorf("shared disk coordination requires disk storage")
		}
		coordinator, err = sharedcache.NewFileCoordinator(s.config.SharedCache.CoordinationDir)
		if err != nil {
			return err
		}
		cacheConfig.MaxSize = 0
		cacheConfig.TTL = 0
		cacheConfig.CleanupInterval = 0
		cacheConfig.DisableStagingSweep = true
	}
	if identity != nil {
		cacheConfig.Integrity = httpcache.IntegritySHA256
	}
	cache, err := s.initCache(cacheConfig)
	if err != nil {
		return wrapErr(apperrors.ErrCacheInit, "failed to initialize cache", err)
	}
	s.cache = &storage.VerifiedCache{ExtendedCache: cache}
	if coordinator != nil {
		s.cache = &sharedcache.LockedCache{ExtendedCache: s.cache, Coordinator: coordinator}
	}

	// Initialize metrics registry
	s.metricsRegistry = metrics.NewRegistry("apt_proxy")

	// Initialize cache metrics
	prometheusmetrics.New(s.metricsRegistry)

	// Initialize health check aggregator
	s.initHealthChecks()

	// Build the per-Server distribution registry. RegisterBuiltins seeds the
	// compile-time defaults; Reload overlays user-supplied YAML.
	//
	// The path is passed through even when empty: that is how Loader.Load is
	// told to walk its search list (./config/distributions.yaml, ./distributions
	// .yaml, /etc/apt-proxy, ~/.config/apt-proxy), which is the behaviour the
	// README documents. Guarding this call on a non-empty path made that search
	// unreachable from the server, so a distributions.yaml sitting at one of
	// those paths was silently ignored unless the operator also named it.
	//
	// Reload parses before it mutates, so a missing file is a no-op and an
	// unparseable one leaves these built-ins in place.
	s.registry = distro.NewBuiltinRegistry()
	if err := s.registry.Reload(s.config.DistributionsConfigPath); err != nil {
		s.log.Warn().
			Err(err).
			Str("path", s.config.DistributionsConfigPath).
			Msg("failed to load distributions config; using built-in defaults")
	}
	s.logRegisteredDistributions()

	// Build the per-Server AppState and apply config (proxy mode, mirrors).
	// The mode is validated here, now that the registry holds the IDs from
	// distributions.yaml: an ID it does not have stops the startup instead of
	// serving a different set of distributions than the one configured.
	s.state = state.NewAppState()
	if err := config.ApplyToState(s.config, s.state, s.registry); err != nil {
		return wrapErr(apperrors.ErrServerInit, "failed to apply config to state", err)
	}
	s.logEnabledDistributions()

	// Initialize proxy with async benchmark for faster startup.
	// This uses default mirrors immediately and updates to the fastest mirror
	// in the background after benchmarking completes. A distribution with a
	// pinned mirror, and every distribution when mirror election is off, has
	// its final mirror from the start and runs no benchmark.
	// ValidateConfig already rejected a malformed allowlist, so a failure here
	// would mean the config changed underneath us; treat it as an init error
	// rather than starting with a list that is not what was written.
	allowlist, err := passthrough.Parse(s.config.Passthrough)
	if err != nil {
		return wrapErr(apperrors.ErrServerInit, "failed to parse passthrough allowlist", err)
	}
	s.logPassthrough(allowlist)

	ps, err := proxy.NewPackageStruct(proxy.Options{
		Resources: s.resources,
		TransportWrapper: func(next http.RoundTripper) http.RoundTripper {
			return identity.WrapTransportWithBudget(next, s.downloadTempDir(), s.resources)
		},
		MirrorHealth:       s.config.MirrorHealth,
		Metalink:           s.config.Metalink,
		UpstreamAccess:     s.config.UpstreamAccess,
		State:              s.state,
		Registry:           s.registry,
		CacheDir:           s.config.CacheDir,
		DownloadTempDir:    s.downloadTempDir(),
		DownloadMaxBytes:   s.config.Download.MaxSpoolMB * (1 << 20),
		DownloadTotalBytes: s.config.Download.TotalSpoolMB * (1 << 20),
		DownloadTimeout:    time.Duration(s.config.Download.TimeoutSeconds) * time.Second,
		Logger:             s.log,
		ModeSet:            s.state.GetProxyMode(),
		EnableKeepAlive:    s.config.UpstreamKeepAlive,
		Async:              true,
		Passthrough:        allowlist,
		FollowRedirects:    s.config.PassthroughFollowRedirects,
	})
	if err != nil {
		return wrapErr(apperrors.ErrServerInit, "failed to initialize proxy", err)
	}
	s.proxy = ps

	// Wrap proxy with cache (request logging is done by the access-log middleware)
	cacheOptions := &httpcache.HandlerOptions{Logger: s.log, StagingBudget: s.resources, TempDir: s.downloadTempDir()}
	if identity != nil {
		cacheOptions.RequestKey = identity.RequestKey
	}
	if coordinator != nil {
		cacheOptions.MissCoordinator = coordinator
	}
	cachedHandler := httpcache.NewHandlerWithOptions(s.cache, s.proxy.Handler, cacheOptions)
	// apt-proxy serves multiple clients and is therefore a shared cache. This
	// activates RFC shared-cache protections for Authorization/private data.
	cachedHandler.Shared = true
	s.cacheProxy = cachedHandler
	s.proxy.Handler = identity.WrapHandler(cachedHandler, s.proxy.Handler)

	if s.config.Debug {
		s.log.Debug().Msg("debug mode enabled")
		httpcache.SetDebugLogging(true)
	}

	// Initialize API handlers (mirrors refresh also reloads distributions config when path set)
	s.cacheHandler = api.NewCacheHandler(s.cache, s.log)
	s.mirrorsHandler = api.NewMirrorsHandler(s.log, s.refreshMirrors)

	// Both middlewares need to agree on what counts as the "real" client
	// IP. Construct the extractor once and share it; otherwise auth logs
	// could attribute a request to r.RemoteAddr (the proxy) while
	// rate-limit logs attributed it to the XFF left-most IP, making it
	// impossible to correlate forensic events.
	clientIP := api.NewClientIPExtractor(s.config.Security.TrustedProxies)

	apiKey := ""
	if s.config.Security.EnableAPIAuth {
		apiKey = s.config.Security.APIKey
	}
	s.authMiddleware = api.NewAuthMiddleware(api.AuthConfig{
		APIKey:   apiKey,
		Logger:   s.log,
		ClientIP: clientIP,
	})

	s.rateLimitMiddleware = api.NewRateLimitMiddleware(
		s.config.Security.APIRateLimitPerMinute,
		s.log,
		s.config.Security.TrustedProxies...,
	)

	s.handler = s.newHandler()
	s.httpServer = s.newHTTPServer()

	return nil
}

// initHealthChecks initializes the health check aggregator
func (s *Server) initHealthChecks() {
	cfg := health.DefaultConfig().
		WithServiceName("apt-proxy").
		WithTimeout(2 * time.Second)

	s.healthAggregator = health.NewAggregator(cfg)

	// Register a storage-specific health check. For the local-disk backend
	// we keep the cheap os.Stat probe; for S3 we delegate to a HeadBucket
	// round-trip so we surface bucket-not-found / IAM regressions promptly.
	if s.s3fs != nil {
		fs := s.s3fs
		s.healthAggregator.AddChecker(health.NewCustomChecker("storage", func(ctx context.Context) error {
			return fs.HealthCheck(ctx)
		}).WithTimeout(2 * time.Second))
		return
	}

	// Fallback: local-disk cache directory check.
	s.healthAggregator.AddChecker(health.NewCustomChecker("cache", func(ctx context.Context) error {
		_, err := os.Stat(s.config.CacheDir)
		return err
	}).WithTimeout(1 * time.Second))
}

// initCache constructs a cache backend selected by config.Storage.Backend.
// Empty backend and "disk" preserve the historical local-disk implementation;
// "s3" wires httpcache-kit through the s3vfs VFS.
//
// On the s3 path we also stash the *S3VFS in the Server so the health check
// (and any future maintenance hooks) can reach it without re-creating a
// client.
func (s *Server) initCache(cacheConfig *httpcache.CacheConfig) (httpcache.ExtendedCache, error) {
	switch s.config.Storage.Backend {
	case "", config.StorageBackendDisk:
		return httpcache.NewDiskCacheWithConfig(s.config.CacheDir, cacheConfig)
	case config.StorageBackendS3:
		s3cfg := s.config.Storage.S3
		var inlineMaxBytes int64
		if s3cfg.InlineMaxMB > 0 {
			inlineMaxBytes = s3cfg.InlineMaxMB * 1024 * 1024
		}
		fs, err := s3vfs.New(context.Background(), s3vfs.Config{
			Endpoint:       s3cfg.Endpoint,
			Region:         s3cfg.Region,
			Bucket:         s3cfg.Bucket,
			Prefix:         s3cfg.Prefix,
			AccessKey:      s3cfg.AccessKey,
			SecretKey:      s3cfg.SecretKey,
			SessionToken:   s3cfg.SessionToken,
			UseSSL:         s3cfg.UseSSL,
			UsePathStyle:   s3cfg.UsePathStyle,
			InlineMaxBytes: inlineMaxBytes,
			TempDir:        s3cfg.TempDir,
			StagingBudget:  s.resources,
			UploadTimeout:  time.Duration(s.config.Download.TimeoutSeconds) * time.Second,
		})
		if err != nil {
			return nil, fmt.Errorf("s3 backend: %w", err)
		}
		s.s3fs = fs
		s.log.Info().
			Str("bucket", s3cfg.Bucket).
			Str("endpoint", s3cfg.Endpoint).
			Str("prefix", s3cfg.Prefix).
			Msg("storage backend: s3")
		return httpcache.NewVFSCacheWithConfig(fs, cacheConfig), nil
	default:
		return nil, fmt.Errorf("unknown storage backend %q", s.config.Storage.Backend)
	}
}

// buildCacheConfig creates a cache configuration from the application config
func (s *Server) buildCacheConfig() *httpcache.CacheConfig {
	cacheConfig := httpcache.DefaultCacheConfig()
	applyCacheIntegrity(cacheConfig, s.config.Cache.Integrity)

	// The loader resolves omitted defaults and preserves explicit CLI/ENV zeros.
	// Passing those zeros through disables the corresponding backend limit.
	if s.config.Cache.MaxSize >= 0 {
		cacheConfig.WithMaxSize(s.config.Cache.MaxSize)
	}
	if s.config.Cache.TTL >= 0 {
		cacheConfig.WithTTL(s.config.Cache.TTL)
	}
	if s.config.Cache.CleanupInterval >= 0 {
		cacheConfig.WithCleanupInterval(s.config.Cache.CleanupInterval)
	}

	return cacheConfig
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

	return s.serve(ctx, sighupChan)
}

// serve binds the configured address and runs until ctx is cancelled. Start
// adapts process signals to these per-server inputs.
func (s *Server) serve(ctx context.Context, sighupChan <-chan os.Signal) error {
	listener, err := net.Listen("tcp", s.config.Listen)
	if err != nil {
		return wrapErr(apperrors.ErrInternal, "server error", err)
	}
	return s.serveListener(ctx, listener, sighupChan)
}

// serveListener runs the server and the reload loop on a bound listener using
// caller-owned cancellation and reload events.
func (s *Server) serveListener(ctx context.Context, listener net.Listener, sighupChan <-chan os.Signal) error {
	if s.config.TLS.Enabled {
		s.log.Info().
			Str("cert", s.config.TLS.CertFile).
			Str("key", s.config.TLS.KeyFile).
			Msg("starting HTTPS server with TLS")
	}
	serverErr := make(chan error, 1)
	go func() {
		if err := s.listen(listener); err != nil {
			serverErr <- err
		}
	}()

	s.log.Info().Str("address", listener.Addr().String()).Msg("server started successfully")
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

// logRegisteredDistributions reports the distributions in effect and the file
// they came from. Without it there is no way to tell a distributions.yaml that
// registered from one that was never found -- both just serve, and the custom
// distribution 404s with nothing said about why.
func (s *Server) logRegisteredDistributions() {
	if s.registry == nil {
		return
	}

	all := s.registry.GetAll()
	ids := make([]string, 0, len(all))
	for id := range all {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	path := s.registry.ConfigPath()
	if path == "" {
		path = "(built-in defaults)"
	}
	s.log.Info().
		Str("config", path).
		Strs("distributions", ids).
		Msg("distributions registered")
}

// logEnabledDistributions reports which of the registered distributions the
// mode serves. The registered list alone does not show that, and a mode given
// in another letter case is reported under the ID it was resolved to.
func (s *Server) logEnabledDistributions() {
	mode := s.state.GetProxyMode()
	enabled := []string{distro.DistroAll}
	if !mode.All() {
		enabled = enabled[:0]
		for id, d := range s.registry.GetAll() {
			if mode.Has(d.Type) {
				enabled = append(enabled, id)
			}
		}
		sort.Strings(enabled)
	}
	s.log.Info().Strs("mode", enabled).Msg("distributions enabled")
}

// logPassthrough reports the allowlisted origins at startup. Widening what a
// shared proxy will fetch is worth a line in the log, and it is the only way
// to tell a list that parsed from one that was never read.
func (s *Server) logPassthrough(list *passthrough.List) {
	if list.Empty() {
		return
	}
	origins := make([]string, 0, len(list.Rules()))
	for _, rule := range list.Rules() {
		origin := rule.Host
		if rule.Port != "" {
			origin = net.JoinHostPort(rule.Host, rule.Port)
		}
		if rule.ForceHTTPS {
			origin = "https://" + origin
		}
		origins = append(origins, origin)
	}
	s.log.Info().
		Strs("origins", origins).
		Msg("passthrough enabled for third-party origins")
	if !s.config.PassthroughFollowRedirects {
		return
	}
	// With redirects followed, the allowlist no longer bounds what apt-proxy
	// fetches: an allowlisted origin can send it to any public host. That is
	// the default, so it is stated once, with the setting that restores the
	// bound.
	s.log.Info().
		Str("setting", "passthrough_follow_redirects").
		Msg("redirects from passthrough origins are followed to public hosts outside the allowlist; " +
			"set passthrough_follow_redirects to false to keep fetches within the allowlist")
}

// refreshMirrors reloads distributions config (when configured) and
// refreshes mirror selection on this Server's proxy. Used as the reload
// closure for the mirrors API handler and for SIGHUP-triggered reloads.
func (s *Server) refreshMirrors() error {
	s.mirrorRefreshMu.Lock()
	defer s.mirrorRefreshMu.Unlock()
	// Empty path is meaningful here too -- see the note in initialize.
	if s.registry != nil {
		if err := s.registry.Reload(s.config.DistributionsConfigPath); err != nil {
			s.log.Warn().
				Err(logsafe.Error(err)).
				Str("path", s.config.DistributionsConfigPath).
				Msg("failed to reload distributions config")
			return apperrors.Wrap(apperrors.ErrConfigParseFail, "Failed to reload distributions configuration; previous routing retained", err)
		}
	}
	if s.state != nil {
		// The mode is resolved against the reloaded registry here. An enabled
		// ID that distributions.yaml no longer defines is an error naming it.
		// Nothing has been applied at that point and the proxy is not
		// refreshed, so the distribution stays served by the previous routing.
		if err := config.ApplyToState(s.config, s.state, s.registry); err != nil {
			s.log.Warn().Err(logsafe.Error(err)).Msg("failed to re-apply config to state during reload")
			return apperrors.Wrap(apperrors.ErrConfigInvalid, "Failed to apply mode and mirror configuration; previous routing retained", err).
				WithDetails("reason", logsafe.Error(err).Error())
		}
	}
	if s.proxy != nil {
		// ApplyToState stored the mode, resolved against the reloaded
		// registry, in the state the proxy also reads its mirrors from. The
		// proxy is handed that set instead of keeping its own, because the
		// reload can have given an enabled ID another type.
		if err := s.proxy.RefreshMirrorsWithMode(s.proxy.State().GetProxyMode()); err != nil {
			return apperrors.Wrap(apperrors.ErrMirrorBenchmark, "Mirror election failed; previous routing retained", err).WithHTTPStatus(http.StatusServiceUnavailable)
		}
	}
	return nil
}

// reload handles configuration hot reload triggered by SIGHUP signal.
// It reloads distributions config (if path set) and refreshes mirror configurations.
func (s *Server) reload() {
	s.log.Info().Msg("received SIGHUP, reloading configuration...")
	if err := s.refreshMirrors(); err != nil {
		s.log.Warn().Err(logsafe.Error(err)).Msg("configuration reload failed; previous routing retained")
		return
	}
	s.log.Info().Msg("configuration reload complete")
}

// shutdown performs a graceful server shutdown with a 5-second timeout.
// Requests still running when it begins are cancelled; the drain then waits
// for their handlers to return before the proxy is closed.
// All cleanup steps run unconditionally so a failed drain does not leak the
// cache file lock or skip the tracing flush.
func (s *Server) shutdown() error {
	s.log.Info().Msg("shutting down proxy...")

	var errs []error

	s.stopRequests()
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
		// Connections that did not drain in time are closed so their handlers
		// cannot outlive the cache they use.
		_ = s.httpServer.Close()
		s.log.Warn().Err(err).Msg("failed to shutdown server gracefully")
		errs = append(errs, wrapErr(apperrors.ErrInternal, "failed to shutdown server gracefully", err))
	}
	cancelShutdown()

	drainCtx, cancelDrain := context.WithTimeout(context.Background(), 5*time.Second)
	if s.cacheProxy != nil {
		if err := s.cacheProxy.Shutdown(drainCtx); err != nil {
			s.log.Warn().Err(err).Msg("failed to drain cache writes")
			errs = append(errs, wrapErr(apperrors.ErrCacheWrite, "failed to drain cache writes", err))
		}
	}
	cancelDrain()

	// Close cache only after background writes have drained, then stop cleanup
	// goroutines and release file locks.
	if s.cache != nil {
		if err := s.cache.Close(); err != nil {
			s.log.Warn().Err(err).Msg("failed to close cache")
			errs = append(errs, wrapErr(apperrors.ErrCacheInit, "failed to close cache", err))
		}
	}

	// Shutdown tracing (flush spans). Always attempt even on prior errors.
	traceCtx, cancelTrace := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelTrace()
	if err := tracing.Shutdown(traceCtx); err != nil {
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

// downloadTempDir follows the selected backend's writable staging location.
func (s *Server) downloadTempDir() string {
	if s.config.Storage.Backend == config.StorageBackendS3 {
		return s.config.Storage.S3.TempDir
	}
	return s.config.CacheDir
}
