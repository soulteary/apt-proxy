# apt-proxy + OtterIO example

A minimal `docker-compose` setup that runs `apt-proxy` with its cache stored
in a colocated [OtterIO](https://github.com/soulteary/otterio) bucket
instead of a local directory.

> **About OtterIO**: an independent, community-maintained fork of the
> Apache-2.0 era MinIO codebase. It exposes the same S3 API and is a
> drop-in object-storage backend for any S3 client. This example previously
> shipped with MinIO; the OtterIO image is published at
> [`soulteary/otterio`](https://hub.docker.com/r/soulteary/otterio) on Docker
> Hub and `ghcr.io/soulteary/otterio` on GitHub Container Registry.

The same compose file works against any other S3-compatible service: just
swap the `apt-proxy` env vars (`APT_PROXY_S3_ENDPOINT`, etc.) with your
provider credentials.

## Quick start

```bash
cd examples/s3-otterio
docker compose up -d
# wait for the apt-proxy service to log "listening on :3142"

# verify the proxy is up
curl -I http://127.0.0.1:3142/healthz

# fetch something through the proxy
curl -x http://127.0.0.1:3142 \
     http://archive.ubuntu.com/ubuntu/dists/noble/Release
```

The OtterIO web console is exposed on `http://127.0.0.1:9001`
(user/password: `otterioadmin` / `otterioadmin`). Open the `apt-proxy` bucket
and you'll see `apt-proxy/body/v1/...` and `apt-proxy/header/v1/...` keys
appear as you proxy more requests.

## Files

- `docker-compose.yaml` — runs OtterIO + a one-shot `aws-cli` initializer +
  `apt-proxy` itself. The initializer creates the `apt-proxy` bucket if it
  doesn't already exist.
- `apt-proxy.yaml` — example apt-proxy YAML config equivalent to the env
  vars used in the compose file. Mount it via
  `-v $(pwd)/apt-proxy.yaml:/etc/apt-proxy.yaml --config /etc/apt-proxy.yaml`
  if you'd rather configure the proxy from a file.

## Why OtterIO instead of MinIO?

OtterIO tracks the **last Apache-2.0 release of MinIO** and continues to
publish the project under that license. If you previously used the MinIO
image here, the migration is mechanical: rename the env vars
(`MINIO_ROOT_USER` → `OTTERIO_ROOT_USER`,
`MINIO_ROOT_PASSWORD` → `OTTERIO_ROOT_PASSWORD`) and update the health-check
path (`/minio/health/live` → `/otterio/health/live`). The S3 API surface
that apt-proxy talks to is unchanged.

## Pointing at a non-OtterIO endpoint

Replace the `apt-proxy` service's environment block with your provider:

| Provider         | `APT_PROXY_S3_ENDPOINT`                  | `USE_SSL` | `USE_PATH_STYLE` |
| ---------------- | ---------------------------------------- | --------- | ---------------- |
| AWS S3           | `s3.us-east-1.amazonaws.com`             | `true`    | `false`          |
| Cloudflare R2    | `<account>.r2.cloudflarestorage.com`     | `true`    | `false`          |
| Backblaze B2     | `s3.us-west-002.backblazeb2.com`         | `true`    | `false`          |
| Aliyun OSS       | `oss-cn-hangzhou.aliyuncs.com`           | `true`    | `false`          |
| Tencent COS      | `cos.ap-shanghai.myqcloud.com`           | `true`    | `false`          |
| Ceph RGW         | `rgw.example.com`                        | depends   | `true`           |
| MinIO (legacy)   | `minio:9000`                             | `false`   | `true`           |
