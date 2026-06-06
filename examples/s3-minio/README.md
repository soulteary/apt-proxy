# apt-proxy + MinIO example

A minimal `docker-compose` setup that runs `apt-proxy` with its cache stored
in a colocated [MinIO](https://min.io) bucket instead of a local directory.
The same compose file works against any other S3-compatible service: just
swap the `apt-proxy` env vars (`APT_PROXY_S3_ENDPOINT`, etc.) with your
provider credentials.

## Quick start

```bash
cd examples/s3-minio
docker compose up -d
# wait for the apt-proxy service to log "listening on :3142"

# verify the proxy is up
curl -I http://127.0.0.1:3142/healthz

# fetch something through the proxy
curl -x http://127.0.0.1:3142 \
     http://archive.ubuntu.com/ubuntu/dists/noble/Release
```

The MinIO web console is exposed on `http://127.0.0.1:9001`
(user/password: `minioadmin` / `minioadmin`). Open the `apt-proxy` bucket and
you'll see `apt-proxy/body/v1/...` and `apt-proxy/header/v1/...` keys appear
as you proxy more requests.

## Files

- `docker-compose.yaml` — runs MinIO + a one-shot `mc` initializer +
  `apt-proxy` itself.
- `apt-proxy.yaml` — example apt-proxy YAML config equivalent to the env
  vars used in the compose file. Mount it via
  `-v $(pwd)/apt-proxy.yaml:/etc/apt-proxy.yaml --config /etc/apt-proxy.yaml`
  if you'd rather configure the proxy from a file.

## Pointing at a non-MinIO endpoint

Replace the `apt-proxy` service's environment block with your provider:

| Provider         | `APT_PROXY_S3_ENDPOINT`                  | `USE_SSL` | `USE_PATH_STYLE` |
| ---------------- | ---------------------------------------- | --------- | ---------------- |
| AWS S3           | `s3.us-east-1.amazonaws.com`             | `true`    | `false`          |
| Cloudflare R2    | `<account>.r2.cloudflarestorage.com`     | `true`    | `false`          |
| Backblaze B2     | `s3.us-west-002.backblazeb2.com`         | `true`    | `false`          |
| Aliyun OSS       | `oss-cn-hangzhou.aliyuncs.com`           | `true`    | `false`          |
| Tencent COS      | `cos.ap-shanghai.myqcloud.com`           | `true`    | `false`          |
| Ceph RGW         | `rgw.example.com`                        | depends   | `true`           |
