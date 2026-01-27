FROM golang:1.23-alpine3.21 AS builder
RUN apk update && \
    apk add --no-cache ca-certificates tzdata
ARG TZ=Asia/Shanghai
ENV TZ=${TZ}
RUN cp /usr/share/zoneinfo/$TZ /etc/localtime && echo $TZ > /etc/timezone
WORKDIR /build
ENV GO111MODULE=on
ENV CGO_ENABLED=0
ENV GOPROXY=https://goproxy.cn
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -ldflags "-w -s"

FROM alpine:3.21
# Install ca-certificates and timezone data
RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -g 1000 aptproxy && \
    adduser -D -u 1000 -G aptproxy aptproxy
ARG TZ=Asia/Shanghai
ENV TZ=${TZ}
RUN cp /usr/share/zoneinfo/$TZ /etc/localtime && echo $TZ > /etc/timezone
COPY --from=builder /build/apt-proxy /usr/bin/apt-proxy
# Create cache directory with proper permissions
RUN mkdir -p /var/cache/apt-proxy && chown aptproxy:aptproxy /var/cache/apt-proxy
USER aptproxy
EXPOSE 3142/tcp
# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:3142/healthz || exit 1
ENTRYPOINT ["/usr/bin/apt-proxy"]
CMD ["--cachedir", "/var/cache/apt-proxy"]