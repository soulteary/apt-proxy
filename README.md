# Apt Proxy

A caching proxy specifically for apt package caching, also rewrites to the fastest local mirror. Built as a tiny docker image for easy deployment.

Built because [apt-cacher-ng](https://www.unix-ag.uni-kl.de/~bloch/acng/) is unreliable.

## Running via Go

```bash
go run apt-proxy.go
```

## Running in Docker for Development

```bash
docker build --rm --tag=apt-proxy-dev .
docker run -it --rm --publish=3142 --net host apt-proxy-dev
```

## Running from Docker

```
docker run -it --rm --publish=3142 --net host lox24/apt-proxy
```

## Debugging

```
http_proxy=http://192.168.33.1:3142 apt-get -o Debug::pkgProblemResolver=true -o Debug::Acquire::http=true update
http_proxy=http://192.168.33.1:3142 apt-get -o Debug::pkgProblemResolver=true -o Debug::Acquire::http=true install apache2
```