# Verified repository snapshots and offline service

`apt-proxy repository` maintains a separate filesystem snapshot store. Select a
repository URL, suite, components and binary architectures explicitly:

```sh
apt-proxy repository prefetch \
  --url https://deb.debian.org/debian --suite bookworm \
  --components main --architectures amd64 --sources \
  --dir /srv/apt-snapshot
apt-proxy repository verify --dir /srv/apt-snapshot
apt-proxy repository serve --dir /srv/apt-snapshot --listen 127.0.0.1:3143
```

Use a dedicated directory owned by the service account. Symlinks in the store
path, metadata, object files or store subdirectories are rejected. On macOS use
the physical `/private/tmp/...` path instead of the `/tmp` symlink for temporary
stores.

The offline endpoint exposes repository-relative paths at its root. An upstream
URL ending in `/debian` therefore serves `/dists/bookworm/...` and `/pool/...`.
For example, a client source can use:

```text
deb [signed-by=/usr/share/keyrings/debian-archive-keyring.gpg] http://127.0.0.1:3143 bookworm main
deb-src [signed-by=/usr/share/keyrings/debian-archive-keyring.gpg] http://127.0.0.1:3143 bookworm main
```

Replace `127.0.0.1` with the reachable server address for a remote client. The
server supports GET, HEAD, conditional requests and byte ranges. Unselected
paths return 404. It never fetches from the upstream repository.

## What complete means

The snapshot preserves the raw `InRelease` file, or `Release` plus
`Release.gpg` when the signature is available. The SHA256 table in Release selects
`Packages`, `Packages.gz`, and `Packages.xz` for every requested component and
architecture; separate `binary-all` indexes are included when advertised. With
`--sources`, the corresponding Sources variants are also required. Every
supported advertised variant is parsed, so the selected compression format
cannot expose a missing package dependency.

All indexes must match their declared sizes and SHA256 digests. Every Packages
`Filename`/`Size`/`SHA256` object and every Sources
`Directory`/`Checksums-Sha256` reference, including `.dsc` and source archives,
is then fetched and verified. SHA256 by-hash aliases for selected indexes are
provided. Optional translation, Contents, AppStream and installer indexes are
outside this selected package/source snapshot. Clients can disable optional
translation downloads with `Acquire::Languages=none`.

A complete set contains every package listed by the selected indexes, rather
than only the dependency closure of a particular requested package. This may be
large. The signed repository's dependency relationships are not rewritten or
resolved; a package depending on another suite/component/architecture requires
that repository selection too. This version stores one suite/selection per
active snapshot, so use separate directories/endpoints for other selections.

The proxy checks transport/object integrity, not OpenPGP signatures. APT remains
responsible for trust, signature, repository identity and Valid-Until checks.
The proxy does not edit signed metadata or extend expired Release validity.
Unsigned repositories still require the client's explicit trust decision.

## Parsing and resource limits

Plain and gzip indexes use Go readers. Parsing a selected `.xz` variant requires
the system `xz` executable in `PATH`. The process uses no shell, a 256 MiB
decompression memory limit and the same bounded output size as other index
formats. An unavailable executable, malformed compression, oversized index or
missing declared object fails the operation without publishing a partial set.

Install `xz-utils` on Debian/Ubuntu or `xz` on Alpine before using snapshots whose
Release advertises selected xz indexes. Existing distroless images do not ship
this executable; use the host binary or an image with xz for these commands.

The optional offline image includes xz and CA certificates:

```sh
docker build -f docker/Dockerfile.offline -t apt-proxy-offline .
mkdir -p snapshot
docker run --rm --user "$(id -u):$(id -g)" \
  -v "$PWD/snapshot:/var/lib/apt-proxy/repository" apt-proxy-offline \
  repository prefetch --url https://deb.debian.org/debian --suite bookworm \
  --components main --architectures amd64 --sources \
  --dir /var/lib/apt-proxy/repository
docker run --rm --user "$(id -u):$(id -g)" -p 3143:3143 \
  -v "$PWD/snapshot:/var/lib/apt-proxy/repository:ro" apt-proxy-offline
```

The host directory must be writable by the selected UID for prefetch, import
and repair. Serving works with a read-only bind mount of an existing store.

All commands accept these bounds; zero selects the default:

- `--max-file-bytes`: 2 GiB per downloaded/imported object.
- `--max-total-bytes`: 128 GiB of unique objects in one snapshot.
- `--max-index-bytes`: 128 MiB per decompressed index or bootstrap metadata file.
- `--max-manifest-bytes`: 16 MiB of serialized manifest.
- `--max-objects`: 1,000,000 manifest paths, including by-hash aliases.

These limits apply to the explicit snapshot operation, independently of the
daemon's download/spool limits. Historical generations and verified objects
from failed operations are retained for reuse; the total-byte limit does not
cap accumulated historical disk usage. Retire a dedicated store directory when
its older generations are no longer needed. Fetching has a five-minute timeout
per HTTP request. URLs may not contain credentials, query strings, fragments or
escaped paths. Redirects must remain at the same scheme and host and may not
introduce those URL fields. Private authenticated repositories are not supported
by this explicit snapshot command.

## Verify and repair

```sh
apt-proxy repository verify --dir /srv/apt-snapshot
apt-proxy repository repair --dir /srv/apt-snapshot
apt-proxy repository repair --dir /srv/apt-snapshot \
  --paths pool/main/h/hello/hello_2.10-3_amd64.deb
```

Verification hashes every unique object, re-reads the Release/index chain and
checks that the manifest includes exactly the selected complete set. Repair
uses the manifest's repository origin and expected digests, fetching only
missing or damaged objects. `--paths` optionally restricts it to comma-separated
manifest paths. A repair is successful only when the whole set verifies; healthy
objects are not downloaded again. Changed upstream content cannot silently
replace the pinned object: its expected digest must still match.

Serving starts only after the full selected set verifies. Each requested object
is checked again before serving; a damaged object returns 503. A running server
pins its starting manifest. Restart it after publishing a new snapshot to change
the active selection. The store must be protected from unrelated processes
editing its immutable files in place.

## Export and import

```sh
apt-proxy repository export --dir /srv/apt-snapshot --file snapshot.tar
apt-proxy repository import --dir /srv/apt-imported --file snapshot.tar
```

Export first verifies the entire set and writes a manifest-first tar archive
with one regular file per unique SHA256 object. The CLI atomically replaces the
destination archive only after successful completion. Import accepts only the
declared regular files and checks every digest, size, bound and metadata reference
before switching the current snapshot. Traversal names, symlinks, duplicate or
extra entries, missing objects and corrupt payloads fail closed. The archive
contains repository URLs without credentials; it provides integrity validation,
not an authenticated provenance signature.

Publication writes synced immutable objects and a hash-addressed manifest before
atomically replacing `current`. A failed prefetch/import keeps the previous
published selection available. Object repair uses atomic file replacement, so
existing readers retain their original open file.
