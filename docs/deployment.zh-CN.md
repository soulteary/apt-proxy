# 部署与容器缓存

[项目首页](../README_CN.md) · [English](deployment.md)

## Docker 集成

### 在 Docker 中运行 APT Proxy

将 APT Proxy 部署为 Docker 容器：

```bash
docker run -d \
  --name=apt-proxy \
  -p 3142:3142 \
  -v apt-proxy-cache:/var/cache/apt-proxy \
  soulteary/apt-proxy:v0.19.4
```

命名卷挂载到当前镜像实际使用的缓存路径，可在容器删除或重建后保留数据。
使用 bind mount 时，目录须允许镜像用户写入：发布镜像 UID 为 65532，源码构建镜像 UID 为 1000。

### 在 Docker 构建中使用 APT Proxy

加速 Docker 容器中的软件包安装：

```bash
# 启动容器（Ubuntu 或 Debian）
docker run --rm -it ubuntu

# 在容器内使用代理
http_proxy=http://host.docker.internal:3142 \
  apt-get -o Debug::pkgProblemResolver=true -o Acquire::http=true update

http_proxy=http://host.docker.internal:3142 \
  apt-get -o Debug::pkgProblemResolver=true -o Acquire::http=true install vim -y
```

**注意**：`host.docker.internal` 在 Docker Desktop 上有效。对于 Linux，请使用主机的 IP 地址或适当配置 Docker 网络。

构建 Docker 镜像时，可以通过构建参数传入代理：

```dockerfile
FROM ubuntu:24.04
ARG APT_PROXY
RUN http_proxy="$APT_PROXY" apt-get update \
 && http_proxy="$APT_PROXY" apt-get install -y --no-install-recommends vim \
 && rm -rf /var/lib/apt/lists/*
```

```bash
docker build --build-arg APT_PROXY=http://APT_PROXY_HOST:3142 -t my-app .
```

将 `APT_PROXY_HOST` 替换为构建容器能访问的地址；Docker Desktop 与 Linux 的主机名
访问方式仍按前述说明选择。

### Docker Compose 示例

[examples 目录](../examples/)包含 **六个**示例：

- [`basic/`](../examples/basic/)：最小部署。
- [`specify-mirrors/`](../examples/specify-mirrors/)：指定上游镜像。
- [`s3-otterio/`](../examples/s3-otterio/)：使用 OtterIO 的 S3 缓存。
- [`custom-distros/`](../examples/custom-distros/)：Deepin、Armbian 与 Arch 路由。
- [`proxmox/`](../examples/proxmox/)：本地构建的 PBS Client 部署。
- [`config-template/`](../examples/config-template/)：配置参考，非可直接运行的 Compose 部署。
