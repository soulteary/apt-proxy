# APT Proxy

Small and Reliable APT packages cache tool, supports both Ubuntu and Debian.

You can safely use it instead of [apt-cacher-ng](https://www.unix-ag.uni-kl.de/~bloch/acng/).

## (WIP) Usage

- Binaries
- Docker

### (WIP) Development

```bash
go run apt-proxy.go
```

## Ubuntu / Debian Debugging

```
http_proxy=http://192.168.33.1:3142 apt-get -o Debug::pkgProblemResolver=true -o Debug::Acquire::http=true update
http_proxy=http://192.168.33.1:3142 apt-get -o Debug::pkgProblemResolver=true -o Debug::Acquire::http=true install apache2
```

## Licenses, contains dependent software

- MIT: [lox/httpcache](https://github.com/lox/httpcache/blob/master/LICENSE)
- NOT FOUND: [lox/apt-proxy](https://github.com/lox/apt-proxy#readme)
- MIT: [djherbis/stream](https://github.com/djherbis/stream/blob/master/LICENSE)
- MPL 2.0 [rainycape/vfs](https://github.com/rainycape/vfs/blob/master/LICENSE)
- MIT: [stretchr/testify](https://github.com/stretchr/testify/blob/master/LICENSE)