# Client repository cache compatibility

The Docker end-to-end suite validates apt-proxy as a cache for **client software
repositories**. It runs real package managers against disposable signed
repositories through the current proxy binary. It does not test whether the
proxy daemon runs on every client operating system.

For the completed 18-client evidence snapshot, exact client versions,
configuration prerequisites and capability boundaries, see
[verified client support](client-support.md). This page describes how to run
and maintain the suite.

## When to run it

In GitHub Actions, select **Client repository cache compatibility → Run
workflow**, choose a branch or tag, and select `all`, `apt`, `dnf`, `apk`,
`pacman`, or `zypper`. The selected family uses only committed, allowlisted
images; the workflow cannot run an arbitrary image supplied by an input.

The **Release** workflow calls the complete suite on a version tag (`v*`) and
on a manual Release run. Every client must pass before the release build,
container registry logins, and GoReleaser can run. A manual Release run on a
branch validates the full matrix and release preflight without publishing a
version. Version publication still requires a tag.

Ordinary pull requests and main-branch pushes do not run the Docker client
matrix. CI retains fast planner/evidence and shell-entrypoint regressions,
Go checks, and the existing integration tests. To validate an entire branch
without publishing, manually run **Release** on that branch; its reusable client
workflow executes from the same commit.

## Selected clients

`tests/client-compatibility/clients.json` is the shared matrix source:

- APT: Debian Bookworm and Trixie; Ubuntu 22.04, 24.04 and 26.04.
- DNF/DNF5: CentOS Stream 9/10, Fedora 44, Rocky Linux 9/10 and AlmaLinux 9/10.
- APK: Alpine 3.22, 3.23 and 3.24, covering native APK2 and APK3 clients.
- pacman: the official Arch Linux `archlinux:base` image.
- zypper: openSUSE Leap 16.0 and Tumbleweed.

The cloud matrix runs all 18 images on Linux AMD64 Docker hosts. Local family
entrypoints compile the proxy for the Docker daemon's architecture; use a
supported client image for that architecture. Arch's official image requires
the Linux AMD64 platform; this run used native AMD64, and ARM-host emulation
is not part of the verified complete matrix. Version tags and rolling images
can change upstream, so evidence
identifies the requested image and exact tested proxy source commit. A matrix
entry is a test target, not a compatibility claim until its run succeeds.

## What success means

Every family must complete all of these checks:

1. Refresh repository metadata and install a small native package using the
   real client with its protocol's signature and hash trust checks enabled.
   APT verifies signed metadata and authenticated package hashes rather than
   independent GPG signatures on these `.deb` files.
2. Verify the installed payload byte for byte.
3. Clear the client's package cache and reinstall. The origin's package GET
   count must stay unchanged, and must have been nonzero for the first install.
   This demonstrates proxy caching rather than reuse of the client's cache.
4. Reject corrupted repository/package signatures or mismatched discovery
   hashes using fresh caches, as appropriate to the family protocol.
5. Verify TLS for HTTPS phases and retain the fixture's client/origin/proxy logs.

APT additionally exercises source downloads, by-hash and gzip indexes, byte
ranges, retry/interruption, authenticated HTTPS and an offline snapshot. DNF
exercises native repository-variable expansion, rewritten Metalink, verified
mirror fallback and metadata digest rejection. Family implementation details:
[APT](apt-client-tests.md), [DNF](dnf-client-tests.md),
[APK](apk-client-tests.md), [pacman](pacman-client-tests.md),
[zypper](zypper-client-tests.md).

Bootstrap tools and base images are downloaded from public repositories during
Docker image builds. The actual assertions then use only the local fixture;
external repositories cannot hide a cache miss. Ephemeral signing/TLS private
keys remain inside disposable containers.

## Evidence and reruns

Each client uploads logs, a `result.txt` success stamp, `cache-hit.json` origin
counters, and `report.json` containing the client image, family, source commit,
workflow run and attempt. Artifacts are retained for 30 days, including failures.
The workflow summary includes per-client results and a machine-readable
`summary.json`.

The release gate requires the matrix job itself to succeed and rejects missing
reports, invalid cache counters, failed clients, and evidence for another source
commit or run. A newer job that fails before writing evidence cannot be masked
by an earlier successful artifact. On **Re-run failed
jobs**, it combines the latest attempt for each client with unchanged successful
jobs from that same workflow run and commit. A failed newer attempt cannot be
masked by an older successful report. Matrix jobs run independently with a
maximum concurrency of four; one failure does not cancel other evidence.

## Run locally

Install Docker, Go (the version in `go.mod`) and Python 3. From the repository:

```bash
python3 tests/client-compatibility/client_matrix.py plan --family apt
python3 tests/client-compatibility/client_matrix.py run ubuntu-2604
```

The unified runner accepts IDs from the manifest and writes to
`client-compatibility-evidence/<id>/`. It requires both a complete success
stamp and cache-hit evidence. Family entrypoints also accept image references:

```bash
bash tests/apt-client/matrix.sh ubuntu:26.04
bash tests/dnf-client/matrix.sh quay.io/fedora/fedora:44
bash tests/apk-client/matrix.sh alpine:3.24
bash tests/pacman-client/matrix.sh archlinux:base
bash tests/zypper-client/matrix.sh opensuse/leap:16.0
```

Run fast regressions without Docker:

```bash
python3 tests/client-compatibility/client_matrix_test.py
for family in apt dnf apk pacman zypper; do
  python3 "tests/$family-client/matrix_test.py"
done
```

## Scope of the result

A successful run demonstrates signed repository downloads, package installation
and proxy cache reuse for those concrete clients against local repositories.
It does not certify every public mirror, live mirror synchronization, all
installation media, RHEL subscription authentication, or every distribution
and operating system. Derivatives such as Linux Mint, Deepin and Armbian can
share package-manager protocols, but require their own image/fixture evidence
to appear as independently verified clients. Homebrew, Winget and Chocolatey
are outside this Linux repository matrix.
