# Verified client repository support

apt-proxy caches software repository metadata and package downloads. Configure
both the proxy's upstream repository and the client's repository URL so those
requests reach the proxy. A supported client protocol does not automatically
configure every distribution's public repositories.

## Evidence and version availability

On **2026-10-29 (Asia/Shanghai)**, all **18 Linux AMD64 client images** passed the
[v0.19.4 release gate](https://github.com/soulteary/apt-proxy/actions/runs/40728681270)
against proxy source commit
[`028d5a49c9ab1a1f338f0c7eca5243e602972651`](https://github.com/soulteary/apt-proxy/commit/028d5a49c9ab1a1f338f0c7eca5243e602972651).
The run's `client-summary-40728681270-1` artifact records all clients as passed,
with a successful matrix and overall evidence gate. Every client refreshed
signed repository metadata, installed and verified a package, cleared its own
cache and reinstalled without increasing origin package GET counts.
Family-specific integrity rejection and HTTPS verification checks also passed.

This is a pinned evidence snapshot. The proxy ran inside Linux AMD64 Docker
containers with the default disk cache and small dependency-free packages;
`all`, `noarch` or `any` package payloads do not establish native package or
installer coverage on every CPU architecture. Image tags and rolling releases
can change; exact package-manager versions belong to that run's individual
client logs, not to the image tag forever. Artifacts are retained for 30 days.

[v0.19.4](https://github.com/soulteary/apt-proxy/releases/tag/v0.19.4) contains
both the optional Metalink endpoint and the 18-client release gate, introduced
in v0.19.0. v0.18.0 predates them. Proxmox host/path routing requires v0.19.2;
its mirror-root URL-joining fix requires v0.19.3. See the
[v0.19.4 upgrade notes](releases/v0.19.4.md).

## Clients verified in the complete run

- **APT, 5 images:** Debian 12 Bookworm and Debian 13 Trixie; Ubuntu 22.04,
  24.04 and 26.04.
- **DNF/DNF5, 7 images:** CentOS Stream 9/10, Rocky Linux 9/10, AlmaLinux
  9/10 and Fedora 44. The native `yum` command was not exercised separately.
- **APK, 3 images:** Alpine 3.22, 3.23 and 3.24, covering APK2 and APK3
  clients consuming signed legacy packages and `APKINDEX.tar.gz`.
  Native ADB formats were not tested.
- **pacman, 1 image:** Arch Linux `archlinux:base`, with signed repository
  databases and packages.
- **zypper, 2 images:** openSUSE Leap 16.0 and Tumbleweed.

Exact image references are maintained in
[`clients.json`](../tests/client-compatibility/clients.json). Individual client
logs, `cache-hit.json`, `report.json`, and the workflow summary record the
source commit, image, run and attempt. For the earlier 2026-10-13 snapshot,
see [its original run](https://github.com/soulteary/apt-proxy/actions/runs/38382735057);
do not transfer its package-manager version numbers to the newer release run.

## Repository configuration required

### APT

Use the built-in Ubuntu, Ubuntu Ports or Debian routes with the matching
archive root. Configure the client's sources or its HTTP proxy so the request
matches that route; select the Debian Security root separately where needed.
Keep normal repository signatures, expiry and package checksum checks enabled.
APT verifies signed Release/InRelease metadata and the package hashes it
authenticates; these `.deb` fixtures do not carry independent GPG signatures.
The complete client suite used the built-in `/ubuntu/...` mapping with an
explicit local signed archive for all five APT clients; it did not contact
those distributions' live mirrors during assertions.

APT additionally passed source downloads, SHA256 by-hash, gzip index fallback,
byte ranges, 503/interrupted-transfer recovery, authenticated HTTPS and signed
offline snapshot update/install/source operations. See
[APT client tests](apt-client-tests.md) and [offline snapshots](repository-offline.md).
The full snapshot commands parse APT Release/Packages/Sources metadata; this
result does not establish RPM, APK or pacman offline repository support.

### DNF and DNF5

For a fixed rpm-md repository, select its actual archive root and point the
client's `baseurl` at the proxy's mapped path. The built-in `/centos/...` route
can be configured for an explicit RPM archive; its name does not select Fedora,
Rocky or Alma repositories automatically. Disable competing `metalink` or
`mirrorlist` options when selecting a fixed `baseurl`.

For dynamic discovery, enable the optional Metalink endpoint, select the
provider's `source_url`, and explicitly allow the mirror hosts it returns.
Keep the appropriate repository query and native architecture/release variables,
and use the proxy's `metalink=` URL so the rewritten metadata and package links
stay in the proxy cache. Each proxy instance has one configured Metalink source.
Providers must return the supported Metalink v3 document and accepted query
parameters; a text `mirrorlist` is not transformed by this endpoint.
The endpoint serves public repository GET/HEAD requests and rejects credentials,
cookies and token/tenant headers; authenticated discovery requires separate
support rather than reusing this public Metalink route.

CentOS Stream preserves native `$stream` and `$basearch`. Fedora, Rocky and Alma
passed the same protocol with synthetic local repository IDs and their native
`$releasever`; these fixtures do not certify those vendors' default discovery
URLs or repository files. The default Metalink provider is CentOS, so another
provider needs explicit configuration. A generic DNF `proxy=` setting alone
is not evidence of HTTPS package caching; configure the repository URLs and
upstreams as above.

DNF/DNF5 also passed verified fallback from a failing preferred mirror,
Metalink variable expansion and metadata digest rejection. See
[Metalink configuration](centos-metalink.md) and [DNF client tests](dnf-client-tests.md).
Keep metadata/package GPG checks enabled. This suite does not validate native
YUM clients, vendor mirrorlist rewriting or RHEL subscription authentication.

### APK

Select the Alpine upstream root and direct the client's `/etc/apk/repositories`
through the corresponding `/alpine/...` proxy route, including the branch,
component and architecture. APK2 and APK3 passed with verified HTTPS, a signed
legacy index and package, and fresh client caches. Repository installation
checks the package identity protected by the signed index; a separate native
`apk verify` control checks the embedded package signature. Both damaged-index
and damaged-package controls were rejected. See [APK client tests](apk-client-tests.md).

### pacman

Supply an explicit distribution mapping and point `pacman.conf`'s repository
`Server` at the mapped proxy URL, preserving the architecture path. The suite
replaced the Alpine routing slot with a bounded `/pacman/...` fixture mapping;
this is configured support, not a built-in Arch mode or automatic mirror list.
Both database and package signatures remained required. The proven formats are
a gzip repository database, a `.pkg.tar.zst` package and
PGP signatures; other archive suffixes in a mapping are not additional test
results. See [pacman client tests](pacman-client-tests.md) and
[custom distribution examples](../examples/custom-distros/).

### zypper

Supply an explicit rpm-md archive mapping and point the client's repository
`baseurl` at it. The suite selected a local HTTPS archive root behind the
built-in `/centos/...` routing slot; there is no built-in openSUSE mirror mode.
Keep both metadata and package GPG checks enabled. Valid installs, cache reuse,
damaged metadata/package rejection, and restoration all passed. See
[zypper client tests](zypper-client-tests.md).

## Capability boundaries

The common proof for all 18 clients is online repository refresh, package
installation with the family's signature/hash trust checks, payload verification
and proxy cache reuse after clearing the client cache. HTTPS phases preserve
certificate verification. DNF, APK and
zypper also record client connections that must reach the proxy only.

Additional APT source/offline coverage is specific to APT. The
[`mirror_health`](runtime-mirror-health.md) and
[shared by-hash repository identities](shared-repository-cache.md) features
have their own Debian-family configuration and regressions; this client matrix
does not establish those capabilities for RPM/APK/pacman repositories, shared
multi-instance deployments, or every resource/storage configuration. The full
18-client run used disk storage, not an 18-client S3 matrix.

Public mirror availability and synchronization, stock repository discovery,
complete distribution upgrades and dependency graphs, installation media, and
private RPM/APK/pacman repositories require separate validation. Linux Mint,
Deepin and Armbian examples or shared APT protocols do not make those clients
independently verified. Homebrew, Winget, Chocolatey and FreeBSD `pkg` were not
part of this suite.

## Proxy host platforms and ongoing verification

Daemon binaries are built for Linux and macOS, and container images run Linux;
see the [daemon platform list](../README.md#proxy-daemon-platforms). That list
answers where the proxy process can run. It does not establish Homebrew caching
on macOS or a client matrix on every published CPU architecture. Windows and
FreeBSD are outside the configured daemon release targets.

The complete suite runs manually or before version publication; ordinary PRs
run fast harness regressions and the existing Go checks. Failures or missing
cache evidence block the release gate. See [execution and evidence](client-compatibility.md).
When expanding support, add a real client target and appropriate repository
fixture, then update this evidence snapshot after a successful run.
