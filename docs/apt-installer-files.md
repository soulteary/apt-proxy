# Debian-family installer file coverage

Built-in and shipped YAML rules for Debian, Ubuntu and Ubuntu Ports recognize
common files below `dists/<suite>/<component>/installer-<architecture>/<version>/images/`.
The version can be `current` or a dated installer version. The configured mirror
must actually publish these paths; this does not restore installer trees removed
from newer Ubuntu releases.

Supported paths relative to `images/`:

- `MD5SUMS`, `SHA256SUMS`, `MANIFEST`, `MANIFEST.udebs`, and `udeb.list`;
- `netboot/netboot.tar.gz`, `netboot/mini.iso`, and `netboot/pxelinux.0`;
- `netboot/{debian,ubuntu}-installer/<architecture>/{linux,initrd.gz}`,
  including the corresponding `netboot/gtk/` paths;
- `{hd-media,cdrom}/{vmlinuz,initrd.gz}`, including their `gtk/` paths;
- `hd-media/boot.img.gz`.

These names can be inspected in the [Debian installer image directory](https://deb.debian.org/debian/dists/trixie/main/installer-amd64/current/images/).
The rules require the complete installer hierarchy and an exact supported filename.
They do not allow arbitrary ISO/tarball downloads, PXE configuration directories,
EFI loaders, signature files, or all architecture-specific installer artifacts.
Consequently, this subset alone does not promise a complete PXE or offline installation.

All matched files use fallback `max-age=3600`, including dated directories.
`current` is mutable, so the longer binary/source package fallback is inappropriate.
Explicit upstream Cache-Control still takes precedence, including `no-store`.
Existing private-request isolation, connection policy and cache length checks apply.
No cache migration is needed. Existing custom distribution YAML must explicitly
adopt these two rules from the updated shipped example.

Files are cached individually. This change does not atomically refresh a whole
installer tree, compare image bytes against checksum manifests, or verify signatures.
Clients must perform integrity/authenticity checks and handle upstream updates;
serving a manifest does not by itself authenticate the boot files.

Tests cover all three distributions and both rule sources, exact path/suffix
boundaries, and daemon rewriting, byte preservation, cold/warm cache behavior,
and upstream `no-store`. Mock boot bytes are not a real installer compatibility test.
