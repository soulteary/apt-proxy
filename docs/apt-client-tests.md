# Real APT client compatibility

Run `bash tests/apt-client/matrix.sh` with Docker and Go installed. The default
matrix uses Debian Bookworm/Trixie and Ubuntu 22.04/24.04/26.04. Pass image names to
select clients. Each container builds an isolated binary/source repository,
generates an ephemeral signing key and TLS certificate, and starts the actual
apt-proxy binary. Bootstrap tools are installed while building the image;
all repository tests use only the local signed fixture.

The suite exercises signed `apt-get update`, forced SHA256 by-hash, gzip path
fallback, `apt-get install`, source dependency downloads, byte-checked ranges,
503 retries and interrupted package transfers, and authenticated HTTPS on both
the client and upstream connections. An unauthenticated private update must
fail, as must a tampered InRelease signature. The fixture also prefetches a complete
binary/source snapshot, verifies it, exports/imports it, stops the origin, and
performs signed offline update, installation and source downloads. A normal
online reinstall follows `apt-get clean` and must leave the origin package GET
count unchanged; `cache-hit.json` records that proxy-cache proof and installed
payload verification. No signature or TLS verification is disabled.

Logs and assertion evidence are written to `apt-client-evidence/` (override
with `APT_CLIENT_EVIDENCE_DIR`). Private signing keys and TLS private keys stay
inside the disposable container. Relative evidence roots are resolved from the
caller's working directory before compilation. Client references whose derived
evidence identifiers collide are rejected before any evidence is cleared.
Identifiers use ASCII lowercase to detect tag collisions on case-insensitive
filesystems too.
Digest-pinned references, registry ports and IPv6 registry addresses are
normalized to Docker-safe identifiers. Long identifiers receive a bounded
128-character tag with a SHA256 suffix; the original client reference is still
passed unchanged to Docker's base-image argument. Duplicate references or
references whose normalized identifiers collide fail before Docker or cleanup.
The entrypoint regression checks run with `python3 tests/apt-client/matrix_test.py`
and run in ordinary CI without Docker.
The gzip phase records origin requests and requires both binary and source
indexes to use `.gz`, so an xz/plain fallback cannot pass as gzip coverage.
The [client compatibility workflow](client-compatibility.md) runs the selected
clients on manual requests, and all clients before a version release. It
uploads evidence even on failure. Ordinary PRs and main pushes run only the
fast harness regressions. These are local archive compatibility tests;
they do not assert every public mirror or installer is compatible.

This committed suite checks the normal daemon and explicit offline commands.
Enabling runtime health, shared disk coordination or custom resource limits
requires additional configuration and focused regressions; the client matrix
alone does not prove cross-instance coalescing or authorize snapshot failover.
