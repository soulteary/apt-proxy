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
The main server config (`apt-proxy.yaml`) is parsed with the same strictness; see
the [configuration reference](configuration-reference.md#yaml-configuration-file).

**Example `config/distributions.yaml`:**

```yaml
distributions:
  - id: ubuntu
    name: Ubuntu
    type: 1
    url_pattern: "/ubuntu/(.+)$"
    benchmark_url: "dists/devel/Release"
    geo_mirror_api: "http://mirrors.ubuntu.com/mirrors.txt"
    official_url: "http://archive.ubuntu.com/ubuntu/"
    cache_rules:
      - pattern: "deb$"
        cache_control: "max-age=100000, immutable"
        rewrite: true
    mirrors:
      official:
        - "http://archive.ubuntu.com/ubuntu/"
        - "mirrors.tuna.tsinghua.edu.cn/ubuntu/"
        - "mirrors.ustc.edu.cn/ubuntu/"
      custom:
        - "mirrors.163.com/ubuntu/"
    aliases:
      tsinghua: "mirrors.tuna.tsinghua.edu.cn/ubuntu/"
      ustc: "mirrors.ustc.edu.cn/ubuntu/"
```

After editing the file, send **SIGHUP** or call **POST /api/mirrors/refresh** to hot-reload without restart.

Reload is published only after the configuration loads and every distribution
that needs a mirror has one. A distribution whose mirror election fails keeps
the mirror it was using, takes its reloaded match patterns and cache rules, and
is named in a warning; that alone does not fail the reload. A distribution with
no previous mirror, such as one the reload adds, falls back to its
`official_url`. The reload fails when such a distribution cannot elect a mirror
and has no `official_url`. It also fails when [`mode`](configuration-reference.md#mode) names an
`id` that the reloaded file no longer defines. A failed reload keeps the
previous routing and cache rules;
the API returns a structured error (`500` for configuration loading failures,
`400` for a mode that no longer matches the file, `503` for failed mirror
election), and SIGHUP logs the failure. Repair the file
or mirror connectivity and retry. Routing first tries entries matching both
host and path, then path-only entries, then host-only fallback. Within each
phase, built-ins retain their established order and overlapping custom entries
are tried in ascending `id` order. Within a distribution, the first matching
`cache_rules[]` entry wins; a selected distribution with no matching cache rule
returns `404` rather than falling through to another repository.

**Field reference:**

- `id` — stable, unique registry identifier, also shown in rewrite debug logs. It is the name [`mode`](configuration-reference.md#mode) selects the distribution by. `url_pattern` determines which URL paths it serves.
- `name` — human-readable display name.
- `type` — integer distro type. `1` Ubuntu, `2` UbuntuPorts, `3` Debian, `4` CentOS and `5` Alpine reconfigure the built-in distributions; `0` is reserved for "all". **Any other positive integer registers a distribution apt-proxy does not ship** — see [Adding a distribution apt-proxy does not ship](#adding-a-distribution-apt-proxy-does-not-ship). A type already in use is rejected at load time, so pick a free number (`6`, `7`, …) and keep it stable: it is the key the mirror and rewriter state is held under across reloads.
- `url_pattern` — regex matched against the request path; its last captured group is appended to the upstream mirror root.
- `host_pattern` — optional regex matched against the original request host, lower-cased and including any explicit port. An entry matching both host and `url_pattern` takes priority over generic path-only routes. If no distribution path matches, host-only fallback treats the entire request path as relative to the mirror root (for example `security.debian.org` or `apt.armbian.com`). Anchor it (`^...$`) to avoid matching lookalike hosts. This is a routing hint, not a host allowlist: path aliases still work with the proxy's host name. Omitting it inherits the built-in matcher for that distro type (Debian keeps `security.debian.org`); setting it replaces the built-in. Only the built-in Debian security host routes to the dedicated Debian Security mirror; a custom type `3` host resolves to that entry's own mirror.
- `benchmark_url` — path, relative to each mirror root, probed during mirror benchmarking. A mirror is eligible only if it answers `200` itself: a redirect, any other status or an HTML page disqualifies it.
- `geo_mirror_api` — optional URL returning a list of geo-located mirrors (Ubuntu-style `mirrors.txt`).
- `official_url` — optional address of the distribution's own archive, as a full `http(s)` URL or, like a mirror list entry, without a scheme (then `https`). It is used without being probed: when [mirror election is off](#custom-mirror-selection), until the first election has finished, and when an election finds no usable mirror and there is no previous mirror to keep. An entry without it uses its first listed mirror in the first two cases and has no fallback in the third. An entry does not inherit the official address of a built-in type it reconfigures, and the address takes part in the election only if it is also listed under `mirrors`.
- `cache_rules[]` — per-pattern cache directives. `cache_control` supplies a default response `Cache-Control` for matched paths when the upstream omits both `Cache-Control` and `Expires` (`200` uses the configured value; `404` uses `public, max-age=30`). Explicit upstream directives and expiration dates, including past or invalid `Expires` dates, shorter freshness lifetimes, `private`, and `no-store`, are preserved. Defaults are applied before cache storage. `rewrite: true` points a matched request at the distribution's mirror, and a rule needs it to be served: a distribution request is fetched from that mirror only, never from the host named in the request, so a rule without it answers `404`. A `cache_control` containing the `immutable` directive (for example `max-age=100000, immutable`) also declares that the matched URLs never change content: a zero `max-age` in the request is ignored and an expired entry survives an upstream failure. Use it only for versioned or content-addressed paths; see [freshness and immutable artifacts](cache-storage.md#freshness-and-immutable-artifacts).
- `mirrors.official` / `mirrors.custom` — mirror host lists. Aliases of the form `cn:<name>` are auto-generated from each mirror's host (e.g. `mirrors.tuna.tsinghua.edu.cn` → `cn:tsinghua`).
- `aliases` — explicit name-to-mirror mapping that overrides/augments the auto-generated aliases.

**Adding or editing a distribution:** Add or edit an entry under `distributions` with `id`, `name`, `type`, `url_pattern`, `benchmark_url`, `cache_rules`, `mirrors`, and `aliases` (plus `host_pattern` for a host-root archive or a repository whose path overlaps another distribution, and `official_url` for an address to use without an election). The repo includes `config/distributions.yaml` and a [Proxmox example](../examples/proxmox) that you can extend.

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
- A custom distribution gets only the lifetimes written in its rules. To treat versioned packages or `by-hash` paths like the built-in Debian-family rules do, add `immutable` to that rule's `cache_control` (for example `max-age=100000, immutable`) and keep the rule ahead of the catch-all.
- The mode has to include the distribution. `--mode=all` (the default) serves every registered distribution; a list such as `--mode=debian,deepin` serves the named ones only, built-in or custom, by `id`. See [Serving only some distributions](#serving-only-some-distributions).
- A distribution that the mode does not select is not served and takes no part in request matching. Its paths answer `404` in every request form, including for a proxy client that names another host: apt-proxy never fetches a distribution path from the host a client names. Two cases remain served: a path that the `url_pattern` of an enabled distribution matches as well goes to that distribution, and a request to an origin on the [`passthrough`](#caching-third-party-archives-passthrough) allowlist goes to that origin.
- Only requests matching `url_pattern` (or `host_pattern`) are proxied; everything else still returns `404`.
- The file has to be somewhere apt-proxy looks: one of the search paths above, or named with `--distributions-config` / `APT_PROXY_DISTRIBUTIONS_CONFIG`. A file the server never loads is an entry that silently does nothing. The startup log names the file in effect and every distribution that registered (`distributions registered config=... distributions=[...]`), which is the quickest way to confirm yours did. The line after it, `distributions enabled mode=[...]`, lists the ones the mode serves.

### Serving only some distributions

`mode` limits apt-proxy to the distributions it names, by `id`. Built-in and
custom distributions can be mixed. A distribution that is not named answers
`404`, has no mirror elected or benchmarked, and ignores its mirror setting.

```bash
# Debian and the Deepin entry of distributions.yaml, and nothing else
./apt-proxy --mode=debian,deepin --distributions-config=./distributions.yaml
```

```yaml
# apt-proxy.yaml
mode: [debian, deepin]
distributions_config: ./distributions.yaml
```

`APT_PROXY_MODE=debian,deepin` is the environment form. The IDs are checked at
startup against the built-in distributions and the loaded `distributions.yaml`:
an ID that is not registered stops startup with an error that lists the
registered ones, and `all` cannot be combined with other values. The
[configuration reference](configuration-reference.md#mode) has the complete
rules, including what a reload does when an enabled `id` has been removed from
the file.

### Custom Mirror Selection

Each distribution is fetched from one mirror. apt-proxy chooses it in this order:

1. **The pinned mirror.** A mirror set with `--ubuntu`, `--ubuntu-ports`, `--debian`, `--centos` or `--alpine`, the matching environment variable or the matching `mirrors` key in `apt-proxy.yaml` is used as it is. Pinning a mirror disables election for that distribution: its candidates are not probed, at startup or on refresh.
2. **The official address, when mirror election is off.** A distribution without a pinned mirror then uses its official address. Nothing is probed and the Ubuntu geo mirror API is not queried.
3. **The elected mirror.** Otherwise apt-proxy probes the candidate mirrors of the distribution and uses the fastest one. This is the default.

**Official addresses:**

| Distribution | Official address |
|--------------|------------------|
| Ubuntu | `http://archive.ubuntu.com/ubuntu/` |
| Ubuntu Ports | `http://ports.ubuntu.com/ubuntu-ports/` |
| Debian | `https://deb.debian.org/debian/` |
| Alpine | `https://dl-cdn.alpinelinux.org/alpine/` |
| CentOS | none |

Debian security requests go to the dedicated `--debian-security` mirror when one
is set. Otherwise they go to the `/debian-security/` archive on the Debian mirror
in use, which for the official address is `https://deb.debian.org/debian-security/`.

CentOS has no official address, because no host of the CentOS project serves the
tree of the listed mirrors together with the `TIME` file that the election
probes: `mirror.centos.org/centos/` no longer carries a release,
`vault.centos.org/centos/` carries the releases but no `TIME` file, and
`mirror.stream.centos.org` serves CentOS Stream 9 and 10 under other paths.
Where an official address would be used, CentOS uses the first mirror of its
list instead, and with election off it logs a warning that names that mirror.
To use a host of the project, pin it: `--centos=https://vault.centos.org/centos/`
for CentOS 7 and 8, or `--centos=https://mirror.stream.centos.org/` for CentOS
Stream as shown under [CentOS](#centos).

A `distributions.yaml` entry has an official address only if it sets
`official_url`; this includes an entry that reconfigures a built-in distribution.

**Mirror election:**

The candidates of a built-in distribution are its official address followed by
its built-in mirror list. Ubuntu first asks the geo mirror API
(`http://mirrors.ubuntu.com/mirrors.txt`) for mirrors near the server and uses
its built-in list only when that request fails or returns no mirror. A
distribution with a `distributions.yaml` entry uses the mirrors listed there. An
election runs in the background at startup, and again on `SIGHUP` and
`POST /api/mirrors/refresh`.

- Until the first election has finished, requests are fetched from the official
  address.
- When an election finds no usable mirror, the distribution keeps the mirror it
  was using. A distribution that has no mirror yet uses its official address.
- A probe does not follow redirects. A candidate is eligible only if it answers
  `200` itself with something other than an HTML page.

**Turning election off:**

```bash
./apt-proxy --mirror-election=false
```

`APT_PROXY_MIRROR_ELECTION=false` and, in `apt-proxy.yaml`, `mirrors.election: false`
do the same. Every distribution without a pinned mirror then uses its official
address at startup and on refresh, and a pinned mirror is still used for its
distribution. The startup log names the mirror of each distribution with the
`mirror election is off, using official mirror` message.

The environment variable takes `true` or `false` (also `1` or `0`). A value that
is not a boolean, such as `off`, leaves election on, as it does for the other
boolean variables; check the startup log after setting it.

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

**Redirects.** An origin may answer with a redirect to another host instead of
the file; `pkgs.k8s.io` sends every index and package request to a CDN. A
client cannot act on that answer through apt-proxy, so apt-proxy follows the
redirect itself and caches the file under the URL the client asked for. The
CDN host does not have to be on the allowlist:

```bash
./apt-proxy --passthrough=https://pkgs.k8s.io
```

```text
deb http://pkgs.k8s.io/core:/stable:/v1.31/deb/ /
```

Up to five redirects are followed per request. A redirect to another
allowlisted origin is followed the way a client request for that origin is
served. A redirect to any other host is followed only if all of these hold:

- The host resolves to public addresses only, and apt-proxy connects to an
  address it checked. Loopback, private, link-local and other special-purpose
  addresses are refused, so a redirect cannot lead to the machine apt-proxy
  runs on or to the network behind it.
- The port is 80 or 443.
- The redirect does not go from `https` to `http`.
- apt-proxy connects directly. Behind a forward proxy (`HTTP_PROXY`,
  `HTTPS_PROXY`) the proxy resolves the name and the address cannot be
  checked. The same applies where DNS answers with placeholder addresses, as
  transparent proxies in fake-IP mode do (`198.18.0.0/15`).
- [`upstream_access`](upstream-access.md), if enabled, allows the host.

Otherwise the redirect is passed to the client unchanged and a warning in the
log gives the reason. A host on the allowlist is not subject to the first four
conditions; its entry decides the port and the scheme. Credentials a client
sent for the origin are not sent on to a different host.

Following redirects means the allowlist no longer bounds what apt-proxy
fetches: an allowlisted origin can send it to any public host. The startup log
says so. To keep fetches within the allowlist, turn it off:

```bash
./apt-proxy --passthrough=https://prod-cdn.packages.k8s.io --passthrough-follow-redirects=false
```

```yaml
passthrough_follow_redirects: false
```

`APT_PROXY_PASSTHROUGH_FOLLOW_REDIRECTS=false` does the same. Every redirect
then reaches the client, as in earlier versions. A client behind the proxy can
use a redirecting origin only by naming the host that serves the files:

```text
deb http://prod-cdn.packages.k8s.io/repositories/isv:/kubernetes:/core:/stable:/v1.31/deb/ /
```

**What it does not do:**

- Wildcards are not supported. Name each origin; a typo is rejected at startup
  rather than silently allowing less (or more) than you wrote.
- Loopback, private, link-local addresses and `localhost` are refused as
  entries. A *hostname* that resolves into your network is still accepted — the
  allowlist is the security boundary, not a network-level control. Only put
  origins there that you are willing to let any client of this proxy reach.
  With redirects followed, that includes the public hosts they redirect to.
- `GET` and `HEAD` only. apt fetches; it does not write.
- Cache lifetime is the origin's: apt-proxy knows a distribution's index and
  package TTLs, but nothing about a third-party archive, so its
  `Cache-Control` is respected as-is.
- A distribution's own hosts keep distribution routing. If an allowlisted host
  matches a configured distribution's `host_pattern`, is one of its listed
  mirrors, or is the mirror currently selected for it, that distribution's
  mirror and cache rules apply to its paths. Any other allowlisted origin is
  fetched from the origin itself, including paths that start like a
  distribution's, such as `packages.microsoft.com/ubuntu/...`.
- A host that is not allowlisted is still routed by path alone:
  `http://<any host>/ubuntu/...` is served from the selected Ubuntu mirror.
  Credentials a proxy client sends for such a host are not forwarded to that
  mirror; see [private repositories](private-repositories.md).

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
transports ignore environment forward proxies. Probes do not follow redirects;
discovery redirects and the probes' HEAD-to-GET fallback remain guarded. Startup
and mirror refresh use the same per-instance policy. Restart to change policy.
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

### Immutable artifacts and upstream failures

The built-in Ubuntu, Ubuntu Ports and Debian rules give versioned pool artifacts
(`.deb`, `.udeb`, source artifacts) and `by-hash` paths the fallback policy
`max-age=100000, immutable`. For these paths a zero `max-age` in the request is
ignored, so APT's `by-hash` index requests are answered from the cache, and an
expired cached entry is still served while the mirror fails. Mutable indexes
such as `InRelease` keep `max-age=3600` and still need the mirror. Custom
distributions opt in by adding `immutable` to a rule's `cache_control`. See
[freshness and immutable artifacts](cache-storage.md#freshness-and-immutable-artifacts)
for the conditions, the limits and the relation to the backend TTL.

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
