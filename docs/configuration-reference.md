# Configuration reference

[Project README](../README.md) · [中文](configuration-reference.zh-CN.md)

## Configuration Options

View all available options:

```bash
./apt-proxy -h
```

**Available options:** the tables below list flags and environment equivalents.
YAML also exposes policies without CLI flags; see the YAML section and the
[annotated template](../examples/config-template/apt-proxy.yaml).

| Option | Description | Default |
|--------|-------------|---------|
| `-host` | Network interface to bind to | `0.0.0.0` |
| `-port` | Port to listen on | `3142` |
| `-mode` | Distribution mode: `all`, `ubuntu`, `ubuntu-ports`, `debian`, `centos`, `alpine` | `all` |
| `-cachedir` | Directory to store cached packages | `./.aptcache` |
| `-ubuntu` | Ubuntu mirror URL or shortcut | (auto-select) |
| `-ubuntu-ports` | Ubuntu Ports mirror URL or shortcut | (auto-select) |
| `-debian` | Debian mirror URL or shortcut | (auto-select) |
| `-debian-security` | Dedicated Debian Security mirror URL or shortcut | (derived from `-debian`) |
| `-centos` | CentOS mirror URL or shortcut | (auto-select) |
| `-alpine` | Alpine mirror URL or shortcut | (auto-select) |
| `-distributions-config` | Path to distributions/mirrors YAML (distributions.yaml) | (optional) |
| `-passthrough` | Comma-separated third-party origins to fetch and cache unrewritten | (empty) |
| `-cache-max-size` | Maximum cache size in GB (0 to disable) | `10` |
| `-cache-ttl` | Cache TTL in hours (0 to disable) | `168` (7 days) |
| `-cache-cleanup-interval` | Cache cleanup interval in minutes (0 to disable) | `60` |
| `-tls` | Enable TLS/HTTPS (requires `-tls-cert` and `-tls-key`) | `false` |
| `-tls-cert` | Path to TLS certificate file | |
| `-tls-key` | Path to TLS private key file | |
| `-api-key` | API key for protected endpoints (auto-enables auth when set) | |
| `-enable-api-auth` | Explicitly enable/disable API authentication middleware | `false` (auto `true` when `-api-key` is set) |
| `-api-rate-limit` | API requests per IP per minute (`0` to disable) | `60` |
| `-trusted-proxies` | Comma-separated CIDRs whose `X-Forwarded-For` is honored by rate limiter and auth | |
| `-upstream-keep-alive` | Enable HTTP keep-alive to upstream mirrors | `true` |
| `-storage-backend` | Cache storage backend: `disk` or `s3` (see [S3 Storage Backend](cache-storage.md#s3-storage-backend)) | `disk` |
| `-s3-endpoint` | S3 endpoint host[:port] (required when backend is `s3`) | |
| `-s3-region` | S3 region (required for AWS S3, ignored by most MinIO services) | |
| `-s3-bucket` | S3 bucket name (must already exist) | |
| `-s3-prefix` | S3 object key prefix | `apt-proxy/` |
| `-s3-access-key` / `-s3-secret-key` | S3 IAM credentials | |
| `-s3-session-token` | Optional STS session token | |
| `-s3-use-ssl` | Use HTTPS to talk to the S3 endpoint | `true` |
| `-s3-use-path-style` | Force path-style URLs (needed for MinIO/Ceph) | `false` |
| `-s3-inline-max-mb` | In-memory write threshold in MiB before spilling to TempDir | `32` |
| `-s3-temp-dir` | Directory for spilled writes (default `os.TempDir()`) | |
| `-config` | Path to YAML configuration file | |
| `-debug` | Enable verbose debug logging (also dumps request headers/body to logs) | `false` |

**Example with Custom Configuration:**

```bash
./apt-proxy \
  --host=0.0.0.0 \
  --port=3142 \
  --cachedir=/var/cache/apt-proxy \
  --mode=ubuntu \
  --ubuntu=cn:tsinghua \
  --cache-max-size=20 \
  --debug
```

### Environment Variables

Every CLI flag has an equivalent environment variable. Plus a few extras for logging and tracing.

**Server / Mode**

| Variable | Equivalent flag | Description |
|----------|-----------------|-------------|
| `APT_PROXY_HOST` | `-host` | Network interface to bind to |
| `APT_PROXY_PORT` | `-port` | Port to listen on |
| `APT_PROXY_MODE` | `-mode` | Distribution mode (`all`/`ubuntu`/`ubuntu-ports`/`debian`/`centos`/`alpine`) |
| `APT_PROXY_DEBUG` | `-debug` | Enable verbose debug logging |
| `APT_PROXY_UBUNTU` | `-ubuntu` | Ubuntu mirror URL or shortcut |
| `APT_PROXY_UBUNTU_PORTS` | `-ubuntu-ports` | Ubuntu Ports mirror URL or shortcut |
| `APT_PROXY_DEBIAN` | `-debian` | Debian mirror URL or shortcut |
| `APT_PROXY_DEBIAN_SECURITY` | `-debian-security` | Dedicated Debian Security mirror URL or shortcut |
| `APT_PROXY_CENTOS` | `-centos` | CentOS mirror URL or shortcut |
| `APT_PROXY_ALPINE` | `-alpine` | Alpine mirror URL or shortcut |
| `APT_PROXY_UPSTREAM_KEEP_ALIVE` | `-upstream-keep-alive` | HTTP keep-alive to upstream mirrors |

**Cache**

| Variable | Equivalent flag | Description |
|----------|-----------------|-------------|
| `APT_PROXY_CACHEDIR` | `-cachedir` | Cache directory |
| `APT_PROXY_CACHE_MAX_SIZE` | `-cache-max-size` | Maximum cache size in GB (`0` disables) |
| `APT_PROXY_CACHE_TTL` | `-cache-ttl` | Cache TTL in hours (`0` disables) |
| `APT_PROXY_CACHE_CLEANUP_INTERVAL` | `-cache-cleanup-interval` | Cache cleanup interval in minutes (`0` disables) |

**TLS**

| Variable | Equivalent flag | Description |
|----------|-----------------|-------------|
| `APT_PROXY_TLS_ENABLED` | `-tls` | Enable TLS/HTTPS |
| `APT_PROXY_TLS_CERT` | `-tls-cert` | Path to TLS certificate |
| `APT_PROXY_TLS_KEY` | `-tls-key` | Path to TLS private key |

**Security (API)**

| Variable | Equivalent flag | Description |
|----------|-----------------|-------------|
| `APT_PROXY_API_KEY` | `-api-key` | API key for protected endpoints |
| `APT_PROXY_ENABLE_API_AUTH` | `-enable-api-auth` | Explicit toggle for API auth middleware |
| `APT_PROXY_API_RATE_LIMIT_PER_MINUTE` | `-api-rate-limit` | API requests per IP per minute (`0` disables) |
| `APT_PROXY_TRUSTED_PROXIES` | `-trusted-proxies` | Comma-separated trusted proxy CIDRs |

**Upstream Network** (standard variables, no equivalent flag)

| Variable | Description |
|----------|-------------|
| `HTTP_PROXY` / `http_proxy` | Forward proxy for `http://` requests to mirrors |
| `HTTPS_PROXY` / `https_proxy` | Forward proxy for `https://` requests to mirrors (`socks5h://` accepted) |
| `NO_PROXY` / `no_proxy` | Hosts, domain suffixes and CIDRs that bypass the forward proxy |

**Storage Backend**

| Variable | Equivalent flag | Description |
|----------|-----------------|-------------|
| `APT_PROXY_STORAGE_BACKEND` | `-storage-backend` | `disk` (default) or `s3` |
| `APT_PROXY_S3_ENDPOINT` | `-s3-endpoint` | S3 endpoint host[:port] |
| `APT_PROXY_S3_REGION` | `-s3-region` | S3 region |
| `APT_PROXY_S3_BUCKET` | `-s3-bucket` | S3 bucket name |
| `APT_PROXY_S3_PREFIX` | `-s3-prefix` | S3 object key prefix |
| `APT_PROXY_S3_ACCESS_KEY` | `-s3-access-key` | S3 access key ID |
| `APT_PROXY_S3_SECRET_KEY` | `-s3-secret-key` | S3 secret access key |
| `APT_PROXY_S3_SESSION_TOKEN` | `-s3-session-token` | Optional STS session token |
| `APT_PROXY_S3_USE_SSL` | `-s3-use-ssl` | Use HTTPS to talk to the S3 endpoint |
| `APT_PROXY_S3_USE_PATH_STYLE` | `-s3-use-path-style` | Force path-style URLs |
| `APT_PROXY_S3_INLINE_MAX_MB` | `-s3-inline-max-mb` | Memory write threshold in MiB before spilling |
| `APT_PROXY_S3_TEMP_DIR` | `-s3-temp-dir` | Directory for spilled writes |

**Configuration files**

| Variable | Equivalent flag | Description |
|----------|-----------------|-------------|
| `APT_PROXY_CONFIG_FILE` | `-config` | Path to `apt-proxy.yaml` |
| `APT_PROXY_DISTRIBUTIONS_CONFIG` | `-distributions-config` | Path to `distributions.yaml` |
| `APT_PROXY_PASSTHROUGH` | `-passthrough` | Allowlisted third-party origins |

**Logging & Tracing** (no CLI equivalent)

| Variable | Description |
|----------|-------------|
| `APT_PROXY_LOG_LEVEL` | Log level: `debug` / `info` / `warn` / `error`. `--debug` forces `debug`. |
| `APT_PROXY_LOG_FORMAT` | Log format: `json` / `console` / `auto` (auto-detects based on TTY). |
| `LOG_LEVEL` | Legacy alias; only used when `APT_PROXY_LOG_LEVEL` is unset. |
| `LOG_FORMAT` | Legacy alias; only used when `APT_PROXY_LOG_FORMAT` is unset. |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | When set, enables OpenTelemetry tracing and exports spans via OTLP to this endpoint. Spans are flushed on graceful shutdown. |

**Configuration Priority:** CLI flags > Environment variables > Config file > Default values

### YAML Configuration File

APT Proxy supports YAML configuration files for more complex setups. Create a file named `apt-proxy.yaml`:

```yaml
server:
  host: 0.0.0.0
  port: 3142
  debug: false

cache:
  dir: /var/cache/apt-proxy
  max_size_gb: 20
  ttl_hours: 168
  cleanup_interval_min: 60
  integrity: sha256          # default; optional none, see cache integrity

# YAML-only shared download/copy budget; zero selects the defaults.
resources:
  max_object_mb: 1024
  total_temp_mb: 4096
  max_active_downloads: 32
  min_free_mb: 128

# YAML-only download bounds; zero selects these defaults, not unlimited values.
download:
  max_spool_mb: 1024        # per unknown-length response, MiB
  total_spool_mb: 4096      # shared unknown-length spool budget, MiB
  timeout_seconds: 1200     # total upstream fetch time

# Optional: switch the cache to an S3-compatible object store.
# When backend is "disk" (the default) only the cache.dir field above matters.
storage:
  backend: disk        # "disk" (default) or "s3"
  s3:
    endpoint: ""
    region: ""
    bucket: ""
    prefix: apt-proxy/
    access_key: ""
    secret_key: ""
    use_ssl: true
    use_path_style: false
    inline_max_mb: 32
    temp_dir: ""

mirrors:
  ubuntu: cn:tsinghua
  ubuntu_ports: ""
  debian: cn:ustc
  debian_security: https://mirrors.ustc.edu.cn/debian-security/
  centos: ""
  alpine: ""

tls:
  enabled: false
  cert_file: /etc/ssl/certs/apt-proxy.crt
  key_file: /etc/ssl/private/apt-proxy.key

security:
  download_allow_cidrs: []             # connection-peer ACL, YAML-only
  download_deny_cidrs: []              # deny takes precedence; empty lists allow all
  api_key: ${APT_PROXY_API_KEY}        # supports ${VAR} and ${VAR:-default} expansion
  enable_api_auth: true
  api_rate_limit_per_minute: 60        # 0 disables; default 60
  trusted_proxies:                     # CIDRs whose X-Forwarded-For is trusted
    - 10.0.0.0/8
    - 192.168.0.0/16

mode: all

# Optional repository connection policy (YAML-only; restart after changes).
# Lists must be empty while disabled. Enabling this ignores environment proxies.
upstream_access:
  enabled: false
  allowed_hosts: []
  allowed_ports: []       # empty means 80/443 when enabled
  allow_cidrs: []         # explicit trusted private-network exceptions

# Upstream transport
upstream_keep_alive: true

# Optional: external distributions/mirrors config (hot-reloadable)
distributions_config: ./config/distributions.yaml
```

**Environment variable expansion in YAML:** values support `${VAR}` and `${VAR:-default}` forms. Bare `$VAR` is **not** expanded. An undefined `${VAR}` is left as-is (instead of becoming empty) so that typos surface loudly.

**Note:** the cache section uses the human-friendly fields shown above (`dir`, `max_size_gb`, `ttl_hours`, `cleanup_interval_min`, `integrity`); the raw byte/duration fields (`max_size`, `ttl`, `cleanup_interval`) are internal representations and are **not** read from YAML.

For the three cache size/TTL/cleanup fields, omitted or nonpositive YAML values
retain the normal defaults (10 GiB, 168 hours and 60 minutes). Explicit CLI or
environment values of `0` disable the corresponding backend control. This
differs from the YAML-only `download`/`resources` limits, where `0` always selects
a bounded default.

**Config file search paths (in order):**
1. Path specified via `-config` flag or `APT_PROXY_CONFIG_FILE` environment variable
2. `./apt-proxy.yaml` (current directory)
3. `/etc/apt-proxy/apt-proxy.yaml`
4. `~/.config/apt-proxy/apt-proxy.yaml`
5. `~/.apt-proxy.yaml`
