# apt-proxy examples

Each subdirectory is a self-contained example. Pick the one closest to your
target deployment and read its `README.md`.

| Example | Runnable                | What it shows                                                                 |
| ------- | ----------------------- | ----------------------------------------------------------------------------- |
| [`basic/`](basic/)                     | `docker compose up -d` | Smallest possible deployment; defaults only, in-container cache.            |
| [`specify-mirrors/`](specify-mirrors/) | `docker compose up -d` | Same as `basic/` but pins upstream Ubuntu/Debian mirrors via CLI flags.     |
| [`s3-otterio/`](s3-otterio/)           | `docker compose up -d` | Production-shaped: cache offloaded to an S3-compatible bucket (OtterIO here). |
| [`custom-distros/`](custom-distros/)   | `docker compose up -d` | Caches Deepin, Armbian and Arch Linux via `distributions.yaml` — no code change. |
| [`proxmox/`](proxmox/)                 | `docker compose up --build -d` | PBS Client via the original HTTP source or a unique local path; builds the checkout containing the #167 routing fix. |
| [`config-template/`](config-template/) | not directly runnable  | Fully-commented `apt-proxy.yaml` reference. Copy & trim to your needs.      |

## Picking an example

- Just kicking the tires on a laptop? Start with [`basic/`](basic/).
- Want to pin a fast mirror (e.g. Tsinghua/USTC)? See [`specify-mirrors/`](specify-mirrors/).
- Need durable object storage? See [`s3-otterio/`](s3-otterio/) — it also works
  against other S3-compatible providers with their connection settings. Give
  independently running instances separate bucket prefixes; S3 does not provide
  shared publication or miss-coordination guarantees. For coordinated shared
  Unix disk storage and verified by-hash identities, see
  [shared repository cache](../docs/shared-repository-cache.md).
- Need a distribution apt-proxy does not ship (Deepin, Armbian, Arch, an
  internal archive)? See [`custom-distros/`](custom-distros/).
- Need Proxmox Backup Client, whose original path overlaps Debian's
  `/debian/` route? See [`proxmox/`](proxmox/) for host-and-path selection and
  the version requirement.
- Need to know what every config knob does? Read
  [`config-template/apt-proxy.yaml`](config-template/apt-proxy.yaml).

## Verifying any example

After starting one of the Compose examples, it exposes the proxy on
`http://127.0.0.1:3142`:

```bash
curl -I http://127.0.0.1:3142/healthz

curl -x http://127.0.0.1:3142 \
     http://archive.ubuntu.com/ubuntu/dists/noble/Release
```
