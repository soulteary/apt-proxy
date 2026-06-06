# basic

The smallest viable apt-proxy deployment: one container, default settings,
local in-container cache. Useful for trying apt-proxy out on a single host.

## Run

```bash
cd examples/basic
docker compose up -d
```

## Verify

```bash
curl -I http://127.0.0.1:3142/healthz

curl -x http://127.0.0.1:3142 \
     http://archive.ubuntu.com/ubuntu/dists/noble/Release
```

## Use it from clients

Point apt at the proxy on the host (Linux client example):

```bash
echo 'Acquire::http::Proxy "http://<host-ip>:3142";' \
  | sudo tee /etc/apt/apt.conf.d/00proxy
sudo apt-get update
```

## Notes

- Cache lives **inside the container** in this example. To persist across
  restarts, mount a volume at `/var/cache/apt-proxy` or switch to the
  [`../s3-otterio/`](../s3-otterio/) example.
- Need to pin upstream mirrors? See [`../specify-mirrors/`](../specify-mirrors/).
