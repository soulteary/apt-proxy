# Operations and management

[Project README](../README.md) · [中文](operations.zh-CN.md)

## Status UI

The `/` endpoint serves the status page. Example UI:

![APT Proxy status page](../.github/assets/apt-proxy-webui-preview.jpg)

## API Endpoints

APT Proxy provides REST API endpoints for monitoring and management:

### Health & Monitoring

| Endpoint | Description |
|----------|-------------|
| `GET /healthz` | Aggregated health check (cache, dependencies) |
| `GET /livez` | Kubernetes liveness probe (lightweight, no dependencies) |
| `GET /readyz` | Kubernetes readiness probe (currently shares the same aggregator as `/healthz`) |
| `GET /version` | Version information (also available via `X-Version` response header on every response) |
| `GET /metrics` | Prometheus metrics |
| `ALL /_/ping`, `ALL /_/ping/*` | Cheap reachability probe; always returns `pong` |
| `GET /` | Internal status page (HTML) showing routes, mirrors, and cache stats |

### Cache Management (Protected)

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/cache/stats` | GET | Cache statistics (size, hit rate, item count) |
| `/api/cache/purge` | POST | Purge all cached items |
| `/api/cache/cleanup` | POST | Remove stale cache entries |

### Mirror Management (Protected)

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/mirrors/refresh` | POST | Reload distributions/mirrors config (distributions.yaml) and refresh mirrors |
| `/api/mirrors/status` | GET | Inspect process-local runtime mirror health and selection decisions |

### API Authentication

When an API key is configured and authentication is enabled, all `/api/*` endpoints require authentication. Setting `--api-key` (or `APT_PROXY_API_KEY`) implicitly enables auth unless `--enable-api-auth=false` is explicitly supplied. Enabling authentication without a non-empty key is rejected as an invalid configuration.

The read-only `/api/cache/stats` and `/api/mirrors/status` endpoints remain available when authentication is disabled. Mutating endpoints (`purge`, `cleanup`, and mirror `refresh`) fail closed with HTTP 503 until API authentication is configured; an unauthenticated network listener can therefore not modify proxy state. Provide the API key using one of these methods:

1. **X-API-Key Header** (recommended):
   ```bash
   curl -H "X-API-Key: your-api-key" http://localhost:3142/api/cache/stats
   ```

2. **Authorization Bearer Token**:
   ```bash
   curl -H "Authorization: Bearer your-api-key" http://localhost:3142/api/cache/stats
   ```

### API Rate Limiting

All `/api/*` endpoints are subject to per-IP rate limiting. The default budget is **60 requests per IP per minute** (sliding 1-minute window); set `--api-rate-limit=0` to disable. When the limit is exceeded the server responds with HTTP `429 Too Many Requests` and a JSON body whose error code is `ErrRateLimited`.

By default the client IP is taken from `RemoteAddr`. To honor `X-Forwarded-For` (e.g. behind nginx, ALB, or a cloud LB), pass the **trusted proxy CIDRs** via `--trusted-proxies=10.0.0.0/8,192.168.0.0/16` (or `APT_PROXY_TRUSTED_PROXIES`). Only requests originating from those CIDRs will have their `X-Forwarded-For` parsed; otherwise it is ignored to prevent spoofing.

### Response Headers

The server attaches the following headers to every response:

- `X-Version`, `X-Build-*` — version and build metadata (also available at `GET /version`).
- Standard security headers (e.g. `X-Content-Type-Options`, `X-Frame-Options`, `Referrer-Policy`, `Strict-Transport-Security` when TLS is on).
- `X-Cache: HIT` / `MISS` / `SKIP` on proxy responses (used by the request logger to classify traffic).
- Budget-controlled cacheable GET responses can carry the `X-Cache-Staging` HTTP trailer: `ADMITTED` means response staging was admitted; `SKIP` means it was skipped. Read the entire body to observe the trailer. `ADMITTED` does not promise background publication has committed. HEAD, HTTP/1.0 and other responses without trailer support skip budget-controlled publication.

**Example: Get Cache Statistics (with authentication)**

```bash
curl -H "X-API-Key: your-api-key" http://localhost:3142/api/cache/stats
```

Response:

```json
{
  "total_size_bytes": 1073741824,
  "total_size_human": "1.00 GB",
  "item_count": 150,
  "stale_count": 5,
  "hit_count": 1250,
  "miss_count": 150,
  "hit_rate": 0.893
}
```

## Hot Reload

APT Proxy supports hot reloading of **distributions and mirror config only** (including `distributions.yaml`) without restart. Changes to the **main configuration** (e.g. `apt-proxy.yaml`: server host/port, cache limits, TLS, security, API key) **do not** hot-reload and require a process restart.

To reload distributions and mirrors:

```bash
# Send SIGHUP to reload config and refresh mirrors
kill -HUP $(pgrep apt-proxy)
```

Or use the API with authentication enabled and the configured API key
(mutating API requests return 503 while authentication is disabled):

```bash
curl -X POST -H "X-API-Key: YOUR_API_KEY" \
  http://localhost:3142/api/mirrors/refresh
```

Both paths are equivalent: they reload `distributions.yaml` and re-run mirror selection. SIGHUP signals are debounced (consecutive signals within ~500ms are coalesced) and queued (at most one extra reload is scheduled while a reload is in progress), so it is safe to invoke them rapidly from scripts.

A reload does not interrupt serving. Mirror re-election is network-bound and can take a while, and for that whole window requests continue to be answered from the previous configuration; the new distributions and their mirrors become visible together, in one step, once the election finishes. A distribution added by the reload is simply not routable until then, rather than matching a rule whose mirror does not exist yet.

## Observability

### Metrics

The `/metrics` endpoint exposes Prometheus metrics. Key metrics and suggested alerts:

| Metric / area | Description | Suggested alert |
|---------------|-------------|-----------------|
| `apt_proxy_cache_hits_total` / `apt_proxy_cache_misses_total` | Cache hits and misses | Hit ratio drops sharply |
| `apt_proxy_cache_size_bytes` / `apt_proxy_cache_items` | Current cache footprint | Cache size near `--cache-max-size` limit |
| `apt_proxy_cache_evictions_total` | LRU evictions due to size limit | Sustained eviction rate (cache too small) |
| `apt_proxy_cache_cleanup_duration_seconds` | Periodic cleanup duration | Cleanup taking too long |
| `apt_proxy_cache_upstream_request_duration_seconds{method,status}` | Upstream request latency by method/status | P99 above threshold |
| `apt_proxy_cache_upstream_errors_total` | Upstream fetch errors | Error rate spike |
| Health (`/healthz`, `/readyz`) | Service and dependency health | Probes failing |

Exact labels and additional series are emitted by the underlying [httpcache-kit](https://github.com/soulteary/httpcache-kit); scrape `/metrics` to enumerate them.

### Logging

Logging is structured (JSON or console) and configured purely via environment variables:

- `APT_PROXY_LOG_LEVEL` — `debug` / `info` / `warn` / `error` (default `info`). `LOG_LEVEL` is honored as a legacy fallback.
- `APT_PROXY_LOG_FORMAT` — `json` / `console` / `auto` (default `auto`, picks `console` when stdout is a TTY). `LOG_FORMAT` is honored as a legacy fallback.
- `--debug` / `APT_PROXY_DEBUG=true` forces `debug` level **and** dumps request headers and bodies into access logs — use only for troubleshooting.

Each request log carries `request_id`, `cache` (`HIT`/`MISS`/`SKIP`/empty), and the response `size`. The probe paths `/healthz`, `/livez`, and `/readyz` are excluded from access logs to keep them quiet.

### Distributed Tracing (OpenTelemetry)

Set `OTEL_EXPORTER_OTLP_ENDPOINT` to your OTLP collector (e.g. `http://otel-collector:4317`) to enable OpenTelemetry tracing. The exporter is wired up automatically; spans are flushed during graceful shutdown. Tracing is disabled when the variable is unset.
