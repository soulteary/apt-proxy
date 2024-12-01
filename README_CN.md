# APT Proxy / 轻量 APT 加速工具

[![Security Scan](https://github.com/soulteary/apt-proxy/actions/workflows/scan.yml/badge.svg)](https://github.com/soulteary/apt-proxy/actions/workflows/scan.yml) [![Release](https://github.com/soulteary/apt-proxy/actions/workflows/release.yaml/badge.svg)](https://github.com/soulteary/apt-proxy/actions/workflows/release.yaml) [![goreportcard](https://img.shields.io/badge/go%20report-A+-brightgreen.svg?style=flat)](https://goreportcard.com/report/github.com/soulteary/apt-proxy) [![Docker Image](https://img.shields.io/docker/pulls/soulteary/apt-proxy.svg)](https://hub.docker.com/r/soulteary/apt-proxy)


<p style="text-align: center;">
  <a href="README.md" target="_blank">ENGLISH</a> | <a href="README_CN.md">中文文档</a>
</p>

<img src="example/assets/logo.png" width="64"/>

> 一个轻量级的 APT 缓存代理 - 仅仅只有 2MB 大小！

<img src="example/assets/preview.png" width="600"/>

APT Proxy 是一个轻量级的缓存工具，用于 APT、YUM 和 APK 包（支持 Ubuntu、Debian、CentOS 和 Alpine Linux）。它可以无缝地与传统系统安装和 Docker 环境配合使用。

你也可以将它作为古老的 [apt-cacher-ng](https://www.unix-ag.uni-kl.de/~bloch/acng/) 安全可靠的替代品。

## 支持的平台

- Linux：x86_64 / x86_32 / Ubuntu ARM64v8
- ARM：ARM64v8 / ARM32v6 / ARM32v7
- macOS：x86_64 / Apple Silicon (ARM64v8)

## 快速开始

直接运行二进制文件：

```bash
./apt-proxy

2022/06/12 16:15:40 running apt-proxy
2022/06/12 16:15:41 Start benchmarking mirrors
2022/06/12 16:15:41 Finished benchmarking mirrors
2022/06/12 16:15:41 using fastest mirror https://mirrors.company.ltd/ubuntu/
2022/06/12 16:15:41 proxy listening on 0.0.0.0:3142
```

当你看到类似上面的日志时，一个带有缓存功能的 APT 代理服务就启动完毕了。

## Ubuntu / Debian 支持

要在 `apt-get` 命令中使用代理，请在命令前添加代理设置：

```bash
# 使用 apt-proxy 更新包列表
http_proxy=http://your-domain-or-ip-address:3142 apt-get -o pkgProblemResolver=true -o Acquire::http=true update 
# 使用 apt-proxy 安装包
http_proxy=http://your-domain-or-ip-address:3142 apt-get -o pkgProblemResolver=true -o Acquire::http=true install vim -y
```

由于包被本地缓存，后续的包操作将会显著加快。

## CentOS 支持

对于 CentOS 7：

```bash
cat /etc/yum.repos.d/CentOS-Base.repo | sed -e s/mirrorlist.*$// | sed -e s/#baseurl/baseurl/ | sed -e s#http://mirror.centos.org#http://your-domain-or-ip-address:3142# | tee /etc/yum.repos.d/CentOS-Base.repo
```

对于 CentOS 8：

```bash
sed -i -e"s#mirror.centos.org#http://your-domain-or-ip-address:3142#g" /etc/yum.repos.d/CentOS-*
sed -i -e"s/#baseurl/baseurl/" /etc/yum.repos.d/CentOS-*
sed -i -e"s#\$releasever/#8-stream/#" /etc/yum.repos.d/CentOS-*
```

运行 `yum update` 验证配置。

## Alpine Linux 支持

APT Proxy 也可以加速 Alpine Linux 的包下载：

```bash
cat /etc/apk/repositories | sed -e s#https://.*.alpinelinux.org#http://your-domain-or-ip-address:3142# | tee /etc/apk/repositories
```

运行 `apk update` 验证配置。

## 镜像配置

你可以通过两种方式指定镜像：

使用完整 URL：

```bash
# 同时缓存 Ubuntu 和 Debian 包
./apt-proxy --ubuntu=https://mirrors.tuna.tsinghua.edu.cn/ubuntu/ --debian=https://mirrors.tuna.tsinghua.edu.cn/debian/
# 仅缓存 Ubuntu 包
./apt-proxy --mode=ubuntu --ubuntu=https://mirrors.tuna.tsinghua.edu.cn/ubuntu/
# 仅缓存 Debian 包
./apt-proxy --mode=debian --debian=https://mirrors.tuna.tsinghua.edu.cn/debian/
```

使用快捷方式：

```bash
go run apt-proxy.go --ubuntu=cn:tsinghua --debian=cn:163
2022/06/15 10:55:26 running apt-proxy
2022/06/15 10:55:26 using specify debian mirror https://mirrors.163.com/debian/
2022/06/15 10:55:26 using specify ubuntu mirror https://mirrors.tuna.tsinghua.edu.cn/ubuntu/
2022/06/15 10:55:26 proxy listening on 0.0.0.0:3142
```

可用的快捷方式：

- cn:tsinghua
- cn:ustc
- cn:163
- cn:aliyun
- cn:huaweicloud
- cn:tencent 等等...

## Docker 集成

要加速 Docker 容器中的包安装：

```bash
# 启动容器（Ubuntu 或 Debian）
docker run --rm -it ubuntu
# 或
docker run --rm -it debian

# 使用代理安装包
http_proxy=http://host.docker.internal:3142 apt-get -o Debug::pkgProblemResolver=true -o Debug::Acquire::http=true update && \
http_proxy=http://host.docker.internal:3142 apt-get -o Debug::pkgProblemResolver=true -o Debug::Acquire::http=true install vim -y
```

## Docker 部署

<img src="example/assets/dockerhub.png" width="600"/>

使用单个命令部署：

```bash
docker run -d --name=apt-proxy -p 3142:3142 soulteary/apt-proxy
```

## 配置选项

我们可以通过使用 `-h` 参数来查看程序支持的所有参数：

```bash
./apt-proxy -h

用法说明：
  -alpine string
        用于获取包的 alpine 镜像
  -cachedir string
        存储缓存数据的目录 (默认 "./.aptcache")
  -centos string
        用于获取包的 centos 镜像
  -debian string
        用于获取包的 debian 镜像
  -debug
        是否输出调试日志
  -host string
        绑定的主机地址 (默认 "0.0.0.0")
  -mode all
        选择要缓存的系统模式：all / `ubuntu` / `ubuntu-ports` / `debian` / `centos` / `alpine` (默认 "all")
  -port string
        绑定的端口 (默认 "3142")
  -ubuntu string
        用于获取包的 ubuntu 镜像
```

## 开发

运行测试：

```bash
# 运行带覆盖率报告的测试
go test -cover ./...

# 生成并查看详细的覆盖率报告
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```


## 调试包操作

对于 Ubuntu/Debian：

```bash
http_proxy=http://192.168.33.1:3142 apt-get -o Debug::pkgProblemResolver=true -o Debug::Acquire::http=true update
http_proxy=http://192.168.33.1:3142 apt-get -o Debug::pkgProblemResolver=true -o Debug::Acquire::http=true install apache2
```

## 开源协议

这个项目基于 [Apache License 2.0](https://github.com/soulteary/apt-proxy/blob/master/LICENSE)。

## 依赖组件

- 未指定协议
    - [lox/apt-proxy](https://github.com/lox/apt-proxy#readme)
- MIT License
    - [lox/httpcache](https://github.com/lox/httpcache/blob/master/LICENSE)
    - [djherbis/stream](https://github.com/djherbis/stream/blob/master/LICENSE)
- Mozilla Public License 2.0
    - [rainycape/vfs](https://github.com/rainycape/vfs/blob/master/LICENSE)
