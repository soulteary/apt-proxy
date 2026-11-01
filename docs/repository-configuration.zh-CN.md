# 仓库配置与客户端用法

[项目首页](../README_CN.md) · [English](repository-configuration.md)

## 使用示例

### Ubuntu / Debian

通过设置 `http_proxy` 环境变量来配置系统使用代理：

```bash
# 更新软件包列表（首次运行会下载并缓存）
http_proxy=http://your-domain-or-ip-address:3142 \
  apt-get -o pkgProblemResolver=true -o Acquire::http=true update

# 安装软件包（后续安装将使用缓存的软件包）
http_proxy=http://your-domain-or-ip-address:3142 \
  apt-get -o pkgProblemResolver=true -o Acquire::http=true install vim -y
```

**提示**：为了方便，你可以在 shell 中导出代理设置：

```bash
export http_proxy=http://your-domain-or-ip-address:3142
apt-get update
apt-get install vim -y
```

首次下载后，所有后续的包操作都会显著加快，因为软件包从本地缓存提供。

### CentOS

APT Proxy 支持 DNF/YUM 仓库。CentOS Stream 9 和 10 使用
[CentOS Stream 镜像](https://mirror.stream.centos.org/)和基于 metalink 的仓库定义。
要保留 metalink 发现与 DNF 的元数据摘要校验，可启用可选的 `metalink`
配置，将仓库文件中的 metalink 服务地址改为 apt-proxy。返回 XML 中的下载链接
会指向代理，并按仓库快照共享缓存。详见
[CentOS Metalink 配置](../docs/centos-metalink.md)和
[真实 DNF 客户端测试](../docs/dnf-client-tests.md)。

[Docker 客户端兼容性测试](../docs/client-compatibility.md)使用 APT、DNF/DNF5、APK、
pacman 和 zypper 验证签名仓库下载、安装和代理缓存复用。可在 GitHub Actions
手动运行；完整矩阵也会在版本发布前运行，全部通过后才能发布。

如果使用固定 base URL，先为 apt-proxy 指定 Stream 镜像：

```bash
./apt-proxy \
  --mode=centos \
  --centos=https://mirror.stream.centos.org/
```

在每台 CentOS Stream 9 或 10 客户端上，禁用 metalink，并通过 apt-proxy
启用对应的 base URL。原有的 `$stream` 和 `$basearch` 变量会选择当前系统的
Stream 版本和架构：

```bash
sudo sed -i \
  -e '/^metalink=/s/^/#/' \
  -e 's|^#baseurl=http://mirror.stream.centos.org|baseurl=http://your-domain-or-ip-address:3142/centos|' \
  -e 's|^#baseurl=https://mirror.stream.centos.org|baseurl=http://your-domain-or-ip-address:3142/centos|' \
  /etc/yum.repos.d/centos*.repo

sudo dnf clean all
sudo dnf makecache
```

如果镜像提供方已经修改过仓库文件，请在执行命令前先检查文件内容。
这套固定 base URL 配置不使用 metalink；可选的 metalink 入口需要明确配置允许的镜像主机。
客户端使用 HTTP
访问 apt-proxy，apt-proxy 再从配置好的 HTTPS 上游获取内容。

### Alpine Linux

配置 Alpine 的 APK 包管理器使用代理：

```bash
# 更新仓库使用代理
cat /etc/apk/repositories | \
  sed -e s#https://.*.alpinelinux.org#http://your-domain-or-ip-address:3142# | \
  tee /etc/apk/repositories

# 验证配置
apk update
```

## 高级配置

### apt-cacher-ng 风格的主机名前缀路径

对于已知发行版，apt-proxy 兼容 apt-cacher-ng 常用的主机名前缀路径形式。例如：

```text
deb http://apt-proxy.example:3142/ftp.uni-kl.de/debian bookworm main
```

路径中的主机名仅作为兼容前缀。
`/ftp.uni-kl.de/debian/dists/bookworm/InRelease` 这类请求仍会被路由到
apt-proxy 中配置的 Debian 镜像；它不会授权访问 `ftp.uni-kl.de`，也不会将服务
变成不受限制的来源代理。带主机名前缀的
`/security.debian.org/debian-security/...` 路径会使用单独配置的 Debian
Security 镜像。查询参数会被保留，无法匹配已配置发行版的路径返回 `404`。

前缀必须是单个主机名：要么没有前缀（`/debian/dists/...`），要么在发行版路径段
前恰好只有一段形如主机名的内容（`/ftp.uni-kl.de/debian/dists/...`，可带
`:端口`，也可以是 IP 字面量或 `localhost`）。层级更深的路径一律返回 `404`，
因为那是第三方软件源，而不是该发行版的镜像：

```text
/ppa.launchpad.net/deadsnakes/ppa/ubuntu/dists/jammy/InRelease   404
/download.docker.com/linux/ubuntu/dists/jammy/InRelease          404
```

这类软件源并不是它所嵌套的发行版的副本，若用该发行版的镜像来应答，客户端拿到的
就是另一个仓库的内容。apt-proxy 没有 PPA 或厂商源的镜像，请在 `sources.list`
中直接指向它们的源站。
### 发行版与镜像配置文件（distributions.yaml）

通过外部 YAML 文件可维护发行版和镜像列表，无需改代码或重新编译。

**自 v0.19.2 起提供（[#167](https://github.com/soulteary/apt-proxy/issues/167)）：**
下述域名与路径联合匹配优先级、严格 YAML 校验已包含在 v0.19.2 及后续版本中。
v0.19.3 还修复了镜像根地址缺少尾部斜杠时的 URL 拼接；Proxmox 部署请使用
v0.19.3 或更高版本。[Proxmox Backup Client 示例](../examples/proxmox)提供保留官方源地址的
代理配置，以及独立的本地路径别名。v0.19.4 在
[`config/distributions.yaml`](../config/distributions.yaml) 中补充了默认注释的 PVE、PBS Server、
PBS Client 和 Ceph Squid 条目，需要手动加入有效发行版列表才会启用。

**让 apt-proxy 读到这个文件：** 把它放在查找路径之一，或显式指定路径。未指定路径
时，按以下顺序取第一个存在的文件：

1. `./config/distributions.yaml`
2. `./distributions.yaml`
3. `/etc/apt-proxy/distributions.yaml`
4. `~/.config/apt-proxy/distributions.yaml`

```bash
# 放在查找路径上，自动发现
./apt-proxy

# 或显式指定，位置不受限制
./apt-proxy --distributions-config=/srv/apt-proxy/distributions.yaml
```

环境变量 `APT_PROXY_DISTRIBUTIONS_CONFIG` 与该 flag 等价。都找不到时使用内置发行版；
文件解析失败时会保留内置发行版并打一条 warning，不会导致服务启动失败。

配置必须是单个 YAML 文档，并使用已定义的字段名。未知字段会报出行号和字段名，
例如应写 `cache_control`，而非 `cache-control`。空文件仍然有效；支持在已定义字段
内部使用 YAML 锚点与合并，不支持额外的顶层锚点辅助字段。这项校验仅针对
`distributions.yaml`，不改变服务器主配置的加载方式。

**示例 `config/distributions.yaml`：**

```yaml
distributions:
  - id: ubuntu
    name: Ubuntu
    type: 1
    url_pattern: "/ubuntu/(.+)$"
    benchmark_url: "dists/noble/main/binary-amd64/Release"
    geo_mirror_api: "http://mirrors.ubuntu.com/mirrors.txt"
    cache_rules:
      - pattern: "deb$"
        cache_control: "max-age=100000"
        rewrite: true
    mirrors:
      official:
        - "mirrors.tuna.tsinghua.edu.cn/ubuntu/"
        - "mirrors.ustc.edu.cn/ubuntu/"
      custom:
        - "mirrors.163.com/ubuntu/"
    aliases:
      tsinghua: "mirrors.tuna.tsinghua.edu.cn/ubuntu/"
      ustc: "mirrors.ustc.edu.cn/ubuntu/"
```

修改配置后，发送 **SIGHUP** 或调用 **POST /api/mirrors/refresh** 即可热重载，无需重启。

路由先查找同时匹配域名和路径的条目，再匹配路径，最后按域名匹配根目录仓库。
每个阶段内仍按既定顺序匹配内置发行版，自定义条目按 `id` 升序匹配。
选中发行版后使用第一条命中的 `cache_rules[]`；没有命中缓存规则时返回 `404`，
不会继续尝试其他仓库。配置加载或镜像选优失败时，热重载保留原有路由与缓存规则。

**字段说明：**

- `id` — 稳定、唯一的注册标识符，也会出现在 URL 重写调试日志中；实际匹配的 URL 路径由 `url_pattern` 决定。
- `name` — 显示名。
- `type` — 整数类型。`1` Ubuntu、`2` UbuntuPorts、`3` Debian、`4` CentOS、`5` Alpine 用于覆盖内置发行版的配置，`0` 保留给 "all"。**填写其它正整数即可注册一个 apt-proxy 未内置的发行版** —— 参见[添加 apt-proxy 未内置的发行版](#添加-apt-proxy-未内置的发行版)。已被占用的类型在加载时会直接报错，所以请挑一个未使用的数字（`6`、`7`……）并保持稳定：镜像选择与 rewriter 状态都以它为键跨重载保存。
- `url_pattern` — 用于匹配请求路径的正则；最后一个捕获组会拼到上游镜像根目录后。
- `host_pattern` — 可选，用于匹配原始请求域名的正则，匹配前转为小写并保留显式端口。同时命中域名和 `url_pattern` 的条目优先于通用路径规则；没有任何发行版路径命中时，才按域名匹配，并把整个请求路径拼到镜像根目录后，适用于 `security.debian.org`、`apt.armbian.com` 等域名根仓库。请使用 `^...$` 锚定，避免匹配相似域名。它是路由提示，不是域名访问白名单：本地路径别名仍可使用代理自身的域名。省略时继承该类型的内置匹配器（Debian 保留 `security.debian.org`），显式设置则覆盖内置值。只有内置 Debian security 域名会路由到专用的 Debian Security 镜像；为 type `3` 配置的其他域名使用该条目自己的镜像。
- `benchmark_url` — 镜像测速用的相对路径。
- `geo_mirror_api` — 可选，返回地理镜像列表（Ubuntu 风格的 `mirrors.txt`）。
- `cache_rules[]` — 上游没有提供 `Cache-Control` 与 `Expires` 时，按规则补充默认值：`200` 使用配置值，`404` 使用 `public, max-age=30`。上游显式指令与过期时间（包括过去或无效的 `Expires`）、较短有效期、`private` 和 `no-store` 会被保留。默认值在写入缓存前应用；`rewrite` 决定是否对该模式做 URL 重写。
- `mirrors.official` / `mirrors.custom` — 镜像主机列表。基于主机末段会自动生成 `cn:<name>` 形式的别名（如 `mirrors.tuna.tsinghua.edu.cn` → `cn:tsinghua`）。
- `aliases` — 显式别名映射，可覆盖/补充自动生成的别名。

**添加或修改发行版：** 在 `distributions` 下增加或编辑一项，填写 `id`、`name`、`type`、`url_pattern`、`benchmark_url`、`cache_rules`、`mirrors`、`aliases` 即可；仓库位于域名根目录，或路径与其他发行版重叠时，再配置 `host_pattern`。可参考本仓库的 `config/distributions.yaml` 和 [Proxmox 示例](../examples/proxmox)。

### 添加 apt-proxy 未内置的发行版

内置的五个发行版并不是上限。只要把某一项的 `type` 填成 `1`–`5` 之外的数字，它就
会被注册成一个新的发行版：拥有独立的镜像列表、独立的测速与独立的 rewriter，和内置
发行版完全一样。全程不需要改代码或重新编译。

以 Deepin 为例，缓存在 `/deepin/...` 下：

```yaml
distributions:
  - id: deepin
    name: Deepin
    type: 6
    url_pattern: "/deepin/(.+)$"
    benchmark_url: "dists/apricot/main/binary-amd64/Release"
    cache_rules:
      - pattern: "deb$"
        cache_control: "max-age=100000"
        rewrite: true
      - pattern: "(InRelease|Release(\\.gpg)?)$"
        cache_control: "max-age=3600"
        rewrite: true
      # 兜底规则必须放最后：apt 还会拉取软件包索引
      # （Packages.xz、by-hash/... 等），未命中任何规则的路径会返回 404。
      - pattern: ".*"
        cache_control: "max-age=3600"
        rewrite: true
    mirrors:
      official:
        - "community-packages.deepin.com/deepin/"
```

```text
deb http://apt-proxy.example:3142/deepin apricot main contrib non-free
```

如果仓库直接位于域名根目录，路径里没有前缀供 `url_pattern` 匹配，就改用
`host_pattern` 来识别。以 Armbian 为例，它的 `sources.list` 写法是
`deb http://apt.armbian.com <suite> main`：

```yaml
distributions:
  - id: armbian
    name: Armbian
    type: 7
    url_pattern: "/armbian/(.+)$"
    host_pattern: "^apt\\.armbian\\.com(:\\d+)?$"
    benchmark_url: "dists/bookworm/main/binary-arm64/Release"
    cache_rules:
      - pattern: "deb$"
        cache_control: "max-age=100000"
        rewrite: true
      - pattern: "(InRelease|Release(\\.gpg)?)$"
        cache_control: "max-age=3600"
        rewrite: true
      # 兜底规则必须放最后：apt 还会拉取软件包索引
      # （Packages.xz、by-hash/... 等），未命中任何规则的路径会返回 404。
      - pattern: ".*"
        cache_control: "max-age=3600"
        rewrite: true
    mirrors:
      official:
        - "mirrors.tuna.tsinghua.edu.cn/armbian/"
```

只要客户端带着 `Host: apt.armbian.com` 访问 apt-proxy（设置 `http_proxy` 即可
自动做到），`/dists/<suite>/...` 这类请求就会落到配置好的镜像上。

上面三种形态（Deepin、Armbian、Arch Linux）合在一个文件里的可运行版本，连同各自的
客户端配置，见 [`examples/custom-distros/`](../examples/custom-distros)。

几条能省掉一轮排查的提示：

- `type` 请跨重载保持稳定 —— 镜像选择与 rewriter 状态都以它为键。
- `benchmark_url` 必须是镜像列表里每个站点都存在的小文件，测速时会实际请求它。
- `cache_rules` 按顺序匹配，命中第一条即停，而且**未命中任何规则的路径会直接返回 `404`**，不会透传。`apt update` 除了 `InRelease` 还会拉取软件包索引（`Packages.xz`、`by-hash/...`），所以除非你刻意只想代理特定类型的文件，否则请以兜底规则 `".*"` 结尾。
- 以 `--mode=all`（默认值）运行。`--mode` 只接受内置发行版的名字，自定义发行版在 `all` 模式下才会启用。
- 只有匹配 `url_pattern`（或 `host_pattern`）的请求才会被代理，其余仍然返回 `404`。
- 文件必须放在 apt-proxy 会去找的位置：上面列出的查找路径之一，或用 `--distributions-config` / `APT_PROXY_DISTRIBUTIONS_CONFIG` 指定。服务端根本没加载的文件，等于这条配置静默失效。启动日志会打印实际生效的文件和所有已注册的发行版（`distributions registered config=... distributions=[...]`），是确认配置是否生效最快的方式。

### 自定义镜像选择

默认情况下，APT Proxy 会自动测试可用镜像并选择最快的一个。但如果需要，你可以指定自定义镜像。

**使用完整 URL：**

```bash
# 同时缓存多个发行版
./apt-proxy \
  --ubuntu=https://mirrors.tuna.tsinghua.edu.cn/ubuntu/ \
  --debian=https://mirrors.tuna.tsinghua.edu.cn/debian/

# 仅缓存 Ubuntu 软件包（减少内存占用）
./apt-proxy --mode=ubuntu --ubuntu=https://mirrors.tuna.tsinghua.edu.cn/ubuntu/

# 仅缓存 Debian 软件包
./apt-proxy --mode=debian --debian=https://mirrors.tuna.tsinghua.edu.cn/debian/
```

**使用镜像快捷方式：**

为了方便，你可以使用预定义的快捷方式代替完整 URL：

```bash
./apt-proxy --ubuntu=cn:tsinghua --debian=cn:163
```

**可用的快捷方式：**

- `cn:tsinghua` - 清华大学镜像
- `cn:ustc` - 中科大镜像
- `cn:163` - 网易镜像
- `cn:aliyun` - 阿里云镜像
- `cn:huaweicloud` - 华为云镜像
- `cn:tencent` - 腾讯云镜像

启动日志会通过 `using specified mirror` 消息记录显式选择的镜像，
并带上发行版和镜像 URL 字段。

### 缓存第三方软件源（`passthrough`）

apt-proxy 只代理它配置过的发行版，其余一律 `404` —— 这是刻意的，它不是开放转发
代理。但人们实际安装软件的来源不止发行版：Launchpad PPA、厂商源、内部源。把这些
源站写进白名单，apt-proxy 就会原样抓取并缓存，不做任何改写：

```bash
./apt-proxy --passthrough=ppa.launchpad.net,https://download.docker.com
```

```yaml
# apt-proxy.yaml
passthrough:
  - ppa.launchpad.net
  - https://download.docker.com
  - archive.internal.example:8080
```

环境变量 `APT_PROXY_PASSTHROUGH` 使用同样的逗号分隔格式。

**客户端配置。** 直通只对「把 apt-proxy 当作 HTTP 代理」的客户端生效，因为只有这种
方式才会在请求里点名源站：

```bash
http_proxy=http://apt-proxy.example:3142 apt-get update
```

`sources.list` 条目保持指向源站即可。URL 前缀形式
（`deb http://apt-proxy.example:3142/<host>/...`）寻址的是 apt-proxy 自己，没有
点名任何源站，因此不会触发直通。

**即使软件源本身只提供 HTTPS，条目也必须写成 `http://`：**

```text
deb http://download.docker.com/linux/ubuntu jammy stable
```

```yaml
passthrough:
  - https://download.docker.com   # 由 apt-proxy 负责把上游那一跳升级为 TLS
```

如果 `sources.list` 里写 `https://`，apt 会向代理发 `CONNECT host:443` 建立隧道。
隧道是端到端加密的，apt-proxy 只能转发字节，既读不到也缓存不了 —— 而缓存正是它存在
的全部意义。因此 apt-proxy 不响应 `CONNECT`：请把源写成 `http://`，让白名单里的
`https://` 条目（或 [`HTTPS///` 标记](troubleshooting.zh-CN.md#https-形式的-url-返回-403)）来承担 TLS 上游。
客户端到 apt-proxy 这一跳是你自己网络内的明文 HTTP，而 apt 无论走什么传输都会校验
软件源签名。

**条目写法：**

| 写法 | 含义 |
|------|------|
| `ppa.launchpad.net` | 沿用客户端的协议，仅匹配默认端口 |
| `https://download.docker.com` | 上游请求强制升级为 TLS |
| `archive.example:8080` | 只匹配该端口 |

不带端口的条目只匹配 80 和 443 —— 被放行的软件源主机，不应该顺带把那台机器上其它
监听的服务也暴露出去。

**它不做的事：**

- 不支持通配符。请逐个写明源站；写错的条目会在启动时直接报错，而不是悄悄放行多于
  或少于你所写的范围。
- 环回地址、私有地址、链路本地地址和 `localhost` 不允许作为条目。但**域名**即使
  解析到内网也会被接受 —— 白名单是安全边界，不是网络层管控。只把你愿意让这个代理
  的任意客户端访问的源站写进去。
- 只放行 `GET` 和 `HEAD`。apt 只读取，不写入。
- 缓存时长由源站决定：apt-proxy 清楚发行版的索引和软件包该缓存多久，但对第三方源
  一无所知，因此原样沿用它的 `Cache-Control`。
- 发行版路由优先。若白名单里的主机同时也被某个已配置的发行版覆盖，则按该发行版的
  镜像和缓存规则处理。

启动日志会打印生效的白名单（`passthrough enabled for third-party origins`），
空列表或写错的条目因此是可见的，不会静默。

**apt-cacher-ng 的 `HTTPS///` 标记**是访问白名单源站的另一种方式，适合本来就按这种
写法配置的客户端：

```text
deb http://HTTPS///get.docker.com/linux/ubuntu jammy stable
```

两种写法都支持，源站同样走这份白名单，且上游请求一律走 TLS。参见
[`HTTPS///` 形式的 URL 返回 `403`](troubleshooting.zh-CN.md#https-形式的-url-返回-403)。

### 通过前置代理访问镜像站

apt-proxy 的出站连接遵循标准的代理环境变量。若机器无法直连镜像站，可以让它
通过已有的前置代理访问：

```bash
HTTP_PROXY=http://proxy.internal:3128 \
HTTPS_PROXY=http://proxy.internal:3128 \
NO_PROXY=10.0.0.0/8,.internal \
  ./apt-proxy
```

| 变量名 | 作用 |
|--------|------|
| `HTTP_PROXY` / `http_proxy` | 访问上游的 `http://` 请求所用的代理 |
| `HTTPS_PROXY` / `https_proxy` | 访问上游的 `https://` 请求所用的代理 |
| `NO_PROXY` / `no_proxy` | 逗号分隔的主机名、域名后缀（`.example.com`）和 CIDR，命中则不走代理 |

SOCKS5 前置代理同样可用，直接写成代理 URL 即可：

```bash
HTTPS_PROXY=socks5h://127.0.0.1:1080 ./apt-proxy
```

这些变量对软件包下载和镜像测速选优都生效，因此选出来的镜像与实际下载走的是同一
条链路。它们只影响 apt-proxy 自身到镜像站的连接；客户端仍然直连 apt-proxy，所以
不要把 apt-proxy 自己的地址写进 `HTTP_PROXY`。在 Docker 中用 `-e` 传入即可。

启用 `upstream_access.enabled: true` 后，仓库下载、镜像测速与 Ubuntu 镜像发现
会直接连接目标并忽略上述代理变量；需要前置代理时应关闭该策略。
具体检查范围与配置见[上游访问策略](../docs/upstream-access.md)。

## 仓库操作与下载策略

### 下载资源限制

未知长度下载默认限制单个暂存文件为 1 GiB，每个实例共享暂存预算为 4 GiB；上游获取总超时默认为 20 分钟。可在 YAML 中配置 `download.max_spool_mb`、`download.total_spool_mb` 和 `download.timeout_seconds`，零值使用默认限制。这组旧式限制只约束未知长度落盘；它同时计入下面的共享资源预算。详见[下载限制说明](../docs/download-limits.md)。

### 私有仓库请求

携带 `Authorization` 或 `Cookie` 的请求绕过共享缓存，带 `Set-Cookie` 的响应不会存储。认证请求指定仅使用缓存时返回 504，不访问上游。本版本启用新的缓存键命名空间，以隔离升级前可能包含私有内容的条目；升级后会有一次冷缓存，旧文件不会直接删除。详见[私有仓库隔离说明](../docs/private-repositories.md)。

### 软件包下载客户端访问控制

可在 YAML 中设置 `security.download_allow_cidrs` 和
`security.download_deny_cidrs`，按直接连接来源的 IPv4/IPv6 网段限制下载。
拒绝规则优先，检查在缓存读取和上游请求之前执行；默认两个列表均为空。
转发头不能授予下载权限。部署在反向代理之后时，需要在反向代理限制最终客户端。
运维和管理 API 路由沿用原有策略；修改下载规则后需要重启。
配置示例和适用范围见 [下载访问控制说明](../docs/download-access.md)。

### 下载上游连接防护

可在 YAML 中启用 `upstream_access.enabled: true`，限制软件下载、镜像测速和 Ubuntu
地理镜像发现的上游主机、
端口和解析地址。新连接检查全部 DNS 结果后直接连接已允许的 IP，保留 TLS 主机名
验证。私有仓库需要明确的 CIDR 例外；启用后的仓库连接不使用环境变量中的转发代理。
测速和发现请求的重定向及 HEAD→GET 回退同样受保护；启动和镜像刷新沿用实例策略。
修改后需要重启。配置示例及镜像测速等后台请求的适用边界见
[上游访问说明](../docs/upstream-access.md)。

### Debian 系源码包覆盖

Ubuntu、Ubuntu Ports 和 Debian 的源码池文件现可经配置的镜像代理并缓存，
包括 `.dsc`、原始/附加/原生源码归档、Debian 补丁归档、旧式 `.diff.gz`
及原始归档的 `.asc` 签名。规则仅匹配 Debian 风格源码池中的带版本文件，
保留上游缓存指令。已有自定义发行版 YAML 需要明确加入新规则。
支持范围和客户端验证职责见 [源码包覆盖说明](../docs/apt-source-packages.md)。

### Debian 系安装器文件

常用 netboot、hd-media、cdrom 启动文件及安装器校验清单现可经配置的镜像
代理并缓存。规则限定到 Debian 风格安装器 `images/` 或 `legacy-images/` 目录中的具体文件名，默认缓存
3600 秒；自定义 YAML 需显式补充新规则。支持范围及客户端校验职责见
[安装器文件覆盖说明](../docs/apt-installer-files.md)。

安装器目录中的常用 PXE/UEFI 资源也可代理并缓存：EFI 加载器、默认 PXELINUX
配置、启动菜单及 GRUB 模块。范围限定在安装器 netboot 目录内，不提供
DHCP/TFTP 服务或启动配置内容改写。

### Debian 系 AppStream 元数据

DEP-11 组件索引、内容 ID 索引及已知图标归档的直接路径现可使用镜像和缓存，
包括从 by-hash 回退到直接 URL 的客户端。规则限定在
`dists/<suite>/<component>/dep11/`，不开放任意 YAML、JSON 或归档下载。
支持范围见[AppStream 元数据](../docs/apt-appstream.md)。

### 完整仓库快照和离线访问

`apt-proxy repository` 可预取指定 suite、component、架构的 Packages 索引、
可选 Sources 索引及其全部引用对象，校验大小和 SHA256 后发布完整快照。
提供 verify、repair、export、import，以及无上游回退的离线 HTTP serve 命令。
原始签名由 APT 验证；选定的 xz 索引需要系统 xz，另有包含 xz 的可选容器镜像。
命令、支持范围和限制见[仓库快照说明](../docs/repository-offline.md)。

显式公共仓库身份与 Unix 共享磁盘协调：[配置和运行边界](../docs/shared-repository-cache.md)。

### 临时资源与缓存完整性

仓库下载默认限制单对象 1 GiB、每实例暂存总量 4 GiB、32 个上游活跃下载，
并为磁盘保留 128 MiB 可用空间。已声明长度的响应、HTTP 缓存副本和 S3 内存/落盘副本
同样参与准入；共享预算拒绝时返回 `503` 与 `Retry-After: 1`。
缓存副本在响应已成功送达后超出预算时跳过缓存发布。配置与边界见[资源限制](../docs/resource-limits.md)。

持久缓存默认使用 `cache.integrity: sha256`，命中前读取并校验完整响应体；
旧的未验证记录会被惰性重新下载，S3 命中也可能先传输整个对象。
可明确设置 `cache.integrity: none` 接受旧格式的性能权衡；启用 `shared_cache` 时仍强制完整性校验。
迁移与 I/O 成本见[缓存完整性](../docs/cache-integrity.md)。

### 运行时镜像健康与客户端验证

`mirror_health.enabled: true` 可为显式等价的公共 Debian 系仓库启用有界故障切换。
替代镜像必须与当前镜像已完整观察到的 Release/InRelease 指纹相同；未知、过期或不同的
快照以及私有请求不会切换。无自动回切或后台轮询，状态可在受现有管理 API 策略保护的
`GET /api/mirrors/status` 查看。配置见[运行时镜像健康](../docs/runtime-mirror-health.md)。

运行 `bash tests/apt-client/matrix.sh` 可执行五个真实 APT 客户端的本地签名仓库测试，
覆盖安装、源码、SHA256 by-hash、gzip 回退、范围请求、故障恢复和离线导入后的访问。
这些测试不表示所有公共镜像均已验证，详见[客户端测试](../docs/apt-client-tests.md)。
