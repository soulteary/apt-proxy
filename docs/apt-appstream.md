# Debian-family AppStream metadata coverage

Built-in and shipped YAML rules for Debian, Ubuntu and Ubuntu Ports recognize
direct DEP-11 metadata paths under `dists/<suite>/<component>/dep11/`:

- `Components-<architecture>.yml.gz` and `.yml.xz`;
- `CID-Index-<architecture>.json.gz`;
- `icons-{48x48,64x64,128x128}.tar.gz`, including the corresponding `@2` variants.

These files are published in the [Debian DEP-11 directory](https://deb.debian.org/debian/dists/trixie/main/dep11/).
Direct URLs now reach the configured mirror when clients disable by-hash or fall
back to the named file. Existing by-hash routing is unchanged. Architecture names
use letters, digits, underscores and hyphens, starting with a letter or digit.

Matched files receive fallback `max-age=3600`; explicit upstream Cache-Control or Expires
still takes precedence. Private-request isolation, upstream connection controls
and download completeness checks continue to apply. The complete hierarchy and
supported filename are required; arbitrary YAML/JSON files, nested files, unknown
icon sizes and arbitrary tarballs are not enabled.

Existing custom distribution YAML must adopt the new rule explicitly from the
shipped `config/distributions.yaml`. Files are cached independently; this does not
parse AppStream records or verify repository signatures/checksums.

Tests cover built-in and shipped rules across all three distributions, negative
path/suffix boundaries, daemon rewriting, unchanged body bytes, cold/warm cache
behavior and upstream `no-store`. Fixtures use mock metadata and do not establish
compatibility with every desktop package manager.

```sh
go test -race ./internal/distro ./internal/cli -run 'Test(DebStyleAppStreamCoverage|ProductionAppStreamFilesRewriteAndCache)' -count=3
```
