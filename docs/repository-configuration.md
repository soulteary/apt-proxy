# Repository configuration and client recipes

[Project README](../README.md) · [中文](repository-configuration.zh-CN.md)

## Usage Examples

### Ubuntu / Debian

Configure your system to use the proxy by setting the `http_proxy` environment variable:

```bash
# Update package lists (first run will download and cache)
http_proxy=http://your-domain-or-ip-address:3142 \
  apt-get -o pkgProblemResolver=true -o Acquire::http=true update

# Install packages (subsequent installs will use cached packages)
http_proxy=http://your-domain-or-ip-address:3142 \
  apt-get -o pkgProblemResolver=true -o Acquire::http=true install vim -y
```

**Tip**: For convenience, you can export the proxy settings in your shell:

```bash
export http_proxy=http://your-domain-or-ip-address:3142
apt-get update
apt-get install vim -y
```

After the first download, all subsequent package operations will be significantly faster as packages are served from the local cache.

### CentOS

APT Proxy works with DNF/YUM repositories. CentOS Stream 9 and 10 use the
[CentOS Stream mirror](https://mirror.stream.centos.org/) and metalink-based
repository definitions. To keep Metalink discovery and DNF's metadata digest
checks, configure the optional `metalink` endpoint and replace the repository's
Metalink origin with apt-proxy. The XML's metadata and package links then pass
through one snapshot cache namespace. See [CentOS Metalink setup](../docs/centos-metalink.md)
and [real DNF client tests](../docs/dnf-client-tests.md).

The [Docker client compatibility suite](../docs/client-compatibility.md) tests
signed repository downloads, installation and proxy cache reuse with APT,
DNF/DNF5, APK, pacman and zypper. Run it manually in GitHub Actions; the complete
matrix also gates version publication.

For a fixed base URL instead, configure apt-proxy with the Stream mirror first:

```bash
./apt-proxy \
  --mode=centos \
  --centos=https://mirror.stream.centos.org/
```

On each CentOS Stream 9 or 10 client, disable the metalink entries and enable
their matching base URLs through apt-proxy. The existing `$stream` and
`$basearch` variables select the installed Stream release and architecture:

```bash
sudo sed -i \
  -e '/^metalink=/s/^/#/' \
  -e 's|^#baseurl=http://mirror.stream.centos.org|baseurl=http://your-domain-or-ip-address:3142/centos|' \
  -e 's|^#baseurl=https://mirror.stream.centos.org|baseurl=http://your-domain-or-ip-address:3142/centos|' \
  /etc/yum.repos.d/centos*.repo

sudo dnf clean all
sudo dnf makecache
```

Inspect the repository files before applying the command if they have been
customized by an image vendor. This fixed-base configuration does not use
Metalink; the optional Metalink endpoint requires an explicit mirror host list.
The client-facing URL intentionally uses HTTP while apt-proxy fetches from the
configured HTTPS upstream.

### Alpine Linux

Configure Alpine's APK package manager to use the proxy:

```bash
# Update repositories to use proxy
cat /etc/apk/repositories | \
  sed -e s#https://.*.alpinelinux.org#http://your-domain-or-ip-address:3142# | \
  tee /etc/apk/repositories

# Verify configuration
apk update
```

## Advanced Configuration

### apt-cacher-ng-style host-prefixed paths

For known distributions, apt-proxy accepts the host-prefixed path form commonly
used with apt-cacher-ng. For example:

```text
deb http://apt-proxy.example:3142/ftp.uni-kl.de/debian bookworm main
```

The embedded hostname is a compatibility prefix only. A request such as
`/ftp.uni-kl.de/debian/dists/bookworm/InRelease` is routed to the Debian mirror
configured in apt-proxy; it does **not** grant access to `ftp.uni-kl.de` or turn
the service into an unrestricted origin proxy. Host-prefixed
`/security.debian.org/debian-security/...` paths use the configured dedicated
Debian Security mirror. Query parameters are preserved, and paths that do not
match a configured distribution return `404`.

The prefix has to be a single host: either nothing at all (`/debian/dists/...`)
or exactly one host-shaped segment in front of the distribution segment
(`/ftp.uni-kl.de/debian/dists/...`, optionally with a `:port`, an IP literal, or
`localhost`). Anything deeper returns `404`, because it is a third-party archive
rather than a mirror of the distribution:

```text
/ppa.launchpad.net/deadsnakes/ppa/ubuntu/dists/jammy/InRelease   404
/download.docker.com/linux/ubuntu/dists/jammy/InRelease          404
```

Such an archive is **not** a copy of the distribution it is nested under, so
answering it from the distribution's mirror would hand the client a different
repository's content. apt-proxy has no mirror for a PPA or a vendor repository;
point those `sources.list` entries at their origin directly.
### Distributions and Mirrors Config (distributions.yaml)

You can maintain distributions and mirror lists via an external YAML file without changing code or recompiling.

**Available since v0.19.2 ([#167](https://github.com/soulteary/apt-proxy/issues/167)):**
the host-and-path routing priority and strict YAML validation described below
are included in v0.19.2 and later. v0.19.3 additionally fixes mirror-root URL
joining when a trailing slash is absent; use v0.19.3 or later for Proxmox. See the
[Proxmox Backup Client example](../examples/proxmox) for original-source proxying
and a distinct local path alias. v0.19.4 adds commented PVE, PBS Server, PBS Client
and Ceph Squid entries to [`config/distributions.yaml`](../config/distributions.yaml);
these remain disabled until explicitly added to the active distribution list.

**Pointing apt-proxy at the file:** drop it at one of the search paths, or name
it explicitly. When no path is given, the first of these that exists wins:

1. `./config/distributions.yaml`
2. `./distributions.yaml`
3. `/etc/apt-proxy/distributions.yaml`
4. `~/.config/apt-proxy/distributions.yaml`

```bash
# found on the search path
./apt-proxy

# or named explicitly, from anywhere
./apt-proxy --distributions-config=/srv/apt-proxy/distributions.yaml
```

`APT_PROXY_DISTRIBUTIONS_CONFIG` sets the same path as the flag. If nothing is
found the built-in distributions are used, and a file that fails to parse leaves
the built-ins in place with a warning rather than taking the server down.

Use a single YAML document and the documented field names. Unknown fields,
including `cache-control` instead of `cache_control`, are rejected with a line
number and field name. Empty files remain valid. Anchors and merges within
recognized fields are supported; extra top-level anchor-helper keys are not.
This validation applies to `distributions.yaml`, not the main server config.

**Example `config/distributions.yaml`:**

```yaml
distributions:
  - id: ubuntu
    name: Ubuntu
    type: 1
    url_pattern: "/ubuntu/(.+)$"
    benchmark_url: "dists/noble/main/binary-amd64/Release"
    geo_mirror_api: "http://mirrors.ubuntu.com/mirrors.txt"
    cache_rules:
      - pattern: "deb$"
        cache_control: "max-age=100000"
        rewrite: true
    mirrors:
      official:
        - "mirrors.tuna.tsinghua.edu.cn/ubuntu/"
        - "mirrors.ustc.edu.cn/ubuntu/"
      custom:
        - "mirrors.163.com/ubuntu/"
    aliases:
      tsinghua: "mirrors.tuna.tsinghua.edu.cn/ubuntu/"
      ustc: "mirrors.ustc.edu.cn/ubuntu/"
```

After editing the file, send **SIGHUP** or call **POST /api/mirrors/refresh** to hot-reload without restart.

Reload is published only after the configuration loads and all required mirror
elections succeed. A failed reload keeps the previous routing and cache rules;
the API returns a structured error (`500` for configuration loading failures,
`503` for failed mirror election), and SIGHUP logs the failure. Repair the file
or mirror connectivity and retry. Routing first tries entries matching both
host and path, then path-only entries, then host-only fallback. Within each
phase, built-ins retain their established order and overlapping custom entries
are tried in ascending `id` order. Within a distribution, the first matching
`cache_rules[]` entry wins; a selected distribution with no matching cache rule
returns `404` rather than falling through to another repository.

**Field reference:**

- `id` — stable, unique registry identifier, also shown in rewrite debug logs. `url_pattern` determines which URL paths it serves.
- `name` — human-readable display name.
- `type` — integer distro type. `1` Ubuntu, `2` UbuntuPorts, `3` Debian, `4` CentOS and `5` Alpine reconfigure the built-in distributions; `0` is reserved for "all". **Any other positive integer registers a distribution apt-proxy does not ship** — see [Adding a distribution apt-proxy does not ship](#adding-a-distribution-apt-proxy-does-not-ship). A type already in use is rejected at load time, so pick a free number (`6`, `7`, …) and keep it stable: it is the key the mirror and rewriter state is held under across reloads.
- `url_pattern` — regex matched against the request path; its last captured group is appended to the upstream mirror root.
- `host_pattern` — optional regex matched against the original request host, lower-cased and including any explicit port. An entry matching both host and `url_pattern` takes priority over generic path-only routes. If no distribution path matches, host-only fallback treats the entire request path as relative to the mirror root (for example `security.debian.org` or `apt.armbian.com`). Anchor it (`^...$`) to avoid matching lookalike hosts. This is a routing hint, not a host allowlist: path aliases still work with the proxy's host name. Omitting it inherits the built-in matcher for that distro type (Debian keeps `security.debian.org`); setting it replaces the built-in. Only the built-in Debian security host routes to the dedicated Debian Security mirror; a custom type `3` host resolves to that entry's own mirror.
- `benchmark_url` — relative path probed during mirror benchmarking.
- `geo_mirror_api` — optional URL returning a list of geo-located mirrors (Ubuntu-style `mirrors.txt`).
- `cache_rules[]` — per-pattern cache directives. `cache_control` supplies a default response `Cache-Control` for matched paths when the upstream omits both `Cache-Control` and `Expires` (`200` uses the configured value; `404` uses `public, max-age=30`). Explicit upstream directives and expiration dates, including past or invalid `Expires` dates, shorter freshness lifetimes, `private`, and `no-store`, are preserved. Defaults are applied before cache storage; `rewrite: true` enables URL rewriting for that pattern.
- `mirrors.official` / `mirrors.custom` — mirror host lists. Aliases of the form `cn:<name>` are auto-generated from each mirror's host (e.g. `mirrors.tuna.tsinghua.edu.cn` → `cn:tsinghua`).
- `aliases` — explicit name-to-mirror mapping that overrides/augments the auto-generated aliases.

**Adding or editing a distribution:** Add or edit an entry under `distributions` with `id`, `name`, `type`, `url_pattern`, `benchmark_url`, `cache_rules`, `mirrors`, and `aliases` (plus `host_pattern` for a host-root archive or a repository whose path overlaps another distribution). The repo includes `config/distributions.yaml` and a [Proxmox example](../examples/proxmox) that you can extend.

### Adding a distribution apt-proxy does not ship

The five built-in distributions are not the limit. Give an entry a `type` outside
`1`–`5` and it is registered as a new distribution: it gets its own mirror list,
its own benchmark and its own rewriter, exactly like a built-in one. No code
change or rebuild is involved.

Deepin, cached under `/deepin/...`:

```yaml
distributions:
  - id: deepin
    name: Deepin
    type: 6
    url_pattern: "/deepin/(.+)$"
    benchmark_url: "dists/apricot/main/binary-amd64/Release"
    cache_rules:
      - pattern: "deb$"
        cache_control: "max-age=100000"
        rewrite: true
      - pattern: "(InRelease|Release(\\.gpg)?)$"
        cache_control: "max-age=3600"
        rewrite: true
      # Catch-all last: apt also fetches package indexes
      # (Packages.xz, by-hash/...), and an unmatched path is a 404.
      - pattern: ".*"
        cache_control: "max-age=3600"
        rewrite: true
    mirrors:
      official:
        - "community-packages.deepin.com/deepin/"
```

```text
deb http://apt-proxy.example:3142/deepin apricot main contrib non-free
```

An archive served from a domain root has no path prefix for `url_pattern` to
match, so name it with `host_pattern` instead. Armbian, whose `sources.list`
entry is `deb http://apt.armbian.com <suite> main`:

```yaml
distributions:
  - id: armbian
    name: Armbian
    type: 7
    url_pattern: "/armbian/(.+)$"
    host_pattern: "^apt\\.armbian\\.com(:\\d+)?$"
    benchmark_url: "dists/bookworm/main/binary-arm64/Release"
    cache_rules:
      - pattern: "deb$"
        cache_control: "max-age=100000"
        rewrite: true
      - pattern: "(InRelease|Release(\\.gpg)?)$"
        cache_control: "max-age=3600"
        rewrite: true
      # Catch-all last: apt also fetches package indexes
      # (Packages.xz, by-hash/...), and an unmatched path is a 404.
      - pattern: ".*"
        cache_control: "max-age=3600"
        rewrite: true
    mirrors:
      official:
        - "mirrors.tuna.tsinghua.edu.cn/armbian/"
```

Point the client at apt-proxy with `Host: apt.armbian.com` (an `http_proxy`
setting does this for you) and requests for `/dists/<suite>/...` resolve against
the configured mirror.

A runnable version of all three shapes — Deepin, Armbian and Arch Linux in one
file, with client setup for each — is in
[`examples/custom-distros/`](../examples/custom-distros).

Notes that save a round of debugging:

- Keep `type` stable across reloads — mirror election and rewriter state are keyed by it.
- `benchmark_url` must be a small file that exists on every mirror in the list; it is fetched to rank them.
- `cache_rules` are tried in order, first match wins, and **a path matching no rule is a `404`** — not a pass-through. `apt update` fetches package indexes (`Packages.xz`, `by-hash/...`) as well as `InRelease`, so end with a catch-all `".*"` unless you are deliberately serving only certain file types.
- Run with `--mode=all` (the default). `--mode` only names the built-in distributions; a custom one is served whenever the mode is `all`.
- Only requests matching `url_pattern` (or `host_pattern`) are proxied; everything else still returns `404`.
- The file has to be somewhere apt-proxy looks: one of the search paths above, or named with `--distributions-config` / `APT_PROXY_DISTRIBUTIONS_CONFIG`. A file the server never loads is an entry that silently does nothing. The startup log names the file in effect and every distribution that registered (`distributions registered config=... distributions=[...]`), which is the quickest way to confirm yours did.

### Custom Mirror Selection

By default, APT Proxy automatically benchmarks available mirrors and selects the fastest one. However, you can specify custom mirrors if needed.

**Using Full URLs:**

```bash
# Cache multiple distributions
./apt-proxy \
  --ubuntu=https://mirrors.tuna.tsinghua.edu.cn/ubuntu/ \
  --debian=https://mirrors.tuna.tsinghua.edu.cn/debian/

# Cache only Ubuntu packages (reduces memory usage)
./apt-proxy --mode=ubuntu --ubuntu=https://mirrors.tuna.tsinghua.edu.cn/ubuntu/

# Cache only Debian packages
./apt-proxy --mode=debian --debian=https://mirrors.tuna.tsinghua.edu.cn/debian/
```

**Using Mirror Shortcuts:**

For convenience, you can use predefined shortcuts instead of full URLs:

```bash
./apt-proxy --ubuntu=cn:tsinghua --debian=cn:163
```

**Available Shortcuts:**

- `cn:tsinghua` - Tsinghua University Mirror
- `cn:ustc` - USTC Mirror
- `cn:163` - NetEase Mirror
- `cn:aliyun` - Alibaba Cloud Mirror
- `cn:huaweicloud` - Huawei Cloud Mirror
- `cn:tencent` - Tencent Cloud Mirror

The startup log records each explicitly selected mirror with the
`using specified mirror` message and its distribution and URL fields.

### Caching Third-Party Archives (`passthrough`)

apt-proxy mirrors the distributions it is configured for; anything else is a
`404`, deliberately — it is not an open forward proxy. The archives people
install from are not only distributions, though: a Launchpad PPA, a vendor
repository, an internal archive. Name those origins in an allowlist and
apt-proxy fetches and caches them as they are, without rewriting:

```bash
./apt-proxy --passthrough=ppa.launchpad.net,https://download.docker.com
```

```yaml
# apt-proxy.yaml
passthrough:
  - ppa.launchpad.net
  - https://download.docker.com
  - archive.internal.example:8080
```

`APT_PROXY_PASSTHROUGH` takes the same comma-separated form.

**Client setup.** Passthrough applies to clients that use apt-proxy as their
HTTP proxy, because that is what names the origin in the request:

```bash
http_proxy=http://apt-proxy.example:3142 apt-get update
```

The `sources.list` entry stays pointed at the origin. The URL-prefix form
(`deb http://apt-proxy.example:3142/<host>/...`) addresses apt-proxy itself, so
no origin is named and nothing passes through.

**The entry has to be `http://`, even for an archive that is HTTPS-only:**

```text
deb http://download.docker.com/linux/ubuntu jammy stable
```

```yaml
passthrough:
  - https://download.docker.com   # apt-proxy makes the upstream hop TLS
```

An `https://` entry in `sources.list` makes apt send `CONNECT host:443` to its
proxy and tunnel through it. A tunnel is encrypted end to end, so apt-proxy
could forward the bytes but never read or cache them — which is the entire
point of running it. apt-proxy therefore does not answer `CONNECT`; write the
source as `http://` and let the `https://` allowlist entry (or [the `HTTPS///`
marker](troubleshooting.md#403-on-https-urls)) carry the TLS upstream. The client-to-apt-proxy
hop is plain HTTP on your own network, and apt verifies the repository's
signatures regardless of transport.

**Entry forms:**

| Entry | Effect |
|-------|--------|
| `ppa.launchpad.net` | Proxied on the client's scheme, default ports only |
| `https://download.docker.com` | Upstream request upgraded to TLS |
| `archive.example:8080` | Only that port matches |

An entry without a port matches ports 80 and 443 only — an allowlisted archive
host should not also expose whatever else listens on that machine.

**What it does not do:**

- Wildcards are not supported. Name each origin; a typo is rejected at startup
  rather than silently allowing less (or more) than you wrote.
- Loopback, private, link-local addresses and `localhost` are refused as
  entries. A *hostname* that resolves into your network is still accepted — the
  allowlist is the security boundary, not a network-level control. Only put
  origins there that you are willing to let any client of this proxy reach.
- `GET` and `HEAD` only. apt fetches; it does not write.
- Cache lifetime is the origin's: apt-proxy knows a distribution's index and
  package TTLs, but nothing about a third-party archive, so its
  `Cache-Control` is respected as-is.
- Distribution routing wins. If an allowlisted host is also served by a
  configured distribution, that distribution's mirror and cache rules apply.

Startup logs the allowlist (`passthrough enabled for third-party origins`), so
an empty or mistyped list is visible rather than silent.

**apt-cacher-ng's `HTTPS///` marker** is the other way to reach an allowlisted
origin, for clients already written for it:

```text
deb http://HTTPS///get.docker.com/linux/ubuntu jammy stable
```

Both spellings are accepted, the origin goes through the same allowlist, and
the upstream request is always TLS. See [`403` on `HTTPS///`
URLs](troubleshooting.md#403-on-https-urls).

### Reaching Mirrors Through an Upstream Proxy

apt-proxy's outbound connections honour the standard proxy environment
variables, so a host that cannot reach mirror sites directly can route them
through an existing forward proxy:

```bash
HTTP_PROXY=http://proxy.internal:3128 \
HTTPS_PROXY=http://proxy.internal:3128 \
NO_PROXY=10.0.0.0/8,.internal \
  ./apt-proxy
```

| Variable | Effect |
|----------|--------|
| `HTTP_PROXY` / `http_proxy` | Proxy for `http://` upstream requests |
| `HTTPS_PROXY` / `https_proxy` | Proxy for `https://` upstream requests |
| `NO_PROXY` / `no_proxy` | Comma-separated hosts, domain suffixes (`.example.com`) and CIDRs that bypass the proxy |

A SOCKS5 forward proxy works too — write it as the proxy URL:

```bash
HTTPS_PROXY=socks5h://127.0.0.1:1080 ./apt-proxy
```

These variables cover both package fetches and the benchmark that elects the
fastest mirror, so mirror selection reflects the path the downloads will
actually take. They apply only to apt-proxy's own connections to mirror sites;
clients still reach apt-proxy directly, so do not put apt-proxy's own address in
`HTTP_PROXY`. In Docker, pass them with `-e`.

When `upstream_access.enabled: true`, repository downloads, mirror probes and
Ubuntu geo discovery connect directly and ignore these proxy variables. Disable
that policy to retain forward-proxy routing. See [upstream access](../docs/upstream-access.md)
for its enforcement boundary and configuration.

## Repository operations and download policies

### Download resource limits

Unknown-length downloads default to a 1 GiB per-response spool limit and a 4 GiB shared spool budget per instance. Upstream fetches default to a 20-minute total timeout. Configure `download.max_spool_mb`, `download.total_spool_mb`, and `download.timeout_seconds` in YAML; zero selects the defaults. See [download limits](../docs/download-limits.md) for scope and failure behavior.

### Private repository requests

Requests with `Authorization` or `Cookie` bypass shared caching; `Set-Cookie` responses are not stored. Authenticated cache-only requests return 504 without upstream access. This version uses a new cache-key namespace to exclude pre-upgrade private responses, causing a one-time cold cache without deleting legacy files. See [private repository isolation](../docs/private-repositories.md) for migration and scope.

### Package download client access

Use `security.download_allow_cidrs` and `security.download_deny_cidrs` in YAML
to restrict package downloads by connection-peer IPv4/IPv6 networks. Deny
rules take precedence and checks run before cache lookup and upstream fetching.
Both lists are empty by default. Forwarded headers do not grant access;
behind a reverse proxy, restrict end clients at the proxy. Operational/API
routes retain their existing controls. Restart to apply policy changes.
See [download access control](../docs/download-access.md) for configuration and scope.

### Guarded download upstreams

Optional YAML `upstream_access.enabled: true` restricts package downloads, mirror
probes and Ubuntu geo discovery by target hosts, ports and resolved IPs. New connections validate all DNS answers and
connect directly to approved IPs, preserving TLS hostname verification.
Private repositories require explicit CIDR exceptions. Enabled repository
transports ignore environment forward proxies; probe/discovery redirects and
HEAD-to-GET fallback remain guarded. Startup and mirror refresh use the same
per-instance policy. Restart to change policy.
See [upstream access](../docs/upstream-access.md) for examples and the separate
background-client boundary.

### Debian-family source packages

Ubuntu, Ubuntu Ports and Debian source pool files now use the configured
mirror and shared cache: `.dsc`, upstream/component/native archives, Debian
packaging archives, legacy `.diff.gz` and original-archive `.asc` signatures.
Rules are limited to versioned source filenames in Debian-style pools and
preserve upstream cache directives. Existing custom distribution YAML must
adopt the new rule explicitly. See [source package coverage](../docs/apt-source-packages.md)
for supported formats and client-side verification responsibilities.

### Debian-family installer files

Common netboot, hd-media and cdrom boot files and installer checksum manifests
can now use configured mirrors and the cache. Rules require exact filenames in
Debian-style `images/` or `legacy-images/` installer directories, with fallback `max-age=3600`. Custom YAML
must adopt the new rules explicitly. See [installer file coverage](../docs/apt-installer-files.md)
for the supported subset and client verification responsibilities.

Common installer PXE/UEFI resources are also covered: EFI loaders, the default
PXELINUX configuration, boot menus and GRUB modules. Paths stay confined to
installer netboot trees; this does not add DHCP/TFTP or boot configuration rewriting.

### Debian-family AppStream metadata

Direct DEP-11 component indexes, content-ID indexes and known icon archives
now use the mirror and cache, including clients that fall back from by-hash URLs.
Rules stay confined to `dists/<suite>/<component>/dep11/`; this does not allow
arbitrary YAML/JSON files or tarballs. See [AppStream coverage](../docs/apt-appstream.md).

### Temporary resources and cache integrity

Repository downloads now share nonblocking temporary-storage and active-download
admission, including declared-length responses and overlapping cache/S3 copies.
Defaults are a 1 GiB object limit, 4 GiB temporary budget per process, 32 active
upstream downloads and 128 MiB disk headroom. Shared-budget refusal returns
`503` with `Retry-After: 1`; a later cache-copy refusal preserves the complete
upstream response and skips publication.
See [resource limits](../docs/resource-limits.md) for YAML defaults and overload
behavior. Persistent cache SHA256 verification is enabled by default; see
[cache integrity](../docs/cache-integrity.md) for legacy-cache migration, storage I/O
cost and the explicit `cache.integrity: none` opt-out. Enabling `shared_cache`
requires SHA256 integrity even with that opt-out.

### Complete repository snapshots and offline access

`apt-proxy repository` can prefetch a selected suite/component/architecture's
Packages and optional Sources indexes and every referenced object into a verified
snapshot. Verify, repair, export and import operate on that selected set; `serve`
provides an offline HTTP endpoint with no network fallback. APT verifies the
preserved signatures. Selected xz indexes require the system `xz` executable.
See [repository snapshots](../docs/repository-offline.md) for commands, scope and limits.

Explicit public repository identities and Unix shared disk coordination: [configuration and operational limits](../docs/shared-repository-cache.md).

### Runtime mirror health and client validation

Enable `mirror_health.enabled: true` in YAML for bounded failover between
explicitly equivalent public Debian-family archives. Every fully observed
Release/InRelease fingerprint must match on the replacement; unknown, expired
or divergent snapshots and private requests cannot switch. There is no automatic
failback or background polling. `GET /api/mirrors/status` uses the existing
management API policy. See [runtime mirror health](../docs/runtime-mirror-health.md).

Run `bash tests/apt-client/matrix.sh` for signed local repository tests with five
actual APT clients, including installation, sources, SHA256 by-hash, gzip fallback,
ranges, failure recovery and imported offline snapshots. This does not establish
compatibility with every public mirror. See [client tests](../docs/apt-client-tests.md).
