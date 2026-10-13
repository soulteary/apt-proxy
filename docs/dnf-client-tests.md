# Real DNF Metalink compatibility

Run `bash tests/dnf-client/matrix.sh` with Docker and Go installed. The default
matrix uses the official `quay.io/centos/centos:stream9` and `:stream10` images.
Pass image references to select clients. The suite compiles the current proxy
for the Docker daemon's architecture; CI executes both clients on x86_64.

Each disposable container builds a dependency-free noarch RPM, signs it with
an ephemeral GPG key, creates rpm-md metadata with `createrepo_c`, and signs
`repomd.xml`. It also generates a local TLS certificate and adds it to the
container's system trust store. Bootstrap tools use public repositories during
the image build. Every compatibility test subsequently disables all repositories
except the local fixture. Signing keys and TLS private keys remain inside the
disposable container.

The fixture serves a Metalink v3 document with a SHA256 digest, size, and two
certificate-verified HTTPS mirror URLs. The actual DNF client uses a proxy
`metalink=` URL containing native `$stream` and `$basearch` variables and the
official repository's `countme=1` setting. The
source checks the expanded query values, including `protocol=https,http`.
Discovery responses use `Cache-Control: no-cache`, exercising dynamic list
refreshes while identical repository packages remain reusable in the proxy.

The suite requires:

- Real `dnf makecache` and RPM installation with `gpgcheck=1`,
  `repo_gpgcheck=1`, and `sslverify=1`.
- A preferred mirror returning HTTP 503 followed by a matching secondary
  mirror. Rewritten Metalink resource URLs must all point to apt-proxy.
- Byte-for-byte installed payload verification and a successful `rpm -V`.
- A clean DNF cache followed by reinstalling the package without an additional
  origin RPM GET, demonstrating apt-proxy's cache rather than DNF's own cache.
- A fresh proxy cache with a deliberately incorrect advertised repomd digest.
  DNF must fail and the origin must receive no child metadata or RPM requests.
- `strace` evidence that DNF and its descendants connect only to the proxy's
  loopback port. The separately running proxy contacts the Metalink source and
  HTTPS mirrors; the client never bypasses it.

The fixture enables `upstream_access` with an exact loopback host, two explicit
ports, and a loopback CIDR exception inside the container. It does not disable
TLS, Metalink checksums, metadata signatures, or package signatures.

Evidence is written to `dnf-client-evidence/`, or the directory selected by
`DNF_CLIENT_EVIDENCE_DIR`. Each client receives a separate evidence directory.
Relative paths are resolved from the caller's working directory, and image
identifiers use ASCII lowercase Docker-safe tags with a SHA256 suffix for long
references. Colliding identifiers are rejected before previous evidence is
cleared. Entrypoint regression checks run with
`python3 tests/dnf-client/matrix_test.py`.

CI runs Stream 9 and 10 independently and uploads logs, syscall traces, Metalink
XML, cache counters, and rejection evidence even when a test fails. These tests
validate an isolated Metalink/rpm-md repository; they do not assert availability
or synchronization of every public CentOS mirror.
