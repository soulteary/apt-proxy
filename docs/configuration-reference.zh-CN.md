# 配置参考

[项目首页](../README_CN.md) · [English](configuration-reference.md)

## 配置选项

查看所有可用选项：

```bash
./apt-proxy -h
```

**可用选项：** 下表列出 CLI 参数及其环境变量映射。YAML 还提供没有 CLI 参数的策略配置，
详见 YAML 章节和[带注释的模板](../examples/config-template/apt-proxy.yaml)。

| 选项 | 描述 | 默认值 |
|------|------|--------|
| `-host` | 绑定的网络接口 | `0.0.0.0` |
| `-port` | 监听端口 | `3142` |
| `-mode` | 发行版模式：`all`、`ubuntu`、`ubuntu-ports`、`debian`、`centos`、`alpine` | `all` |
| `-cachedir` | 缓存目录 | `./.aptcache` |
| `-ubuntu` | Ubuntu 镜像 URL 或快捷方式 | （自动选择） |
| `-ubuntu-ports` | Ubuntu Ports 镜像 URL 或快捷方式 | （自动选择） |
| `-debian` | Debian 镜像 URL 或快捷方式 | （自动选择） |
| `-debian-security` | 独立的 Debian Security 镜像 URL 或快捷方式 | （从 `-debian` 推导） |
| `-centos` | CentOS 镜像 URL 或快捷方式 | （自动选择） |
| `-alpine` | Alpine 镜像 URL 或快捷方式 | （自动选择） |
| `-distributions-config` | 发行版/镜像配置文件路径（distributions.yaml） | （可选） |
| `-passthrough` | 逗号分隔的第三方源站白名单，原样抓取并缓存 | （空） |
| `-cache-max-size` | 最大缓存大小（GB，0 表示禁用） | `10` |
| `-cache-ttl` | 缓存 TTL（小时，0 表示禁用） | `168`（7 天） |
| `-cache-cleanup-interval` | 缓存清理间隔（分钟，0 表示禁用） | `60` |
| `-tls` | 启用 TLS/HTTPS（需同时提供 `-tls-cert` 与 `-tls-key`） | `false` |
| `-tls-cert` | TLS 证书文件路径 | |
| `-tls-key` | TLS 私钥文件路径 | |
| `-api-key` | 受保护端点的 API 密钥（设置后自动启用鉴权） | |
| `-enable-api-auth` | 显式开/关 API 鉴权中间件 | `false`（设置 `-api-key` 时自动变为 `true`） |
| `-api-rate-limit` | 每 IP 每分钟的 API 请求数（`0` 关闭） | `60` |
| `-trusted-proxies` | 受信任代理 CIDR 列表（逗号分隔），仅对其匹配的请求解析 `X-Forwarded-For` | |
| `-upstream-keep-alive` | 上游 HTTP keep-alive | `true` |
| `-storage-backend` | 缓存存储后端：`disk` 或 `s3` | `disk` |
| `-s3-endpoint` | S3 endpoint（`host[:port]`，不含协议前缀） | |
| `-s3-region` | S3 region（AWS 必填，多数 MinIO 服务可留空） | |
| `-s3-bucket` | S3 bucket 名称 | |
| `-s3-prefix` | bucket 内 key 前缀 | `apt-proxy/` |
| `-s3-access-key` | S3 access key（建议改用 ENV） | |
| `-s3-secret-key` | S3 secret key（建议改用 ENV） | |
| `-s3-session-token` | 临时凭证 session token（可选） | |
| `-s3-use-ssl` | 与 S3 通信使用 HTTPS | `true` |
| `-s3-use-path-style` | 强制 path-style 寻址（MinIO/Ceph 必须开启） | `false` |
| `-s3-inline-max-mb` | 内存缓冲上传阈值（MiB），超过则落盘后再 PUT | `32` |
| `-s3-temp-dir` | 大对象上传时使用的临时目录 | （`os.TempDir()`） |
| `-config` | YAML 配置文件路径 | |
| `-debug` | 启用详细调试日志（同时把请求 header/body 打印到日志） | `false` |

**自定义配置示例：**

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

### 环境变量

每个 CLI 参数都有等价的环境变量。此外还有几个仅环境变量提供的开关，用于日志与链路追踪。

**服务/模式**

| 变量名 | 等价 CLI 参数 | 说明 |
|--------|--------------|------|
| `APT_PROXY_HOST` | `-host` | 监听的网络接口 |
| `APT_PROXY_PORT` | `-port` | 监听端口 |
| `APT_PROXY_MODE` | `-mode` | 发行版模式（`all`/`ubuntu`/`ubuntu-ports`/`debian`/`centos`/`alpine`） |
| `APT_PROXY_DEBUG` | `-debug` | 启用详细调试日志 |
| `APT_PROXY_UBUNTU` | `-ubuntu` | Ubuntu 镜像 URL 或快捷方式 |
| `APT_PROXY_UBUNTU_PORTS` | `-ubuntu-ports` | Ubuntu Ports 镜像 |
| `APT_PROXY_DEBIAN` | `-debian` | Debian 镜像 |
| `APT_PROXY_DEBIAN_SECURITY` | `-debian-security` | 独立的 Debian Security 镜像 |
| `APT_PROXY_CENTOS` | `-centos` | CentOS 镜像 |
| `APT_PROXY_ALPINE` | `-alpine` | Alpine 镜像 |
| `APT_PROXY_UPSTREAM_KEEP_ALIVE` | `-upstream-keep-alive` | 上游 HTTP keep-alive |

**缓存**

| 变量名 | 等价 CLI 参数 | 说明 |
|--------|--------------|------|
| `APT_PROXY_CACHEDIR` | `-cachedir` | 缓存目录 |
| `APT_PROXY_CACHE_MAX_SIZE` | `-cache-max-size` | 最大缓存大小（GB，`0` 禁用） |
| `APT_PROXY_CACHE_TTL` | `-cache-ttl` | 缓存 TTL（小时，`0` 禁用） |
| `APT_PROXY_CACHE_CLEANUP_INTERVAL` | `-cache-cleanup-interval` | 缓存清理间隔（分钟，`0` 禁用） |

**存储后端 / S3**

| 变量名 | 等价 CLI 参数 | 说明 |
|--------|--------------|------|
| `APT_PROXY_STORAGE_BACKEND` | `-storage-backend` | `disk` 或 `s3` |
| `APT_PROXY_S3_ENDPOINT` | `-s3-endpoint` | S3 endpoint，如 `s3.amazonaws.com` 或 `minio:9000` |
| `APT_PROXY_S3_REGION` | `-s3-region` | S3 region |
| `APT_PROXY_S3_BUCKET` | `-s3-bucket` | bucket 名称 |
| `APT_PROXY_S3_PREFIX` | `-s3-prefix` | key 前缀（默认 `apt-proxy/`） |
| `APT_PROXY_S3_ACCESS_KEY` | `-s3-access-key` | access key（**推荐**通过 ENV 注入） |
| `APT_PROXY_S3_SECRET_KEY` | `-s3-secret-key` | secret key（**推荐**通过 ENV 注入） |
| `APT_PROXY_S3_SESSION_TOKEN` | `-s3-session-token` | STS 临时 session token |
| `APT_PROXY_S3_USE_SSL` | `-s3-use-ssl` | 是否使用 HTTPS |
| `APT_PROXY_S3_USE_PATH_STYLE` | `-s3-use-path-style` | path-style 寻址（MinIO/Ceph 必须） |
| `APT_PROXY_S3_INLINE_MAX_MB` | `-s3-inline-max-mb` | 内存缓冲阈值（MiB） |
| `APT_PROXY_S3_TEMP_DIR` | `-s3-temp-dir` | 大对象临时目录 |

**上游网络**（标准变量，无对应 CLI 参数）

| 变量名 | 说明 |
|--------|------|
| `HTTP_PROXY` / `http_proxy` | 访问镜像站的 `http://` 请求所用的前置代理 |
| `HTTPS_PROXY` / `https_proxy` | 访问镜像站的 `https://` 请求所用的前置代理（支持 `socks5h://`） |
| `NO_PROXY` / `no_proxy` | 不走前置代理的主机名、域名后缀与 CIDR |

**TLS**

| 变量名 | 等价 CLI 参数 | 说明 |
|--------|--------------|------|
| `APT_PROXY_TLS_ENABLED` | `-tls` | 启用 TLS/HTTPS |
| `APT_PROXY_TLS_CERT` | `-tls-cert` | TLS 证书文件路径 |
| `APT_PROXY_TLS_KEY` | `-tls-key` | TLS 私钥文件路径 |

**安全 / API**

| 变量名 | 等价 CLI 参数 | 说明 |
|--------|--------------|------|
| `APT_PROXY_API_KEY` | `-api-key` | 受保护端点的 API 密钥 |
| `APT_PROXY_ENABLE_API_AUTH` | `-enable-api-auth` | 显式开关 API 鉴权 |
| `APT_PROXY_API_RATE_LIMIT_PER_MINUTE` | `-api-rate-limit` | 每 IP 每分钟 API 请求数（`0` 禁用） |
| `APT_PROXY_TRUSTED_PROXIES` | `-trusted-proxies` | 受信任代理 CIDR 列表 |

**配置文件**

| 变量名 | 等价 CLI 参数 | 说明 |
|--------|--------------|------|
| `APT_PROXY_CONFIG_FILE` | `-config` | `apt-proxy.yaml` 路径 |
| `APT_PROXY_DISTRIBUTIONS_CONFIG` | `-distributions-config` | `distributions.yaml` 路径 |
| `APT_PROXY_PASSTHROUGH` | `-passthrough` | 第三方源站白名单 |

**日志与链路追踪**（无对应 CLI 参数）

| 变量名 | 说明 |
|--------|------|
| `APT_PROXY_LOG_LEVEL` | 日志级别：`debug` / `info` / `warn` / `error`。`--debug` 会强制为 `debug` |
| `APT_PROXY_LOG_FORMAT` | 日志格式：`json` / `console` / `auto`（auto 根据 stdout 是否为 TTY 自动选择） |
| `LOG_LEVEL` | 旧版别名；仅在未设置 `APT_PROXY_LOG_LEVEL` 时使用 |
| `LOG_FORMAT` | 旧版别名；仅在未设置 `APT_PROXY_LOG_FORMAT` 时使用 |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | 设置后启用 OpenTelemetry 链路追踪并通过 OTLP 上报；优雅退出时自动 flush |

**配置优先级：** CLI 参数 > 环境变量 > 配置文件 > 默认值

### YAML 配置文件

APT Proxy 支持 YAML 配置文件，适用于更复杂的配置场景。创建名为 `apt-proxy.yaml` 的文件：

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
  integrity: sha256          # 默认；可选 none，见缓存完整性说明

# 所有下载与暂存副本共用；仅支持 YAML，0 选择默认值。
resources:
  max_object_mb: 1024
  total_temp_mb: 4096
  max_active_downloads: 32
  min_free_mb: 128

# 仅支持 YAML；0 选择以下默认值，不表示无限制。
download:
  max_spool_mb: 1024        # 每个未知长度响应的落盘上限，MiB
  total_spool_mb: 4096      # 实例共享的未知长度落盘预算，MiB
  timeout_seconds: 1200     # 上游下载总时限

# 可选：将缓存改为 S3 兼容对象存储；backend 为 "disk" 时下面的 s3 字段被忽略
storage:
  backend: disk        # "disk"（默认）或 "s3"
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
  download_allow_cidrs: []             # 连接对端 CIDR，仅支持 YAML
  download_deny_cidrs: []              # 拒绝优先；两项为空时允许所有客户端
  api_key: ${APT_PROXY_API_KEY}        # 支持 ${VAR} 与 ${VAR:-default} 展开
  enable_api_auth: true
  api_rate_limit_per_minute: 60        # 0 禁用；默认 60
  trusted_proxies:                     # 受信任代理 CIDR 列表，仅其匹配请求会解析 X-Forwarded-For
    - 10.0.0.0/8
    - 192.168.0.0/16

mode: all

# 可选仓库连接策略：仅支持 YAML，变更后重启。
# 关闭时限制列表必须为空；启用后忽略前置代理环境变量。
upstream_access:
  enabled: false
  allowed_hosts: []
  allowed_ports: []       # 启用时为空表示允许 80/443
  allow_cidrs: []         # 显式允许受信任私网范围

# 上游传输
upstream_keep_alive: true

# 可选：外部 distributions/mirrors 配置文件（可热重载）
distributions_config: ./config/distributions.yaml
```

**YAML 中的环境变量展开：** 仅识别 `${VAR}` 与 `${VAR:-default}` 两种形式；裸 `$VAR` **不会**被展开。未定义的 `${VAR}` 会**保持原样**（而不是替换为空），便于及时发现拼写错误。

**说明：** 缓存识别上述人类可读字段（`dir`、`max_size_gb`、`ttl_hours`、`cleanup_interval_min`、`integrity`）；原始字节 / Duration 字段（`max_size`、`ttl`、`cleanup_interval`）属于内部表示，不会从 YAML 读入。

缓存大小、TTL 与清理间隔这三个 YAML 字段省略或非正值时，保留普通默认值
（10 GiB、168 小时、60 分钟）；通过 CLI 或环境变量显式设置 `0` 才会关闭对应后端控制。
这与仅支持 YAML 的 `download` / `resources` 限制不同，后者的 `0` 始终选择有界默认值。

**配置文件搜索路径（按顺序）：**
1. 通过 `-config` 参数或 `APT_PROXY_CONFIG_FILE` 环境变量指定的路径
2. `./apt-proxy.yaml`（当前目录）
3. `/etc/apt-proxy/apt-proxy.yaml`（系统配置）
4. `~/.config/apt-proxy/apt-proxy.yaml`（用户配置）
5. `~/.apt-proxy.yaml`（用户配置）
