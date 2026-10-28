# APT Proxy

[![Go Test Coverage](./.github/coverage.svg)](./.github/go-test-report.md "Last published coverage report snapshot") [![Go Report Card](./.github/goreportcard.svg)](./.github/goreportcard-report.md "Last published code quality report snapshot") [![Security Scan](https://github.com/soulteary/apt-proxy/actions/workflows/scan.yml/badge.svg)](https://github.com/soulteary/apt-proxy/actions/workflows/scan.yml) [![Release](https://github.com/soulteary/apt-proxy/actions/workflows/release.yaml/badge.svg)](https://github.com/soulteary/apt-proxy/actions/workflows/release.yaml) [![Docker Image](https://img.shields.io/docker/pulls/soulteary/apt-proxy.svg)](https://hub.docker.com/r/soulteary/apt-proxy)

[English](README.md) · [中文](README_CN.md)

<p align="center">
  <img src=".github/assets/apt-proxy-logo.png" alt="APT Proxy Logo" width="160"/>
</p>

<p align="center">
  <img src=".github/assets/apt-proxy-banner.jpg" alt="APT Proxy Banner" width="720"/>
</p>

## Overview

APT Proxy is a lightweight, high-performance caching proxy for package managers. It accelerates package downloads by caching frequently used packages locally, dramatically reducing download times for subsequent installations. Whether you're managing multiple servers, building Docker images, or working in bandwidth-constrained environments, APT Proxy helps you save time and bandwidth.

This README is the project entry point. The latest documented release is
[v0.19.4](https://github.com/soulteary/apt-proxy/releases/tag/v0.19.4);
read its [upgrade notes](docs/releases/v0.19.4.md) when updating an older deployment.

### Key Features

- Repository caching verified with APT, DNF/DNF5, APK, pacman and zypper.
- Automatic mirror benchmarking, custom repository mappings and explicit third-party origin allowlists.
- Disk or S3-compatible caching, with private SHA256 integrity records by default.
- Optional complete APT snapshots, runtime mirror failover and coordinated shared Unix disk caching.
- Download concurrency, temporary-storage budgets and separate client/upstream access policies.
- Single-binary and container deployment, health checks, metrics and management APIs.

**Start here:** [Quick start](#quick-start) · [Common tasks](#common-tasks) ·
[Configuration](#configuration) · [Operations](#operations) · [Development](#development).

<!-- Preserve previously published README section links. -->
<a id="supported-platforms"></a>
<a id="proxy-daemon-platforms"></a>
<a id="verified-client-repository-caching"></a>

## Supported Platforms and Clients

The **proxy daemon** runs on Linux (`amd64`, `386`, `arm64`, ARMv6/ARMv7) and
macOS (`amd64`, `arm64`). Releases provide binary archives and Linux
`.deb`/`.rpm`/`.apk` packages, with archive SBOMs and checksums.

Docker Hub and GHCR publish `linux/amd64`, `linux/arm64` and `linux/arm/v7` images.
ARMv6 has binary archives and Linux packages, but no container image.

The v0.19.4 [release gate](https://github.com/soulteary/apt-proxy/actions/runs/40728681270)
passed **18 real package-manager clients on Linux AMD64**:

- APT: Debian 12/13 and Ubuntu 22.04/24.04/26.04.
- DNF/DNF5: CentOS Stream, Rocky Linux and AlmaLinux 9/10; Fedora 44.
- APK: Alpine 3.22/3.23/3.24 using signed legacy packages and indexes.
- pacman: Arch Linux with an explicit repository mapping.
- zypper: openSUSE Leap 16.0 and Tumbleweed with an explicit rpm-md mapping.

These tests use signed local repositories. They do not certify every public
mirror, client CPU architecture, native YUM, APK ADB format or Homebrew/Winget/
Chocolatey/FreeBSD `pkg`. A supported protocol still needs repository routing.
See [verified versions, configuration and boundaries](docs/client-support.md).

<!-- Preserve previously published README section links. -->
<a id="installation"></a>
<a id="running-apt-proxy"></a>
<a id="docker-integration"></a>
<a id="running-apt-proxy-in-docker"></a>
<a id="using-apt-proxy-in-docker-builds"></a>
<a id="docker-compose-example"></a>

## Quick Start

### Docker

Start a proxy with persistent disk caching:

```bash
docker run -d \
  --name apt-proxy \
  -p 3142:3142 \
  -v apt-proxy-cache:/var/cache/apt-proxy \
  soulteary/apt-proxy:v0.19.4
```

The named volume is mounted at the image's actual cache path,
`/var/cache/apt-proxy`, so data survives container removal or recreation.
For bind-mount permissions, Docker builds and Compose, see
[deployment](docs/deployment.md).

### Binary

Download and extract the archive for your platform from
[Releases](https://github.com/soulteary/apt-proxy/releases), then run:

```bash
./apt-proxy
```

The binary listens on `0.0.0.0:3142` and stores disk cache in `./.aptcache` by
default. Mirror benchmarking runs asynchronously; configured or default mirrors
are available while election completes.

### Configure an APT client

For HTTP repository sources, set `/etc/apt/apt.conf.d/01proxy` on the client:

```text
Acquire::http::Proxy "http://APT_PROXY_HOST:3142";
```

Replace `APT_PROXY_HOST` with the proxy server's reachable hostname or IP, then run:

```bash
sudo apt-get update
sudo apt-get install vim
```

For HTTPS origins, use supported repository mappings or the
[`HTTPS///` rewrite marker](docs/repository-configuration.md#apt-cacher-ng-style-host-prefixed-paths).
Setting a generic HTTPS proxy alone does not establish package caching.

### Verify the proxy

On the proxy host:

```bash
curl -fsS http://127.0.0.1:3142/healthz
curl --noproxy '' --proxy http://127.0.0.1:3142 -fsS -D - -o /dev/null \
  http://archive.ubuntu.com/ubuntu/dists/noble/Release
```

Repeat the download to inspect `X-Cache`. Eligible responses become `HIT` after
cache publication completes; `MISS` or `SKIP` alone does not prove a fault.
See [response headers](docs/operations.md#response-headers) for cache and staging semantics.

<!-- Preserve previously published README section links. -->
<a id="usage-examples"></a>
<a id="ubuntu--debian"></a>
<a id="centos"></a>
<a id="alpine-linux"></a>
<a id="advanced-configuration"></a>
<a id="apt-cacher-ng-style-host-prefixed-paths"></a>
<a id="distributions-and-mirrors-config-distributionsyaml"></a>
<a id="adding-a-distribution-apt-proxy-does-not-ship"></a>
<a id="custom-mirror-selection"></a>
<a id="caching-third-party-archives-passthrough"></a>
<a id="reaching-mirrors-through-an-upstream-proxy"></a>
<a id="repository-operations-and-download-policies"></a>
<a id="download-resource-limits"></a>
<a id="private-repository-requests"></a>
<a id="package-download-client-access"></a>
<a id="guarded-download-upstreams"></a>
<a id="debian-family-source-packages"></a>
<a id="debian-family-installer-files"></a>
<a id="debian-family-appstream-metadata"></a>
<a id="temporary-resources-and-cache-integrity"></a>
<a id="complete-repository-snapshots-and-offline-access"></a>
<a id="runtime-mirror-health-and-client-validation"></a>

## Common Tasks

Choose a recipe instead of combining unrelated configuration blocks:

- **Pin a mirror or add a distribution:** [repository configuration](docs/repository-configuration.md),
  [mirror example](examples/specify-mirrors/) and [custom distributions](examples/custom-distros/).
- **Cache a vendor or private repository:** [third-party allowlists](docs/repository-configuration.md#caching-third-party-archives-passthrough),
  [upstream proxies](docs/repository-configuration.md#reaching-mirrors-through-an-upstream-proxy)
  and [private-request isolation](docs/private-repositories.md).
- **Use Proxmox:** [PBS Client recipe](examples/proxmox/). Use v0.19.3 or later;
  [default configuration](config/distributions.yaml) also contains disabled PVE,
  PBS Server, PBS Client and Ceph Squid examples.
- **Use CentOS/Fedora-style discovery:** [Metalink setup](docs/centos-metalink.md).
  Other package-manager client recipes are in the [support guide](docs/client-support.md).
- **Store cache in S3 or share disk cache:** [cache and storage reference](docs/cache-storage.md),
  [OtterIO example](examples/s3-otterio/) and [shared repository cache](docs/shared-repository-cache.md).
- **Maintain an offline APT repository:** [prefetch, verify, repair, export, import and serve](docs/repository-offline.md).
- **Enable bounded APT mirror failover:** [runtime mirror health](docs/runtime-mirror-health.md).
- **Set download boundaries:** [resource admission](docs/resource-limits.md),
  [download deadlines and spooling](docs/download-limits.md),
  [client CIDRs](docs/download-access.md) and [upstream connection policy](docs/upstream-access.md).
- **Understand APT file coverage:** [source packages](docs/apt-source-packages.md),
  [installer files](docs/apt-installer-files.md) and [AppStream](docs/apt-appstream.md).

The [examples directory](examples/) contains **six** examples:
[`basic`](examples/basic/), [`specify-mirrors`](examples/specify-mirrors/),
[`s3-otterio`](examples/s3-otterio/), [`custom-distros`](examples/custom-distros/),
[`proxmox`](examples/proxmox/) and [`config-template`](examples/config-template/).
The last is a configuration reference, not a runnable Compose stack.

<!-- Preserve previously published README section links. -->
<a id="configuration-options"></a>
<a id="environment-variables"></a>
<a id="yaml-configuration-file"></a>
<a id="cache-capacity-and-eviction"></a>
<a id="cache-directory-layout"></a>
<a id="cache-keys-and-mirror-selection"></a>
<a id="s3-storage-backend"></a>

## Configuration

Configuration priority is **CLI flags > environment variables > YAML > defaults**.
Use `./apt-proxy --help` for current flags, or start from the
[annotated YAML template](examples/config-template/apt-proxy.yaml).

A small configuration can pin mirrors and enable management API authentication:

```yaml
server:
  host: 0.0.0.0
  port: 3142
cache:
  dir: /var/cache/apt-proxy
mirrors:
  ubuntu: cn:tsinghua
security:
  api_key: ${APT_PROXY_API_KEY}
  enable_api_auth: true
```

Set `APT_PROXY_API_KEY` to your own nonempty key and use a writable cache path
before starting `./apt-proxy --config=apt-proxy.yaml`. Unset `${VAR}` references
remain literal; the loader does not generate secrets.

- [Flags, environment mappings, YAML fields and configuration-file discovery](docs/configuration-reference.md).
- [Distribution YAML, host/path matching, mirror aliases and custom types](docs/repository-configuration.md).
- [Cache capacity, directory layout, keys and S3 sizing](docs/cache-storage.md).
- [Default integrity checks and legacy cache migration](docs/cache-integrity.md).

Download/resource policies, client CIDRs and upstream access are YAML-only.
Main configuration changes require a restart. SIGHUP and mirror refresh reload
only distribution/mirror definitions and repeat mirror election.

<!-- Preserve previously published README section links. -->
<a id="api-endpoints"></a>
<a id="health--monitoring"></a>
<a id="cache-management-protected"></a>
<a id="mirror-management-protected"></a>
<a id="api-authentication"></a>
<a id="api-rate-limiting"></a>
<a id="response-headers"></a>
<a id="hot-reload"></a>
<a id="observability"></a>
<a id="metrics"></a>
<a id="logging"></a>
<a id="distributed-tracing-opentelemetry"></a>
<a id="troubleshooting"></a>
<a id="403-on-https-urls"></a>
<a id="routing-changed-after-upgrading-to-v0170"></a>
<a id="404-on-a-ppa-or-vendor-repository"></a>
<a id="debug-mode"></a>
<a id="debugging-package-operations"></a>
<a id="common-issues"></a>
<a id="upgrading-to-v0180"></a>

## Operations

Separate management controls from package-download controls:

- `security.api_key` protects management APIs when authentication is enabled.
  Mutating endpoints return 503 while API authentication is disabled.
- Download CIDRs match the connection peer before cache access. API keys and
  forwarded headers do not grant package-download access.
- Health, version and metrics endpoints retain their separate operational scope.

Useful endpoints are `/healthz`, `/livez`, `/readyz`, `/version`, `/metrics`,
`/api/cache/stats` and `/api/mirrors/status`. The
[operations reference](docs/operations.md) describes authentication, rate limits,
cache management, response headers, logging and tracing.

Reload distribution/mirror definitions without restarting:

```bash
# Signal the intended daemon PID:
kill -HUP "$APT_PROXY_PID"

# Or use an enabled management API with its configured key:
curl -fsS -X POST -H "X-API-Key: $APT_PROXY_API_KEY" \
  http://127.0.0.1:3142/api/mirrors/refresh
```

For `403`/`404`, routing changes or cache misses, start with
[troubleshooting](docs/troubleshooting.md). Historical migrations stay in that
guide and the [release notes](docs/releases/v0.19.4.md), rather than in the startup recipe.

<!-- Preserve previously published README section links. -->
<a id="architecture"></a>
<a id="request-flow"></a>
<a id="project-structure"></a>
<a id="building-from-source"></a>
<a id="running-tests"></a>

## Development

Use Go **1.27.3**, matching `go.mod` and CI. Real-client tests additionally need
Python 3 and Docker; selected offline xz indexes need the system `xz` executable.

```bash
git clone https://github.com/soulteary/apt-proxy.git
cd apt-proxy
make build
make test
make cover
scripts/check-coverage.sh coverage.out
```

The gate is **95% aggregate statement coverage across `./...`**, not a per-package
or branch-coverage claim. Checked-in reports are snapshots; evaluate a change
with its own CI run. See [report publication](.github/GO-REPORTS.md).

### Contributing

See [architecture, project structure, builds and integration tests](docs/development.md)
and [real-client verification](docs/client-compatibility.md). Contributions are
welcome through pull requests; add behavior tests for changes to repository routing
or caching guarantees.

## Project Information

### License

Licensed under [Apache License 2.0](LICENSE). See the [security policy](SECURITY.md)
for vulnerability reporting and version guidance.

### Acknowledgments

This project builds on the following projects and their contributors:

- [lox/apt-proxy](https://github.com/lox/apt-proxy) — The original project from which apt-proxy evolved.
- [soulteary/httpcache-kit](https://github.com/soulteary/httpcache-kit) — The current HTTP caching implementation, evolved from [lox/httpcache](https://github.com/lox/httpcache).
- [soulteary/vfs-kit](https://github.com/soulteary/vfs-kit) — The current virtual filesystem library, derived from [rainycape/vfs](https://github.com/rainycape/vfs), under MPL-2.0.
- [djherbis/stream](https://github.com/djherbis/stream) — Used by earlier versions of the project.

## Support

Made with ❤️ by the APT Proxy community
