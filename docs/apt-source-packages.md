# Debian-family source package coverage

Built-in and shipped `config/distributions.yaml` rules now recognize versioned
source artifacts for Ubuntu, Ubuntu Ports and Debian. This lets source package
requests reach the configured mirror rather than returning a proxy-side 404.

The recognized source pool files are:

- `package_version.dsc` descriptors;
- `package_version.orig.tar.{gz,bz2,lzma,xz}` upstream source archives;
- `package_version.orig-component.tar.{gz,bz2,lzma,xz}` additional source archives;
- `package_version.debian.tar.{gz,bz2,lzma,xz}` Debian packaging changes;
- `package_version.tar.{gz,bz2,lzma,xz}` native source archives;
- `package_version.diff.gz` legacy source differences;
- detached `.asc` signatures for original and component archives.

These file forms follow the documented [dpkg-source formats](https://manpages.debian.org/trixie/dpkg-dev/dpkg-source.1.en.html).
Routing accepts the named forms rather than inspecting the source descriptor's
Format field. It does not imply every extension is valid for every source format.

Rules require a Debian-style `pool/component/prefix/package/` layout (also the
legacy `pool/updates/component/prefix/package/` layout), a package/version
filename, and a supported complete suffix. Component archive names use letters,
digits and hyphens. Arbitrary archive downloads outside the pool, bare generic
tarballs, appended executable suffixes, unsupported compression and arbitrary
signature extensions are not enabled by this change.

Matched source artifacts receive the same fallback `max-age=100000` policy as
versioned binary packages, while `Sources` indexes retain their separate
`max-age=3600` policy. Explicit upstream Cache-Control or Expires, including no-store,
continues to take precedence. Existing private-request isolation, upstream
connection controls, cache length checks and interrupted-download handling also
apply to these paths. Cache migration is unnecessary.

Clients still need source repositories enabled (for example deb-src entries)
and valid repository metadata. This change adds routing/cache coverage, not
source-list configuration, dependency resolution, archive extraction, signature
or repository checksum verification. Existing custom distribution YAML files
must add the source rule themselves or adopt the updated shipped example;
they are not silently modified. Common installer boot files and manifests have separate
[installer rules](apt-installer-files.md); other repository artifacts are not
automatically enabled by the source pool rules.

Tests exercise built-in and shipped YAML rules across all three distributions,
negative suffix/path cases and actual daemon mirror rewriting, byte preservation,
cold/warm caching and upstream no-store behavior. The daemon fixtures simulate
artifact bodies; they do not replace a real APT/dpkg compatibility matrix.
