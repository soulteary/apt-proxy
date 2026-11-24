FROM golang:1.27.4-alpine3.24@sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280 AS builder
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

FROM alpine:3.24.4@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf
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
