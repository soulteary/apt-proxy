# Verified client repository support

apt-proxy caches software repository metadata and package downloads. Configure
both the proxy's upstream repository and the client's repository URL so those
requests reach the proxy. A supported client protocol does not automatically
configure every distribution's public repositories.

## Evidence and version availability

On **2026-10-13**, all **18 Linux AMD64 client images** passed the
[complete Docker end-to-end run](https://github.com/soulteary/apt-proxy/actions/runs/38382735057)
against proxy source commit
[`6a8f683ba7cb5d470329d84676ac5351da5f182f`](https://github.com/soulteary/apt-proxy/commit/6a8f683ba7cb5d470329d84676ac5351da5f182f).
The suite was merged in [PR #159](https://github.com/soulteary/apt-proxy/pull/159).
Every client refreshed repository metadata and installed a native package with
its protocol's signature and hash checks enabled, verified its payload, cleared
its own cache, and reinstalled. Origin package
GET counts remained **1 → 1**, demonstrating reuse of apt-proxy's cache.
Family-specific integrity rejection and HTTPS verification checks also passed.

This is a pinned evidence snapshot. The proxy ran inside Linux AMD64 Docker
containers with the default disk cache and small dependency-free packages;
`all`, `noarch` or `any` package payloads do not establish native package or
installer coverage on every CPU architecture. Version tags and rolling images
can change; the recorded versions below describe this run. A newer manifest
entry becomes verified only after its own successful run.

[v0.18.0](https://github.com/soulteary/apt-proxy/releases/tag/v0.18.0) was
published on 2026-10-13 from commit `1cd3f766f3fddce8eb574c1a7561c50a90abcc3f`.
The optional Metalink endpoint ([PR #158](https://github.com/soulteary/apt-proxy/pull/158))
and expanded 18-client acceptance suite were added afterward. The results here
verify the named main-branch source, not the older v0.18.0 artifacts. Use a build
or release containing those changes when following Metalink instructions;
consult the release's own notes and source commit for availability.

## Clients verified in the complete run

- **APT, 5 images:** Debian 12 Bookworm (`debian:bookworm-slim`, APT 2.6.1),
  Debian 13 Trixie (`debian:trixie-slim`, APT 3.0.3), Ubuntu 22.04
  (APT 2.4.14), Ubuntu 24.04 (APT 2.8.3), and Ubuntu 26.04 (APT 3.2.0).
- **DNF/DNF5, 7 images:** CentOS Stream 9, Rocky Linux 9 and AlmaLinux 9
  use DNF 4.14.0; CentOS Stream 10, Rocky Linux 10 and AlmaLinux 10 use DNF 4.20.0. Fedora 44
  uses DNF5 5.4.6.0. The native `yum` command was not exercised separately.
- **APK, 3 images:** Alpine 3.22 uses APK 2.14.12; Alpine 3.23 and 3.24 use
  APK 3.0.8-r0. Both client generations consume signed legacy APK packages
  and `APKINDEX.tar.gz` in this suite; native ADB formats were not tested.
- **pacman, 1 image:** Arch Linux `archlinux:base`, pacman 7.1.0 and
  libalpm 16.0.1, with signed repository databases and packages.
- **zypper, 2 images:** openSUSE Leap 16.0 and Tumbleweed, both zypper
  1.14.101. The Tumbleweed image reported snapshot `20261011`.

Exact image references are maintained in
[`clients.json`](../tests/client-compatibility/clients.json). Individual client
logs, `cache-hit.json`, `report.json`, and the workflow summary record the
source commit, image, run and attempt. Artifacts are retained for 30 days.

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
