# config-template

A fully-commented `apt-proxy.yaml` showing **every** supported configuration
key with defaults and inline notes. **Not** a directly-runnable example —
copy it, trim what you don't need, then point apt-proxy at it.

## Use it

Pick one of the locations apt-proxy auto-discovers:

- `./apt-proxy.yaml` (current working directory)
- `/etc/apt-proxy/apt-proxy.yaml` (system-wide)
- `~/.config/apt-proxy/apt-proxy.yaml` (per-user)
- `~/.apt-proxy.yaml` (per-user, legacy)

Or pass it explicitly:

```bash
apt-proxy --config=/path/to/apt-proxy.yaml
# or
APT_PROXY_CONFIG_FILE=/path/to/apt-proxy.yaml apt-proxy
```

## Configuration priority

```
CLI flags  >  Environment variables  >  Config file  >  Built-in defaults
```

So a flag (e.g. `--port=8000`) always wins over the same key in this YAML.

## What's in the file

- `server:` — bind host/port and debug logging.
- `cache:` — local cache directory, size cap, TTL, cleanup interval.
- `storage:` — switch between local disk and S3-compatible object storage.
  See [`../s3-otterio/`](../s3-otterio/) for a runnable S3 example.
- `mirrors:` — pin upstream mirrors per distro (full URL or shortcut like
  `cn:tsinghua`).
- `tls:` — terminate HTTPS at apt-proxy itself.
- `security:` — API key for protected `/api/*` endpoints.
- `mode:` — restrict to a single distro family or serve them all.
