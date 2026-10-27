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

### Repositories that share a path prefix

Some third-party archives use a path already recognized by a built-in
distribution. Proxmox Backup Client, for example, uses
`download.proxmox.com/debian/pbs-client/`, which also matches Debian's generic
`/debian/` prefix. For this case, configure both the original host in
`host_pattern` and the repository path in `url_pattern`.

Since v0.19.2, the routing fix for
[#167](https://github.com/soulteary/apt-proxy/issues/167) first prefers an entry
matching **both host and path**, then the existing path-only routes, then
host-only fallback for domain-root archives.
See the dedicated [Proxmox example](../proxmox/) for a working recipe and a
unique local path alias usable on older releases.

`host_pattern` is a routing hint, not a host allowlist: path aliases remain
accessible using the proxy's host name. When multiple custom entries overlap,
use distinct host/path combinations instead of relying on the order of
entries in YAML. The last capture group of `url_pattern` is appended to the
mirror root, so preserve the repository-relative suffix there.

## Run

```bash
cd examples/custom-distros
docker compose up -d
```

The file is mounted at `/etc/apt-proxy/distributions.yaml` and named with
`APT_PROXY_DISTRIBUTIONS_CONFIG`. Naming it is what makes this work on every
release: that path is also one of apt-proxy's search locations, but the search
itself only runs on releases that carry it, so the example does not rely on it.

Running the binary directly:

```bash
./apt-proxy --mode=all --distributions-config=./distributions.yaml
```

## Verifying it loaded

A request for an unregistered path returns `404`. A registered path returns
the upstream response, which can also be `404` when the requested file is
missing or the request was routed to the wrong mirror. Check the explicit
configuration path and startup errors, then compare the intended origin with
the proxied URL and inspect `--debug` logs to verify the selected upstream.
Neither `404` nor `cache: SKIP` alone proves that loading failed:

```bash
# Registered path: the status also depends on upstream availability/content.
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
- **Use the documented YAML keys.** Unknown keys are rejected: write
  `cache_control`, not `cache-control`. Use one YAML document per file. YAML
  anchors and merges can be used under recognized fields, but an extra
  top-level key used only to hold an anchor is still an unknown field. Check
  the reported load/reload error when a configuration is rejected.
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
