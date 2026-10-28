# Deployment and container cache

[Project README](../README.md) · [中文](deployment.zh-CN.md)

## Docker Integration

### Running APT Proxy in Docker

Deploy APT Proxy as a Docker container:

```bash
docker run -d \
  --name=apt-proxy \
  -p 3142:3142 \
  -v apt-proxy-cache:/var/cache/apt-proxy \
  soulteary/apt-proxy:v0.19.4
```

The named volume mounts the cache at the path used by both current images.
It retains cache data when the container is removed or recreated; bind mounts
must be writable by the image user (UID 65532 in release images, UID 1000 in
source-build images).

### Using APT Proxy in Docker Builds

Accelerate package installation in your Docker containers:

```bash
# Start a container (Ubuntu or Debian)
docker run --rm -it ubuntu

# Inside the container, use the proxy
http_proxy=http://host.docker.internal:3142 \
  apt-get -o Debug::pkgProblemResolver=true -o Acquire::http=true update

http_proxy=http://host.docker.internal:3142 \
  apt-get -o Debug::pkgProblemResolver=true -o Acquire::http=true install vim -y
```

**Note**: `host.docker.internal` works on Docker Desktop. For Linux, use the host's IP address or configure Docker networking appropriately.

For a Docker image build, pass the proxy as a build argument:

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

Replace `APT_PROXY_HOST` with an address reachable from the build container;
the same Docker Desktop/Linux hostname considerations apply.

### Docker Compose Example

The [examples directory](../examples/) contains **six** examples:

- [`basic/`](../examples/basic/): Minimal deployment.
- [`specify-mirrors/`](../examples/specify-mirrors/): Explicit upstream mirrors.
- [`s3-otterio/`](../examples/s3-otterio/): S3-compatible cache with OtterIO.
- [`custom-distros/`](../examples/custom-distros/): Deepin, Armbian and Arch mappings.
- [`proxmox/`](../examples/proxmox/): Locally built PBS Client setup.
- [`config-template/`](../examples/config-template/): Configuration reference; not a runnable Compose stack.
