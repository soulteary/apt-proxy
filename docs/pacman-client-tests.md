# Real pacman repository compatibility

Run `bash tests/pacman-client/matrix.sh` with Docker and Go installed. The default
client is the official `archlinux:base` image. The runner builds a Linux x86_64
proxy and uses Docker's `linux/amd64` platform, including on ARM hosts where an
emulator is available. The evidence records the actual rolling pacman version.

An unprivileged builder creates a noarch data-only package using `makepkg` and
signs it with a temporary GPG key. `repo-add` produces a signed repository
database. The public key is imported and locally trusted by `pacman-key`, and
the client requires both database and package signatures with `SigLevel = Required`.
See the official [makepkg documentation](https://man.archlinux.org/man/makepkg.8)
and [pacman signature policy](https://man.archlinux.org/man/pacman.conf.5).

The fixture supplies a `distributions.yaml` mapping with bounded database,
package, and signature suffix rules. It replaces the selected Alpine routing
slot with `/pacman/<architecture>/...`; `mode: alpine` selects that explicit
operator mapping. The client has only one repository URL, pointing to the
proxy. A fixture CA is installed into the container's system trust store, and
both client-to-proxy and proxy-to-origin requests use verified HTTPS.

The real client synchronizes the signed database and installs the package, then
compares its installed payload with the fixture source. It removes the package,
clears the entire client package and sync database caches, and reinstalls. The
origin package GET counter must remain unchanged. Negative phases use fresh
proxy caches: an altered detached database signature must fail synchronization,
and an altered package signature embedded in a newly signed valid database must
fail installation. TLS and signature verification remain enabled.

Evidence is written beneath `pacman-client-evidence/`, or
`PACMAN_CLIENT_EVIDENCE_DIR`, with a separate Docker-safe directory per image.
It includes native versions, origin counters, `cache-hit.json`, required client
signature policy, custom routing YAML, negative logs, and `result.txt`. Private
keys remain inside the disposable container. Bootstrap installs tools from
public repositories before the fixture replaces the client's repositories.

Run `python3 tests/pacman-client/matrix_test.py` for lightweight checks with
stubbed Docker and Go. These cover identifiers, collisions, and removal of
stale evidence before build failures. Native x86_64 CI provides coverage when
the local Docker daemon cannot emulate the official image. These tests validate
the named local repository mapping and selected client, rather than arbitrary
public Arch mirror layouts.

The [shared compatibility guide](client-compatibility.md) documents the unified
[manual and release workflow](../.github/workflows/client-compatibility.yml).
The workflow's native x86_64 runner avoids the download-sandbox restrictions
that can prevent the official image's bootstrap under ARM emulation.
