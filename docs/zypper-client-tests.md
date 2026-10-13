# Real openSUSE zypper repository-cache tests

Run `bash tests/zypper-client/matrix.sh` with Docker and Go installed. The
default clients are the official `opensuse/leap:16.0` and
`opensuse/tumbleweed:latest` images. Pass image references to select clients:

```sh
ZYPPER_CLIENT_EVIDENCE_DIR="$PWD/zypper-client-evidence" \
  bash tests/zypper-client/matrix.sh opensuse/leap:16.0
```

The suite compiles the current apt-proxy checkout for the Docker daemon's
architecture. The official images provide native Linux amd64 and arm64
variants. Bootstrap tools are installed from the image's public repositories
during Docker build. All subsequent repository tests use only a local fixture;
the container's ordinary repository and service definitions are removed.

Each disposable container creates a dependency-free noarch RPM containing a
deterministic 2 MiB payload, signs it with an ephemeral GPG key, generates
rpm-md metadata with `createrepo_c` and signs `repomd.xml`. The public key is
explicitly trusted by the container's RPM database. A local HTTPS origin uses
a generated certificate installed in openSUSE's system trust store. Signing
keys and TLS private keys remain inside the disposable container.

The client repository enables `gpgcheck`, `repo_gpgcheck` and `pkg_gpgcheck`.
No `--no-gpg-checks`, unsigned-package acceptance or TLS-verification bypass is
used. The fixture sets `keeppackages=1` so the cache-cleaning phase must remove
a retained client RPM before the second installation.

## Assertions

- Actual `zypper refresh --force` and `zypper install --no-recommends` verify
  signed rpm-md metadata and install the signed RPM. `rpm -V` and byte comparison
  verify the installed payload.
- `zypper clean --all` clears client metadata and packages, with an additional
  assertion that the client package directory contains no RPM files. A forced
  reinstall must succeed with **no additional origin RPM GET**, proving reuse
  by apt-proxy rather than zypper's local package cache.
- A fresh proxy/client cache serves valid XML with altered signed repomd bytes
  and the original signature. Native zypper refresh must fail, and no package
  request may reach the origin.
- Another fresh proxy/client cache restores trusted metadata while damaging
  one RPM payload byte without changing its length. Native zypper installation
  must reject the metadata-checksum/signature mismatch; earlier cached content
  cannot mask it. The existing installed payload remains intact.
- Restoring valid origin bytes and clearing both caches must permit another
  signed refresh and forced installation. This positive control distinguishes
  integrity rejection from a broken command, trust setup, route or TLS handshake.
- `strace` records zypper and descendant network connections for every phase.
  All observed Internet-family connections must target apt-proxy at
  `127.0.0.1:3142`. The independently running proxy contacts the HTTPS origin;
  zypper cannot bypass it by connecting directly to the origin.

Each traced command publishes its actual exit status atomically. The harness
stops its owned tracer after zypper completes, with a 60-second phase deadline,
so a retained GPG agent cannot hold the test open. Trace scratch files are
collected on the container filesystem, checked there, then exported as readable
evidence; this also avoids directory-enumeration restrictions on macOS mounts.

## Routing and evidence

The test reuses the built-in CentOS rpm-md route with an explicitly selected
HTTPS archive root. The client accesses `/centos/fixture/`, which is rewritten
to the corresponding local origin path. This exercises the production proxy
and cache without introducing openSUSE-specific product code or claiming that
CentOS and openSUSE repositories are interchangeable. A separate distribution
entry may be used for real openSUSE deployments.

The fixture enables `upstream_access` with one exact loopback host, its HTTPS
port and an explicit loopback CIDR exception. The HTTP control endpoint is used
only by the test runner. Package signatures and the origin TLS certificate are
verified through the normal client/proxy implementations.

Evidence is written below `zypper-client-evidence/`, or
`ZYPPER_CLIENT_EVIDENCE_DIR`, with one normalized directory per image. Relative
roots resolve from the caller's working directory. Docker-safe identifiers use
ASCII lowercase, bound long tags with a SHA256 suffix and reject collisions
before clearing prior evidence. Selected evidence is cleared before host build
or Docker operations so an old PASS cannot survive a failed attempt.

The seven lightweight entrypoint regression groups run with
`python3 tests/zypper-client/matrix_test.py`; they do not build images or contact
public repositories.

Each directory contains `result.txt`, `cache-hit.json`, origin request counters,
client/proxy configuration, logs, package-signature output, binary SHA256 and
connection traces. The cache report includes `first_origin_package_gets`,
`second_origin_package_gets`, `client_cache_cleared` and `payload_matches` for
the common compatibility runner, plus RPM-specific counters.

These tests validate an isolated signed rpm-md repository, normal zypper
transactions and apt-proxy cache reuse. They do not validate openSUSE's public
mirror synchronization, service plugins, release upgrades or every repository
metadata extension.
