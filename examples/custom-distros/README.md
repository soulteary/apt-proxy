# custom-distros

Caches distributions apt-proxy does not ship — Deepin, Armbian and Arch Linux —
using only [`distributions.yaml`](distributions.yaml). No code change, no
rebuild.

## How it works

A `type` outside the built-in range (`1` Ubuntu, `2` UbuntuPorts, `3` Debian,
`4` CentOS, `5` Alpine; `0` reserved) registers a **new** distribution, which
then gets its own mirror list, benchmark and rewriter exactly like a built-in
one.

The three entries cover both archive shapes:

| Distribution | Shape | Matched by |
|---|---|---|
| Deepin | archive under a path prefix | `url_pattern` |
| Arch Linux | archive under a path prefix | `url_pattern` |
| Armbian | archive at a domain root (`apt.armbian.com`) | `host_pattern` |

## Run

```bash
cd examples/custom-distros
docker compose up -d
```

The file is mounted at `/etc/apt-proxy/distributions.yaml`, one of the paths
apt-proxy searches, so no flag is needed. Running the binary directly, either
drop it at a search path or name it:

```bash
./apt-proxy --distributions-config=./distributions.yaml
```

## Verifying it loaded

A request for an unregistered path returns `404`; a registered one is routed
upstream, so it returns whatever the mirror says — anything but `404` means the
entry took effect:

```bash
# registered -> 200 (or 502/504 if the mirror is unreachable from here)
curl -o /dev/null -w '%{http_code}\n' \
  http://127.0.0.1:3142/archlinux/core/os/x86_64/core.db

# not registered -> 404
curl -o /dev/null -w '%{http_code}\n' \
  http://127.0.0.1:3142/not-a-distro/core.db

# Armbian is matched by Host, not by path
curl -o /dev/null -w '%{http_code}\n' \
  -H 'Host: apt.armbian.com' \
  http://127.0.0.1:3142/dists/bookworm/InRelease
```

## Client setup

**Deepin** — `/etc/apt/sources.list`:

```text
deb http://apt-proxy.example:3142/deepin apricot main contrib non-free
```

**Arch Linux** — `/etc/pacman.d/mirrorlist`:

```text
Server = http://apt-proxy.example:3142/archlinux/$repo/os/$arch
```

**Armbian** — leave `sources.list` pointed at `apt.armbian.com` and use
apt-proxy as the HTTP proxy, which is what lets `host_pattern` recognise it:

```bash
# /etc/apt/apt.conf.d/01proxy
Acquire::http::Proxy "http://apt-proxy.example:3142";
```

Or rewrite the entry to the path form instead:

```text
deb http://apt-proxy.example:3142/armbian bookworm main bookworm-utils bookworm-desktop
```

## Adapting this

- **Suites.** `benchmark_url` must be a small file that exists on every mirror
  in the list. The Deepin and Armbian entries name a suite (`apricot`,
  `bookworm`) — change it to the release you run.
- **Mirrors.** The lists here are CN mirrors, matching apt-proxy's built-in
  defaults. Replace them with mirrors near you.
- **Keep `type` stable.** Mirror election and rewriter state are keyed by it.
- **Keep the catch-all cache rule last.** A path matching no rule returns `404`,
  not a pass-through, and `apt update` fetches package indexes (`Packages.xz`,
  `by-hash/…`) as well as `InRelease`.
- **`--mode=all`** (the default). `--mode` only names built-in distributions;
  custom ones are served whenever the mode is `all`.

## Reloading

Edit the file, then either send `SIGHUP` or:

```bash
curl -X POST http://127.0.0.1:3142/api/mirrors/refresh
```

## A note on scope

This is generic HTTP caching driven by path and host patterns. For Arch that
means package files and databases are cached with the TTLs above — there is no
pacman-aware database invalidation or prefetch, so it is not a
[pacoloco](https://github.com/anatol/pacoloco) replacement. For a LAN of
machines pulling the same packages, it is enough.
