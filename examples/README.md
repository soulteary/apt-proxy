# apt-proxy examples

Each subdirectory is a self-contained example. Pick the one closest to your
target deployment and read its `README.md`.

| Example | Runnable                | What it shows                                                                 |
| ------- | ----------------------- | ----------------------------------------------------------------------------- |
| [`basic/`](basic/)                     | `docker compose up -d` | Smallest possible deployment; defaults only, in-container cache.            |
| [`specify-mirrors/`](specify-mirrors/) | `docker compose up -d` | Same as `basic/` but pins upstream Ubuntu/Debian mirrors via CLI flags.     |
| [`s3-minio/`](s3-minio/)               | `docker compose up -d` | Production-shaped: cache offloaded to an S3-compatible bucket (MinIO here). |
| [`config-template/`](config-template/) | not directly runnable  | Fully-commented `apt-proxy.yaml` reference. Copy & trim to your needs.      |

## Picking an example

- Just kicking the tires on a laptop? Start with [`basic/`](basic/).
- Want to pin a fast mirror (e.g. Tsinghua/USTC)? See [`specify-mirrors/`](specify-mirrors/).
- Multi-host / shared cache / object storage? See [`s3-minio/`](s3-minio/) — it
  works against AWS S3, R2, B2, OSS, COS, Ceph RGW too; only env vars change.
- Need to know what every config knob does? Read
  [`config-template/apt-proxy.yaml`](config-template/apt-proxy.yaml).

## Verifying any example

After `docker compose up -d`, all examples expose the proxy on
`http://127.0.0.1:3142`:

```bash
curl -I http://127.0.0.1:3142/healthz

curl -x http://127.0.0.1:3142 \
     http://archive.ubuntu.com/ubuntu/dists/noble/Release
```
