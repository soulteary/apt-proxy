# APT Proxy

[![Security Scan](https://github.com/soulteary/apt-proxy/actions/workflows/scan.yml/badge.svg)](https://github.com/soulteary/apt-proxy/actions/workflows/scan.yml) [![Release](https://github.com/soulteary/apt-proxy/actions/workflows/release.yaml/badge.svg)](https://github.com/soulteary/apt-proxy/actions/workflows/release.yaml) [![goreportcard](https://img.shields.io/badge/go%20report-A+-brightgreen.svg?style=flat)](https://goreportcard.com/report/github.com/soulteary/apt-proxy) [![Docker Image](https://img.shields.io/docker/pulls/soulteary/apt-proxy.svg)](https://hub.docker.com/r/soulteary/apt-proxy)

<p style="text-align: center;">
  <a href="README.md">ENGLISH</a> | <a href="README_CN.md"  target="_blank">中文文档</a>
</p>

<img src="example/assets/logo.png" width="64"/>

> A lightweight **APT Cache Proxy** - just over 2MB in size!

<img src="example/assets/preview.png" width="600"/>

## Overview

APT Proxy is a lightweight, high-performance caching proxy for package managers. It accelerates package downloads by caching frequently used packages locally, dramatically reducing download times for subsequent installations. Whether you're managing multiple servers, building Docker images, or working in bandwidth-constrained environments, APT Proxy helps you save time and bandwidth.

### Key Features

- **Multi-Distribution Support**: Works with APT (Ubuntu/Debian), YUM (CentOS), and APK (Alpine Linux)
- **Lightweight**: Binary size is just over 2MB - minimal resource footprint
- **Smart Mirror Selection**: Automatically benchmarks and selects the fastest mirror
- **Docker-Ready**: Seamlessly integrates with Docker containers and build processes
- **Drop-in Replacement**: Compatible with [apt-cacher-ng](https://www.unix-ag.uni-kl.de/~bloch/acng/) configurations
- **Zero Configuration**: Works out of the box with sensible defaults

## Supported Platforms

- Linux: x86_64 / x86_32 / Ubuntu ARM64v8
- ARM: ARM64v8 / ARM32v6 / ARM32v7
- macOS: x86_64 / Apple Silicon (ARM64v8)

## Quick Start

### Installation

Download the latest release for your platform from the [releases page](https://github.com/soulteary/apt-proxy/releases), or use Docker:

```bash
docker pull soulteary/apt-proxy
```

### Running APT Proxy

Simply run the binary - no configuration required:

```bash
./apt-proxy
```

You should see output similar to:

```
2022/06/12 16:15:40 starting apt-proxy
2022/06/12 16:15:41 Starting benchmark for mirrors
2022/06/12 16:15:41 Finished benchmarking mirrors
2022/06/12 16:15:41 using fastest mirror https://mirrors.company.ltd/ubuntu/
2022/06/12 16:15:41 proxy listening on 0.0.0.0:3142
2022/06/12 16:15:41 server started successfully 🚀
```

The proxy is now running and ready to cache packages. By default, it listens on `0.0.0.0:3142` and automatically selects the fastest mirror for your location.

## Usage Examples

### Ubuntu / Debian

Configure your system to use the proxy by setting the `http_proxy` environment variable:

```bash
# Update package lists (first run will download and cache)
http_proxy=http://your-domain-or-ip-address:3142 \
  apt-get -o pkgProblemResolver=true -o Acquire::http=true update

# Install packages (subsequent installs will use cached packages)
http_proxy=http://your-domain-or-ip-address:3142 \
  apt-get -o pkgProblemResolver=true -o Acquire::http=true install vim -y
```

**Tip**: For convenience, you can export the proxy settings in your shell:

```bash
export http_proxy=http://your-domain-or-ip-address:3142
apt-get update
apt-get install vim -y
```

After the first download, all subsequent package operations will be significantly faster as packages are served from the local cache.

### CentOS

APT Proxy works with YUM repositories. Configure your CentOS system to use the proxy:

**For CentOS 7:**

```bash
# Configure repository to use proxy
cat /etc/yum.repos.d/CentOS-Base.repo | \
  sed -e s/mirrorlist.*$// \
      -e s/#baseurl/baseurl/ \
      -e s#http://mirror.centos.org#http://your-domain-or-ip-address:3142# | \
  tee /etc/yum.repos.d/CentOS-Base.repo

# Verify configuration
yum update
```

**For CentOS 8:**

```bash
# Update all CentOS repositories to use proxy
sed -i -e "s#mirror.centos.org#http://your-domain-or-ip-address:3142#g" \
       -e "s/#baseurl/baseurl/" \
       -e "s#\$releasever/#8-stream/#" \
       /etc/yum.repos.d/CentOS-*

# Verify configuration
yum update
```

### Alpine Linux

Configure Alpine's APK package manager to use the proxy:

```bash
# Update repositories to use proxy
cat /etc/apk/repositories | \
  sed -e s#https://.*.alpinelinux.org#http://your-domain-or-ip-address:3142# | \
  tee /etc/apk/repositories

# Verify configuration
apk update
```

## Advanced Configuration

### Custom Mirror Selection

By default, APT Proxy automatically benchmarks available mirrors and selects the fastest one. However, you can specify custom mirrors if needed.

**Using Full URLs:**

```bash
# Cache multiple distributions
./apt-proxy \
  --ubuntu=https://mirrors.tuna.tsinghua.edu.cn/ubuntu/ \
  --debian=https://mirrors.tuna.tsinghua.edu.cn/debian/

# Cache only Ubuntu packages (reduces memory usage)
./apt-proxy --mode=ubuntu --ubuntu=https://mirrors.tuna.tsinghua.edu.cn/ubuntu/

# Cache only Debian packages
./apt-proxy --mode=debian --debian=https://mirrors.tuna.tsinghua.edu.cn/debian/
```

**Using Mirror Shortcuts:**

For convenience, you can use predefined shortcuts instead of full URLs:

```bash
./apt-proxy --ubuntu=cn:tsinghua --debian=cn:163
```

**Available Shortcuts:**

- `cn:tsinghua` - Tsinghua University Mirror
- `cn:ustc` - USTC Mirror
- `cn:163` - NetEase Mirror
- `cn:aliyun` - Alibaba Cloud Mirror
- `cn:huaweicloud` - Huawei Cloud Mirror
- `cn:tencent` - Tencent Cloud Mirror

Example output:

```
2022/06/15 10:55:26 starting apt-proxy
2022/06/15 10:55:26 using specified debian mirror https://mirrors.163.com/debian/
2022/06/15 10:55:26 using specified ubuntu mirror https://mirrors.tuna.tsinghua.edu.cn/ubuntu/
2022/06/15 10:55:26 proxy listening on 0.0.0.0:3142
2022/06/15 10:55:26 server started successfully 🚀
```

## Docker Integration

### Running APT Proxy in Docker

Deploy APT Proxy as a Docker container:

```bash
docker run -d \
  --name=apt-proxy \
  -p 3142:3142 \
  -v apt-proxy-cache:/app/.aptcache \
  soulteary/apt-proxy
```

The `-v apt-proxy-cache:/app/.aptcache` option persists the cache across container restarts.

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

### Docker Compose Example

See the [example directory](example/) for complete Docker Compose configurations.

## Configuration Options

View all available options:

```bash
./apt-proxy -h
```

**Available Options:**

| Option | Description | Default |
|--------|-------------|---------|
| `-host` | Network interface to bind to | `0.0.0.0` |
| `-port` | Port to listen on | `3142` |
| `-mode` | Distribution mode: `all`, `ubuntu`, `ubuntu-ports`, `debian`, `centos`, `alpine` | `all` |
| `-cachedir` | Directory to store cached packages | `./.aptcache` |
| `-ubuntu` | Ubuntu mirror URL or shortcut | (auto-select) |
| `-ubuntu-ports` | Ubuntu Ports mirror URL or shortcut | (auto-select) |
| `-debian` | Debian mirror URL or shortcut | (auto-select) |
| `-centos` | CentOS mirror URL or shortcut | (auto-select) |
| `-alpine` | Alpine mirror URL or shortcut | (auto-select) |
| `-debug` | Enable verbose debug logging | `false` |

**Example with Custom Configuration:**

```bash
./apt-proxy \
  --host=0.0.0.0 \
  --port=3142 \
  --cachedir=/var/cache/apt-proxy \
  --mode=ubuntu \
  --ubuntu=cn:tsinghua \
  --debug
```

## Development

### Building from Source

```bash
git clone https://github.com/soulteary/apt-proxy.git
cd apt-proxy
go build -o apt-proxy .
```

### Running Tests

```bash
# Run all tests with coverage
go test -cover ./...

# Generate detailed coverage report
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

### Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## Troubleshooting

### Debug Mode

Enable debug logging to troubleshoot issues:

```bash
./apt-proxy --debug
```

### Debugging Package Operations

For detailed debugging of package manager operations (Ubuntu/Debian):

```bash
# Enable verbose debugging
http_proxy=http://192.168.33.1:3142 \
  apt-get -o Debug::pkgProblemResolver=true \
          -o Debug::Acquire::http=true \
          update

http_proxy=http://192.168.33.1:3142 \
  apt-get -o Debug::pkgProblemResolver=true \
          -o Debug::Acquire::http=true \
          install apache2
```

### Common Issues

**Issue**: Packages not being cached
**Solution**: Ensure the proxy URL is correctly configured and accessible from your client machines.

**Issue**: Slow first-time downloads
**Solution**: This is expected - the first download populates the cache. Subsequent downloads will be faster.

**Issue**: Cache directory growing too large
**Solution**: The cache directory can be cleaned manually, or you can set a custom cache directory with `--cachedir`.

## License

This project is licensed under the [Apache License 2.0](https://github.com/soulteary/apt-proxy/blob/master/LICENSE).

## Acknowledgments

This project builds upon the excellent work of:

- [lox/apt-proxy](https://github.com/lox/apt-proxy) - Original APT proxy implementation
- [lox/httpcache](https://github.com/lox/httpcache) - HTTP caching library (MIT License)
- [djherbis/stream](https://github.com/djherbis/stream) - Stream handling library (MIT License)
- [rainycape/vfs](https://github.com/rainycape/vfs) - Virtual filesystem library (Mozilla Public License 2.0)

## Support

- **Issues**: [GitHub Issues](https://github.com/soulteary/apt-proxy/issues)
- **Discussions**: [GitHub Discussions](https://github.com/soulteary/apt-proxy/discussions)

---

Made with ❤️ by the APT Proxy community
