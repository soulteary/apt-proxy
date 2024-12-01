FROM golang:1.23.3-alpine3.19 as builder
RUN apk update && \
    apk add ca-certificates
ENV TZ=Asia/Shanghai
RUN apk add tzdata && \
    cp /usr/share/zoneinfo/$TZ /etc/localtime && echo $TZ > /etc/timezone
WORKDIR /build
ENV GO111MODULE=on
ENV CGO_ENABLED=0
ENV GOPROXY=https://goproxy.cn
COPY go.mod .
COPY go.sum .
RUN go mod download
COPY . .
RUN go build -ldflags "-w -s"

FROM alpine:3.16.0
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
ENV TZ=Asia/Shanghai
COPY --from=builder /etc/localtime /etc/localtime
COPY --from=builder /etc/timezone /etc/timezone
COPY --from=builder /build/apt-proxy /usr/bin/apt-proxy
EXPOSE 3142/tcp
ENTRYPOINT ["/usr/bin/apt-proxy"]