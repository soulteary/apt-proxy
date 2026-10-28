# 故障排除与历史升级

[项目首页](../README_CN.md) · [English](troubleshooting.md)

## 故障排除

### `HTTPS///` 形式的 URL 返回 `403`

apt-proxy 支持 apt-cacher-ng 的 `HTTPS///` 重写标记（`deb http://HTTPS///example.com/repo ...`）：它会抓取 `https://example.com/repo` 并缓存。前提是该源站已在[直通白名单](repository-configuration.zh-CN.md#缓存第三方软件源passthrough)中 —— 返回 `403` 正是表示还没加：

```bash
./apt-proxy --passthrough=get.docker.com
```

拒绝信息里会带上源站名和对应的配置项，照着加一条即可。标记 URL 的其它状态码：

| 状态码 | 含义 |
|--------|------|
| `403` | 源站不在白名单中 |
| `400` | 标记后面没有写源站（`.../HTTPS///` 后面是空的） |
| `405` | 不是 `GET` 或 `HEAD` |

apt-cacher-ng 文档里的两种写法都支持 —— `Host: HTTPS` 加路径里的源站，以及把标记嵌在指向 apt-proxy 自身地址的路径里 —— 且大小写不敏感。

### 升级到 v0.17.0 后路由发生变化

v0.17.0 之前，`distributions.yaml` **只有**在通过 `--distributions-config` /
`APT_PROXY_DISTRIBUTIONS_CONFIG` 显式指定路径时才会被读取。放在搜索路径上的文件
会被静默忽略，这类部署发送 SIGHUP 也不会重载任何东西。现在两者的行为与本文档
的描述一致 —— 参见[添加 apt-proxy 未内置的发行版](repository-configuration.zh-CN.md#添加-apt-proxy-未内置的发行版)。

这是一个修复，但升级时可见。放在 `./config/`、`./`、`/etc/apt-proxy/` 或
`~/.config/apt-proxy/` 的 `distributions.yaml` —— 可能是一次实验、从本仓库复制
的模板、或旧部署遗留的文件 —— 会在第一次以 v0.17.0 启动时生效，而你的命令行
没有任何改动。其中 `type` 为 `1`–`5` 的条目会**重新配置对应的内置发行版**，
镜像和 `url_pattern` 都可能随之改变。

启动日志会写明实际生效的文件以及注册了哪些发行版：

```text
config="config/distributions.yaml" distributions=["alpine","centos","debian","deepin","ubuntu","ubuntu-ports"]
```

`config="(built-in defaults)"` 表示没有任何外部配置生效 —— 这**不等于**没有
找到文件。文件存在但读取、解析或校验失败时会被整体拒绝，启动同样回退到内置
配置并打印这一行。区分两者要看紧挨着它上面的那条警告：

```text
failed to load distributions config; using built-in defaults  error="invalid distribution config for x: ..."
```

这条警告**只有**在「找到了文件但被拒绝」时才会出现，所以它在不在就是答案：
没有警告说明确实没找到文件；有警告说明你的文件就在那里但有问题，`error`
字段会说明原因。（当文件来自搜索路径而非显式指定时，这条警告不会写出文件名
—— 请按上面的顺序检查那四个路径。）

如果 `config=` 里出现了你没有预期的文件，把它移走或删除，或者显式指定你真正
想用的那一个。

### PPA 或厂商软件源返回 `404`

apt-proxy 只代理已配置的发行版。路径里**碰巧含有**发行版路径段，并不代表它是该发行版的镜像 —— 那是另一个仓库，会返回 `404`：

```text
/ppa.launchpad.net/deadsnakes/ppa/ubuntu/dists/jammy/InRelease   404
/download.docker.com/linux/ubuntu/dists/jammy/InRelease          404
```

更早的版本会匹配这类路径，并用该发行版自己的镜像来应答，于是一个 PPA 请求拿回来的是 **Ubuntu 主仓**同名套件的索引 —— `200`、看起来完全正常、内容却来自另一个仓库。现在的 `404` 取代了这种静默替换。

具体怎么处理，取决于客户端是以哪种方式接入 apt-proxy 的。

**把 apt-proxy 当作 APT 的代理**（`http_proxy=...` 或 `Acquire::http::Proxy`，也就是快速开始里的用法）：这时**所有**请求都会经过 apt-proxy，改 `sources.list` 条目没有任何作用 —— 请求照样会到达这里（只靠 `Host` 标识），照样返回 `404`。正确做法是让该主机绕过 apt-proxy：

```text
# /etc/apt/apt.conf.d/99-apt-proxy-bypass
Acquire::http::Proxy::ppa.launchpad.net "DIRECT";
Acquire::https::Proxy::ppa.launchpad.net "DIRECT";
```

**使用 URL 前缀形式**（`deb http://apt-proxy.example:3142/<host>/...`）：把该条目直接指向它的源站即可。

**两种方式都适用**：如果你希望 apt-proxy 缓存这个仓库而不是跳过它，把源站加进 `passthrough` 白名单即可 —— 参见[缓存第三方软件源](repository-configuration.zh-CN.md#缓存第三方软件源passthrough)：

```bash
./apt-proxy --passthrough=ppa.launchpad.net
```

只想原样缓存的话，这是最短路径。如果你还想让 apt-proxy 用自己的缓存规则、镜像列表和测速来管理它，则应建模成一个发行版 —— 参见[添加 apt-proxy 未内置的发行版](repository-configuration.zh-CN.md#添加-apt-proxy-未内置的发行版)；客户端处于代理模式时，该条目需要配上匹配源站域名的 `host_pattern`（`host_pattern: "^ppa\\.launchpad\\.net$"`），因为这类请求路径里没有前缀可匹配。

apt-cacher-ng 风格的主机名前缀不受影响：发行版路径段前只有单独一段主机名时（`/ftp.uni-kl.de/debian/...`）依然正常路由。

### 调试模式

启用调试日志来排查问题：

```bash
./apt-proxy --debug
```

### 调试包操作

对于 Ubuntu/Debian 包管理器操作的详细调试：

```bash
# 启用详细调试
http_proxy=http://192.168.33.1:3142 \
  apt-get -o Debug::pkgProblemResolver=true \
          -o Debug::Acquire::http=true \
          update

http_proxy=http://192.168.33.1:3142 \
  apt-get -o Debug::pkgProblemResolver=true \
          -o Debug::Acquire::http=true \
          install apache2
```

### 常见问题

**问题**：软件包没有被缓存
**解决方案**：确保代理 URL 配置正确，并且客户端机器可以访问。

**问题**：首次下载很慢
**解决方案**：这是预期行为 - 首次下载会填充缓存。后续下载会更快。

**问题**：重启之后缓存像是空的，所有东西又重新下载了一遍
**解决方案**：缓存条目以所选镜像的 URL 为键，而镜像选优在重启时会重新执行。用 `--ubuntu=…`、`--debian=…` 显式指定镜像即可让缓存键保持稳定。参见「缓存键与镜像选择」。

**问题**：缓存目录过大
**解决方案**：使用 `--cache-max-size` 配置缓存限制，或使用清理 API 端点。

### 升级到 v0.18.0

检查默认完整性校验、Vary 缓存迁移、下载额度、S3 暂存空间和共享磁盘维护方式后再升级。
详见[中文发布与升级说明](../docs/releases/v0.18.0.zh-CN.md)。
