# Real APK repository compatibility

Run `bash tests/apk-client/matrix.sh` with Docker and Go installed. The default
clients are the official `alpine:3.22`, `alpine:3.23`, and `alpine:3.24` images.
Pass image references to select clients. The runner builds the proxy for the
Docker daemon's architecture and records each image's actual APK version.

Each disposable container uses `abuild` to create a data-only noarch APK,
creates an `APKINDEX.tar.gz`, and signs the package and index with an ephemeral
OpenSSL RSA key. The APKBUILD contains only a locally generated payload. Its
build uses abuild's explicit root flag and preinstalled build tools inside the
disposable container. Dependency installation is skipped for the local recipe;
source checksum verification and signing still run. The noarch package's index
architecture is rewritten to the client's architecture, matching its repository
directory. The fixture public key is installed into APK's trust directory. This tests the
signed legacy package/index format used by Alpine repositories, with both APK2
and APK3 clients; native ADB package/index formats require separate coverage.
See Alpine's [packaging guide](https://wiki.alpinelinux.org/wiki/Building_packages)
and [APK format compatibility notes](https://wiki.alpinelinux.org/wiki/Apk).

The container adds a fixture certificate to its system trust store. Both the
client-to-proxy and proxy-to-origin connections use verified HTTPS. The proxy
uses its Alpine mirror mapping for `/alpine/fixture/main/<architecture>/` and
an exact loopback upstream policy. Every client test phase uses repository
configuration containing only the local fixture.

The suite requires signed `apk update` and package installation, followed by
byte-for-byte payload verification. It removes the package and all client cache
files, updates again, and reinstalls. The fixture's origin package GET counter
must remain unchanged, proving a proxy cache hit. A fresh proxy cache is used
for each negative phase. Corrupting only a signature member while retaining
valid gzip framing must cause index update and package installation to fail.
Client signature and certificate checks remain enabled throughout. Syscall
traces also require every APK network connection to use the proxy port.

Evidence is written beneath `apk-client-evidence/`, or
`APK_CLIENT_EVIDENCE_DIR`, with separate Docker-safe image identifiers. It includes
client versions, origin request counters, `cache-hit.json`, signature rejection
logs, connection traces, proxy configuration, and `result.txt`. Private signing
keys and certificate keys remain inside the disposable container.

Run `python3 tests/apk-client/matrix_test.py` for lightweight entrypoint checks.
These stub Docker and Go, and cover image identifiers, collisions, and cleanup
of previous evidence before build failures. They require no Docker daemon.
Coverage concerns the local signed fixture and the selected client versions.

The [shared compatibility guide](client-compatibility.md) describes the unified
[manual and release workflow](../.github/workflows/client-compatibility.yml),
including evidence retention for each selected client.
