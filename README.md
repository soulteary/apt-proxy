# APT Proxy

[![Security Scan](https://github.com/soulteary/apt-proxy/actions/workflows/scan.yml/badge.svg)](https://github.com/soulteary/apt-proxy/actions/workflows/scan.yml) [![Release](https://github.com/soulteary/apt-proxy/actions/workflows/release.yaml/badge.svg)](https://github.com/soulteary/apt-proxy/actions/workflows/release.yaml) [![goreportcard](https://img.shields.io/badge/go%20report-A+-brightgreen.svg?style=flat)](https://goreportcard.com/report/github.com/soulteary/apt-proxy) [![Docker Image](https://img.shields.io/docker/pulls/soulteary/apt-proxy.svg)](https://hub.docker.com/r/soulteary/apt-proxy)


<p style="text-align: center;">
  <a href="README.md">ENGLISH</a> | <a href="README_CN.md"  target="_blank">中文文档</a>
</p>

<img src="example/assets/logo.png" width="64"/>

> A lightweight **APT Cache Proxy** - just over 2MB in size!

<img src="example/assets/preview.png" width="600"/>

APT Proxy is a lightweight and reliable caching tool for **APT, YUM, and APK packages (supporting Ubuntu, Debian, CentOS, and Alpine Linux)**. It's designed to work seamlessly with both traditional system installations and Docker environments.

It serves as a drop-in replacement for [apt-cacher-ng](https://www.unix-ag.uni-kl.de/~bloch/acng/).

## Supported Platforms

- Linux: x86_64 / x86_32 / Ubuntu ARM64v8
- ARM: ARM64v8 / ARM32v6 / ARM32v7
- macOS: x86_64 / Apple Silicon (ARM64v8)

## Getting Started

Simply run the binary:

```bash
./apt-proxy

2022/06/12 16:15:40 running apt-proxy
2022/06/12 16:15:41 Start benchmarking mirrors
2022/06/12 16:15:41 Finished benchmarking mirrors
2022/06/12 16:15:41 using fastest mirror https://mirrors.company.ltd/ubuntu/
2022/06/12 16:15:41 proxy listening on 0.0.0.0:3142
```

To use the proxy with `apt-get` commands, prefix them with the proxy settings:

```bash
# Update package lists using apt-proxy
http_proxy=http://your-domain-or-ip-address:3142 apt-get -o pkgProblemResolver=true -o Acquire::http=true update 
# Install packages using apt-proxy
http_proxy=http://your-domain-or-ip-address:3142 apt-get -o pkgProblemResolver=true -o Acquire::http=true install vim -y
```

Subsequent package operations will be significantly faster as packages are cached locally.

## CentOS Support

While CentOS uses Yum instead of APT, APT-Proxy provides acceleration for both CentOS 7 and 8.

For CentOS 7:

```bash
cat /etc/yum.repos.d/CentOS-Base.repo | sed -e s/mirrorlist.*$// | sed -e s/#baseurl/baseurl/ | sed -e s#http://mirror.centos.org#http://your-domain-or-ip-address:3142# | tee /etc/yum.repos.d/CentOS-Base.repo
```

For CentOS 8:

```bash
sed -i -e "s#mirror.centos.org#http://your-domain-or-ip-address:3142#g" /etc/yum.repos.d/CentOS-*
sed -i -e "s/#baseurl/baseurl/" /etc/yum.repos.d/CentOS-*
sed -i -e "s#\$releasever/#8-stream/#" /etc/yum.repos.d/CentOS-*
```

Verify the configuration by running `yum update`.

## Alpine Linux Support

APT Proxy also accelerates package downloads for Alpine Linux:

```bash
cat /etc/apk/repositories | sed -e s#https://.*.alpinelinux.org#http://your-domain-or-ip-address:3142# | tee /etc/apk/repositories
```

Verify the configuration by running `apk update`.

## Mirror Configuration

You can specify mirrors in two ways:

Using Full URLs:

```bash
# Cache both Ubuntu and Debian packages
./apt-proxy --ubuntu=https://mirrors.tuna.tsinghua.edu.cn/ubuntu/ --debian=https://mirrors.tuna.tsinghua.edu.cn/debian/
# Cache Ubuntu packages only
./apt-proxy --mode=ubuntu --ubuntu=https://mirrors.tuna.tsinghua.edu.cn/ubuntu/
# Cache Debian packages only
./apt-proxy --mode=debian --debian=https://mirrors.tuna.tsinghua.edu.cn/debian/
```

Using Shortcuts:

```bash
go run apt-proxy.go --ubuntu=cn:tsinghua --debian=cn:163
2022/06/15 10:55:26 running apt-proxy
2022/06/15 10:55:26 using specify debian mirror https://mirrors.163.com/debian/
2022/06/15 10:55:26 using specify ubuntu mirror https://mirrors.tuna.tsinghua.edu.cn/ubuntu/
2022/06/15 10:55:26 proxy listening on 0.0.0.0:3142
```

Available shortcuts:

- cn:tsinghua
- cn:ustc
- cn:163
- cn:aliyun
- cn:huaweicloud
- cn:tencent and more...

## Docker Integration

To accelerate package installation in Docker containers:

```bash
# Start a container (Ubuntu or Debian)
docker run --rm -it ubuntu
# or
docker run --rm -it debian

# Install packages using the proxy
http_proxy=http://host.docker.internal:3142 apt-get -o Debug::pkgProblemResolver=true -o Debug::Acquire::http=true update && \
http_proxy=http://host.docker.internal:3142 apt-get -o Debug::pkgProblemResolver=true -o Debug::Acquire::http=true install vim -y
```

## Docker Deployment

<img src="example/assets/dockerhub.png" width="600"/>

Deploy with a single command:

```bash
docker run -d --name=apt-proxy -p 3142:3142 soulteary/apt-proxy
```

## Configuration Options

```bash
./apt-proxy -h

Usage of apt-proxy:
  -alpine string
    	the alpine mirror for fetching packages
  -cachedir string
    	the dir to store cache data in (default "./.aptcache")
  -centos string
    	the centos mirror for fetching packages
  -debian string
    	the debian mirror for fetching packages
  -debug
    	whether to output debugging logging
  -host string
    	the host to bind to (default "0.0.0.0")
  -mode all
    	select the mode of system to cache: all / `ubuntu` / `debian` / `centos` / `alpine` (default "all")
  -port string
    	the port to bind to (default "3142")
  -ubuntu string
    	the ubuntu mirror for fetching packages
```

## Development

Running Tests:

```bash
# Run tests with coverage reporting
go test -cover ./...

# Generate and view detailed coverage report
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

## Debugging Package Operations

For Ubuntu/Debian:

```bash
http_proxy=http://192.168.33.1:3142 apt-get -o Debug::pkgProblemResolver=true -o Debug::Acquire::http=true update
http_proxy=http://192.168.33.1:3142 apt-get -o Debug::pkgProblemResolver=true -o Debug::Acquire::http=true install apache2
```

## License

This project is licensed under the [Apache License 2.0](https://github.com/soulteary/apt-proxy/blob/master/LICENSE).

## Dependencies

- No License Specified
  - [lox/apt-proxy](https://github.com/lox/apt-proxy#readme)
- MIT License
    - [lox/httpcache](https://github.com/lox/httpcache/blob/master/LICENSE)
    - [djherbis/stream](https://github.com/djherbis/stream/blob/master/LICENSE)
- Mozilla Public License 2.0
    - [rainycape/vfs](https://github.com/rainycape/vfs/blob/master/LICENSE)
