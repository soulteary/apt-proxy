# Real APT client compatibility

Run `bash tests/apt-client/matrix.sh` with Docker and Go installed. The default
matrix uses Debian Bookworm/Trixie and Ubuntu 22.04/24.04. Pass image names to
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
performs signed offline update, installation and source downloads. No signature or TLS verification is disabled.

Logs and assertion evidence are written to `apt-client-evidence/` (override
with `APT_CLIENT_EVIDENCE_DIR`). Private signing keys and TLS private keys stay
inside the disposable container. CI runs all four clients independently and
uploads evidence even on failure. These are local archive compatibility tests;
they do not assert every public mirror or installer is compatible.
