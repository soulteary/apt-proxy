FROM golang:1.26-alpine3.21 AS builder
RUN apk add --no-cache ca-certificates tzdata git
ARG TZ=Asia/Shanghai
ENV TZ=${TZ}
RUN cp /usr/share/zoneinfo/$TZ /etc/localtime && echo $TZ > /etc/timezone
WORKDIR /build
ENV GO111MODULE=on
ENV CGO_ENABLED=0
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
ARG COMMIT=none
ARG DATE=unknown
RUN go build -trimpath \
    -ldflags "-s -w -X 'main.version=${VERSION}' -X 'main.commit=${COMMIT}' -X 'main.date=${DATE}'" \
    -o /out/apt-proxy ./cmd/apt-proxy

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata wget && \
    addgroup -g 1000 aptproxy && \
    adduser -D -u 1000 -G aptproxy aptproxy
ARG TZ=Asia/Shanghai
ENV TZ=${TZ}
RUN cp /usr/share/zoneinfo/$TZ /etc/localtime && echo $TZ > /etc/timezone
COPY --from=builder /out/apt-proxy /usr/bin/apt-proxy
RUN mkdir -p /var/cache/apt-proxy && chown -R aptproxy:aptproxy /var/cache/apt-proxy
USER aptproxy:aptproxy
WORKDIR /var/cache/apt-proxy
EXPOSE 3142/tcp
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget --no-verbose --tries=1 --spider http://localhost:3142/healthz || exit 1
ENTRYPOINT ["/usr/bin/apt-proxy"]
CMD ["--cachedir", "/var/cache/apt-proxy"]