# APT Proxy / 轻量 APT 加速工具

[![Go Test Coverage](./.github/coverage.svg)](./.github/go-test-report.md "最近一次发布的覆盖率报告快照") [![Go Report Card](./.github/goreportcard.svg)](./.github/goreportcard-report.md "最近一次发布的代码质量报告快照") [![Security Scan](https://github.com/soulteary/apt-proxy/actions/workflows/scan.yml/badge.svg)](https://github.com/soulteary/apt-proxy/actions/workflows/scan.yml) [![Release](https://github.com/soulteary/apt-proxy/actions/workflows/release.yaml/badge.svg)](https://github.com/soulteary/apt-proxy/actions/workflows/release.yaml) [![Docker Image](https://img.shields.io/docker/pulls/soulteary/apt-proxy.svg)](https://hub.docker.com/r/soulteary/apt-proxy)

[English](README.md) · [中文](README_CN.md)

<p align="center">
  <img src=".github/assets/apt-proxy-logo.png" alt="APT Proxy Logo" width="160"/>
</p>

<p align="center">
  <img src=".github/assets/apt-proxy-banner.jpg" alt="APT Proxy Banner" width="720"/>
</p>

## 概述

APT Proxy 是一个轻量级、高性能的包管理器缓存代理。它通过在本地缓存常用软件包来加速下载，大幅减少后续安装的下载时间。无论你是管理多台服务器、构建 Docker 镜像，还是在带宽受限的环境中工作，APT Proxy 都能帮你节省时间和带宽。

本文是项目入口。最新记录的发布版本为
[v0.19.4](https://github.com/soulteary/apt-proxy/releases/tag/v0.19.4)，
升级旧部署时请阅读[升级说明](docs/releases/v0.19.4.zh-CN.md)。

### 核心特性

- 已验证 APT、DNF/DNF5、APK、pacman、zypper 的仓库缓存。
- 自动镜像测速、自定义仓库映射，以及显式第三方源站白名单。
- 磁盘或 S3 兼容缓存，默认使用私有 SHA256 完整性记录。
- 可选完整 APT 快照、运行时镜像故障切换和共享 Unix 磁盘缓存协调。
- 下载并发与临时存储预算，以及独立的客户端和上游访问策略。
- 单二进制与容器部署，配备健康检查、指标和管理 API。

**从这里开始：**[快速开始](#快速开始) · [常见任务](#常见任务) ·
[配置](#配置) · [运维](#运维) · [开发](#开发)。

<!-- Preserve previously published README section links. -->
<a id="支持的平台"></a>
<a id="代理服务端运行平台"></a>
<a id="已验证的客户端软件仓库缓存"></a>

## 支持的平台与客户端

**代理服务端**支持 Linux（`amd64`、`386`、`arm64`、ARMv6/ARMv7）和
macOS（`amd64`、`arm64`）。发布产物包括二进制归档、Linux `.deb`/`.rpm`/`.apk`
安装包，以及归档 SBOM 和校验和。

Docker Hub 与 GHCR 提供 `linux/amd64`、`linux/arm64`、`linux/arm/v7` 镜像。
ARMv6 提供二进制归档和 Linux 安装包，未提供容器镜像。

v0.19.4 的[发布验证](https://github.com/soulteary/apt-proxy/actions/runs/40728681270)
通过了 **Linux AMD64 上的 18 个真实包管理器客户端**：

- APT：Debian 12/13，Ubuntu 22.04/24.04/26.04。
- DNF/DNF5：CentOS Stream、Rocky Linux、AlmaLinux 9/10，以及 Fedora 44。
- APK：Alpine 3.22/3.23/3.24，使用传统签名包与索引。
- pacman：Arch Linux，需显式配置仓库映射。
- zypper：openSUSE Leap 16.0、Tumbleweed，需显式配置 rpm-md 映射。

这些测试使用本地签名仓库，不代表每个公共镜像、客户端 CPU 架构、原生 YUM、APK ADB
格式或 Homebrew/Winget/Chocolatey/FreeBSD `pkg` 均已验证。支持某种协议仍需要配置
仓库路由。版本、配置要求和边界见[客户端支持说明](docs/client-support.md)。

<!-- Preserve previously published README section links. -->
<a id="安装"></a>
<a id="运行-apt-proxy"></a>
<a id="docker-集成"></a>
<a id="在-docker-中运行-apt-proxy"></a>
<a id="在-docker-构建中使用-apt-proxy"></a>
<a id="docker-compose-示例"></a>

## 快速开始

### Docker

启动代理，并将磁盘缓存持久化：

```bash
docker run -d \
  --name apt-proxy \
  -p 3142:3142 \
  -v apt-proxy-cache:/var/cache/apt-proxy \
  soulteary/apt-proxy:v0.19.4
```

命名卷挂载到镜像实际使用的 `/var/cache/apt-proxy`，容器删除或重建后仍保留缓存。
bind mount 权限、Docker 构建及 Compose 用法见[部署文档](docs/deployment.zh-CN.md)。

### 二进制

从 [Releases](https://github.com/soulteary/apt-proxy/releases) 下载并解压对应平台归档，再运行：

```bash
./apt-proxy
```

二进制默认监听 `0.0.0.0:3142`，磁盘缓存位于 `./.aptcache`。
镜像测速异步运行，选优完成前可使用已配置或默认的镜像。

### 配置 APT 客户端

对于 HTTP 仓库源，在客户端的 `/etc/apt/apt.conf.d/01proxy` 中设置：

```text
Acquire::http::Proxy "http://APT_PROXY_HOST:3142";
```

将 `APT_PROXY_HOST` 替换为客户端能访问的代理主机名或 IP，再执行：

```bash
sudo apt-get update
sudo apt-get install vim
```

HTTPS 源请使用支持的仓库映射或
[`HTTPS///` 重写标记](docs/repository-configuration.zh-CN.md#apt-cacher-ng-风格的主机名前缀路径)。
仅设置通用 HTTPS 代理，并不能证明软件包会被缓存。

### 验证代理

在代理主机上执行：

```bash
curl -fsS http://127.0.0.1:3142/healthz
curl --noproxy '' --proxy http://127.0.0.1:3142 -fsS -D - -o /dev/null \
  http://archive.ubuntu.com/ubuntu/dists/noble/Release
```

重复下载并查看 `X-Cache`。响应符合缓存条件且发布完成后会显示 `HIT`；单独出现 `MISS`
或 `SKIP` 不代表故障。缓存与暂存状态的含义见[响应头说明](docs/operations.zh-CN.md#响应头)。

<!-- Preserve previously published README section links. -->
<a id="使用示例"></a>
<a id="ubuntu--debian"></a>
<a id="centos"></a>
<a id="alpine-linux"></a>
<a id="高级配置"></a>
<a id="apt-cacher-ng-风格的主机名前缀路径"></a>
<a id="发行版与镜像配置文件distributionsyaml"></a>
<a id="添加-apt-proxy-未内置的发行版"></a>
<a id="自定义镜像选择"></a>
<a id="缓存第三方软件源passthrough"></a>
<a id="通过前置代理访问镜像站"></a>
<a id="仓库操作与下载策略"></a>
<a id="下载资源限制"></a>
<a id="私有仓库请求"></a>
<a id="软件包下载客户端访问控制"></a>
<a id="下载上游连接防护"></a>
<a id="debian-系源码包覆盖"></a>
<a id="debian-系安装器文件"></a>
<a id="debian-系-appstream-元数据"></a>
<a id="完整仓库快照和离线访问"></a>
<a id="临时资源与缓存完整性"></a>
<a id="运行时镜像健康与客户端验证"></a>

## 常见任务

按任务选择配置，避免混用无关的配置块：

- **指定镜像或添加发行版：**[仓库配置](docs/repository-configuration.zh-CN.md)、
  [指定镜像示例](examples/specify-mirrors/)与[自定义发行版](examples/custom-distros/)。
- **缓存厂商源或私有仓库：**[第三方白名单](docs/repository-configuration.zh-CN.md#缓存第三方软件源passthrough)、
  [前置代理](docs/repository-configuration.zh-CN.md#通过前置代理访问镜像站)与[私有请求隔离](docs/private-repositories.md)。
- **使用 Proxmox：**[PBS Client 示例](examples/proxmox/)，请使用 v0.19.3 或更高版本。
  [默认配置](config/distributions.yaml)还提供默认注释的 PVE、PBS Server、PBS Client、Ceph Squid 条目。
- **使用 CentOS/Fedora 类发现流程：**[Metalink 配置](docs/centos-metalink.md)。
  其他包管理器用法见[客户端支持说明](docs/client-support.md)。
- **使用 S3 或共享磁盘缓存：**[缓存与存储参考](docs/cache-storage.zh-CN.md)、
  [OtterIO 示例](examples/s3-otterio/)与[共享仓库缓存](docs/shared-repository-cache.md)。
- **维护离线 APT 仓库：**[预取、校验、修复、导入导出与服务](docs/repository-offline.md)。
- **启用受限的 APT 镜像故障切换：**[运行时镜像健康](docs/runtime-mirror-health.md)。
- **设置下载边界：**[资源准入](docs/resource-limits.md)、[下载期限与暂存](docs/download-limits.md)、
  [客户端 CIDR](docs/download-access.md)与[上游连接策略](docs/upstream-access.md)。
- **了解 APT 文件支持范围：**[源码包](docs/apt-source-packages.md)、
  [安装器文件](docs/apt-installer-files.md)与[AppStream](docs/apt-appstream.md)。

[examples 目录](examples/)包含 **六个**示例：
[`basic`](examples/basic/)、[`specify-mirrors`](examples/specify-mirrors/)、
[`s3-otterio`](examples/s3-otterio/)、[`custom-distros`](examples/custom-distros/)、
[`proxmox`](examples/proxmox/)、[`config-template`](examples/config-template/)。
最后一个是配置参考，非可直接运行的 Compose 部署。

<!-- Preserve previously published README section links. -->
<a id="配置选项"></a>
<a id="环境变量"></a>
<a id="yaml-配置文件"></a>
<a id="缓存容量与淘汰策略"></a>
<a id="缓存目录结构"></a>
<a id="缓存键与镜像选择"></a>
<a id="s3-存储后端"></a>

## 配置

配置优先级为 **CLI 参数 > 环境变量 > YAML > 默认值**。
使用 `./apt-proxy --help` 查看当前参数，或从[带注释的 YAML 模板](examples/config-template/apt-proxy.yaml)开始。

下面的最小配置可以指定镜像并启用管理 API 认证：

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

启动 `./apt-proxy --config=apt-proxy.yaml` 前，请将 `APT_PROXY_API_KEY` 设为自己的非空密钥，
并确保缓存目录可写。未设置的 `${VAR}` 引用会保留为字面值，加载器不会自动生成密钥。

- [CLI 参数、环境变量映射、YAML 字段与配置文件发现](docs/configuration-reference.zh-CN.md)。
- [发行版 YAML、域名与路径匹配、镜像别名和自定义类型](docs/repository-configuration.zh-CN.md)。
- [缓存容量、目录布局、键与 S3 预算](docs/cache-storage.zh-CN.md)。
- [默认完整性校验与旧缓存迁移](docs/cache-integrity.md)。

下载与资源策略、客户端 CIDR、上游访问策略仅支持 YAML。
主配置修改后需要重启。SIGHUP 与镜像刷新只会重载发行版/镜像定义，并重新选优。

<!-- Preserve previously published README section links. -->
<a id="api-端点"></a>
<a id="健康检查与监控"></a>
<a id="缓存管理受保护"></a>
<a id="镜像管理受保护"></a>
<a id="api-认证"></a>
<a id="api-限流"></a>
<a id="响应头"></a>
<a id="热重载"></a>
<a id="可观测性"></a>
<a id="指标"></a>
<a id="日志"></a>
<a id="分布式追踪opentelemetry"></a>
<a id="故障排除"></a>
<a id="https-形式的-url-返回-403"></a>
<a id="升级到-v0170-后路由发生变化"></a>
<a id="ppa-或厂商软件源返回-404"></a>
<a id="调试模式"></a>
<a id="调试包操作"></a>
<a id="常见问题"></a>
<a id="升级到-v0180"></a>

## 运维

管理控制与软件下载控制分别配置：

- 启用认证后，`security.api_key` 保护管理 API；API 认证关闭时，修改类接口返回 503。
- 下载 CIDR 在访问缓存前匹配连接对端。API Key 和转发头不会授予软件下载权限。
- 健康、版本和指标接口保持各自的运维范围。

常用接口包括 `/healthz`、`/livez`、`/readyz`、`/version`、`/metrics`、
`/api/cache/stats` 和 `/api/mirrors/status`。认证、限流、缓存管理、响应头、日志和链路追踪
详见[运维参考](docs/operations.zh-CN.md)。

无需重启即可重载发行版/镜像定义：

```bash
# 向目标服务进程的 PID 发送信号：
kill -HUP "$APT_PROXY_PID"

# 或启用管理 API 认证，并使用已配置的密钥：
curl -fsS -X POST -H "X-API-Key: $APT_PROXY_API_KEY" \
  http://127.0.0.1:3142/api/mirrors/refresh
```

遇到 `403`/`404`、路由变化或缓存未命中时，请先查看[故障排查](docs/troubleshooting.zh-CN.md)。
历史迁移细节保留在该文档和[版本说明](docs/releases/v0.19.4.zh-CN.md)中。

<!-- Preserve previously published README section links. -->
<a id="架构"></a>
<a id="请求流程"></a>
<a id="项目结构"></a>
<a id="从源码构建"></a>
<a id="运行测试"></a>

## 开发

使用与 `go.mod`、CI 一致的 **Go 1.27.3**。真实客户端测试还需要 Python 3 与 Docker；
离线快照选择 xz 索引时，需要系统提供 `xz` 程序。

```bash
git clone https://github.com/soulteary/apt-proxy.git
cd apt-proxy
make build
make test
make cover
scripts/check-coverage.sh coverage.out
```

门槛为 **`./...` 的整体语句覆盖率 95%**，不代表每个包或分支覆盖率。
仓库中的报告是快照，评估改动时应以本次 CI 为准，详见[报告发布流程](.github/GO-REPORTS.md)。

### 贡献代码

[架构、项目结构、构建和集成测试](docs/development.zh-CN.md)，以及
[真实客户端验证](docs/client-compatibility.md)记录了开发流程。欢迎提交 PR；
修改仓库路由或缓存保证时，请补充行为测试。

## 项目信息

### 开源协议

本项目采用 [Apache License 2.0](LICENSE)，漏洞报告与版本说明见[安全策略](SECURITY.md)。

### 致谢

感谢以下项目及其贡献者：

- [lox/apt-proxy](https://github.com/lox/apt-proxy) — 本项目最初源自的 APT 代理实现。
- [soulteary/httpcache-kit](https://github.com/soulteary/httpcache-kit) — 当前使用的 HTTP 缓存实现，由 [lox/httpcache](https://github.com/lox/httpcache) 演进而来。
- [soulteary/vfs-kit](https://github.com/soulteary/vfs-kit) — 当前使用的虚拟文件系统库，源自 [rainycape/vfs](https://github.com/rainycape/vfs)，采用 MPL-2.0 协议。
- [djherbis/stream](https://github.com/djherbis/stream) — 项目早期版本使用的流处理库。

## 支持

由 APT Proxy 社区用 ❤️ 打造
