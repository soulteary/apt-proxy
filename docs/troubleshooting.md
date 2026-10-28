# Troubleshooting and historical upgrades

[Project README](../README.md) · [中文](troubleshooting.zh-CN.md)

## Troubleshooting

### `403` on `HTTPS///` URLs

apt-proxy implements apt-cacher-ng's `HTTPS///` rewrite marker
(`deb http://HTTPS///example.com/repo ...`): it fetches `https://example.com/repo`
and caches it. The origin must be on the [passthrough
allowlist](repository-configuration.md#caching-third-party-archives-passthrough) first, which is what a
`403` means:

```bash
./apt-proxy --passthrough=get.docker.com
```

The refusal names the origin and the setting, so the message itself says what
to add. Other statuses from a marker URL:

| Status | Meaning |
|--------|---------|
| `403` | The origin is not on the allowlist |
| `400` | The marker names no origin (`.../HTTPS///` with nothing after it) |
| `405` | Not a `GET` or `HEAD` |

Both spellings apt-cacher-ng documents are accepted — `Host: HTTPS` with the
origin in the path, and the marker embedded in a path against apt-proxy's own
address — in any case.

### Routing changed after upgrading to v0.17.0

Before v0.17.0 a `distributions.yaml` was read **only** when its path was named
with `--distributions-config` / `APT_PROXY_DISTRIBUTIONS_CONFIG`. A file sitting
on one of the search paths was silently ignored, and SIGHUP reloaded nothing for
such a deployment. Both now behave as this README describes them — see
[Adding a distribution apt-proxy does not ship](repository-configuration.md#adding-a-distribution-apt-proxy-does-not-ship).

That is a fix, but it is visible on upgrade. A `distributions.yaml` left at
`./config/`, `./`, `/etc/apt-proxy/` or `~/.config/apt-proxy/` — an experiment, a
template copied out of this repository, a leftover from an older deployment —
takes effect the first time you start v0.17.0, with no change to your command
line. Entries whose `type` is `1`–`5` **reconfigure the built-in distribution**
of that type, so mirrors and `url_pattern` can move under you.

The startup log names the file in effect and everything that registered:

```text
config="config/distributions.yaml" distributions=["alpine","centos","debian","deepin","ubuntu","ubuntu-ports"]
```

`config="(built-in defaults)"` means no external configuration is in effect —
which is **not** the same as no file being found. A file that exists but cannot
be read, parsed or validated is rejected as a whole, and startup falls back to
the built-ins and logs that same value. The warning just above it is what tells
the two apart:

```text
failed to load distributions config; using built-in defaults  error="invalid distribution config for x: ..."
```

That warning appears *only* when a file was found and rejected, so its presence
is the answer: no warning means nothing was found; a warning means your file is
there and broken, and its `error` field says why. (It does not name the file
when the file came from the search path rather than being named explicitly —
check the four paths above in order.)

If a file you did not expect is named in `config=`, move or delete it, or name
the one you do want explicitly.

### `404` on a PPA or vendor repository

apt-proxy only proxies the distributions it is configured for. A path that
merely *contains* a distribution segment is a different repository, not a mirror
of that distribution, and returns `404`:

```text
/ppa.launchpad.net/deadsnakes/ppa/ubuntu/dists/jammy/InRelease   404
/download.docker.com/linux/ubuntu/dists/jammy/InRelease          404
```

Earlier versions matched those paths and answered them from the distribution's
own mirror, so a PPA request came back as the **main Ubuntu archive's** index
for that suite — `200`, valid-looking, and the wrong repository's content. The
`404` replaces that silent substitution.

What to do depends on how the client reaches apt-proxy.

**Using apt-proxy as APT's proxy** — `http_proxy=...`, or `Acquire::http::Proxy`,
which is the Quick Start setup: *every* request goes through apt-proxy, so
editing the `sources.list` entry changes nothing. The request still arrives here,
named by `Host`, and still `404`s. Bypass apt-proxy for that host instead:

```text
# /etc/apt/apt.conf.d/99-apt-proxy-bypass
Acquire::http::Proxy::ppa.launchpad.net "DIRECT";
Acquire::https::Proxy::ppa.launchpad.net "DIRECT";
```

**Using the URL-prefix form** — `deb http://apt-proxy.example:3142/<host>/...`:
point that entry at its origin instead.

**Either way**, if you would rather apt-proxy cached the repository than skipped
it, add the origin to the `passthrough` allowlist — see [Caching Third-Party
Archives](repository-configuration.md#caching-third-party-archives-passthrough):

```bash
./apt-proxy --passthrough=ppa.launchpad.net
```

That is the short path for an archive you only want cached as-is. Model it as a
distribution instead — see [Adding a distribution apt-proxy does not
ship](repository-configuration.md#adding-a-distribution-apt-proxy-does-not-ship) — when you want apt-proxy's
own cache rules, mirror list and benchmarking applied to it; in proxy mode that
entry needs a `host_pattern` matching the origin
(`host_pattern: "^ppa\\.launchpad\\.net$"`), since the request arrives with no
path prefix to match.

The apt-cacher-ng host-prefixed form is unaffected: a single host segment in
front of the distribution segment (`/ftp.uni-kl.de/debian/...`) still routes.

### Debug Mode

Enable debug logging to troubleshoot issues:

```bash
./apt-proxy --debug
```

### Debugging Package Operations

For detailed debugging of package manager operations (Ubuntu/Debian):

```bash
# Enable verbose debugging
http_proxy=http://192.168.33.1:3142 \
  apt-get -o Debug::pkgProblemResolver=true \
          -o Debug::Acquire::http=true \
          update

http_proxy=http://192.168.33.1:3142 \
  apt-get -o Debug::pkgProblemResolver=true \
          -o Debug::Acquire::http=true \
          install apache2
```

### Common Issues

**Issue**: Packages not being cached
**Solution**: Ensure the proxy URL is correctly configured and accessible from your client machines.

**Issue**: Slow first-time downloads
**Solution**: This is expected - the first download populates the cache. Subsequent downloads will be faster.

**Issue**: Cache appears empty after a restart, and everything downloads again
**Solution**: Cache entries are keyed by the elected mirror's URL, and mirror
election re-runs on restart. Pin the mirror (`--ubuntu=…`, `--debian=…`) to keep
the keys stable. See *Cache Keys and Mirror Selection*.

**Issue**: Cache directory growing too large
**Solution**: Configure cache limits with `--cache-max-size` or use the cleanup API endpoint.

### Upgrading to v0.18.0

Review integrity verification, Vary migration, download bounds, S3 temporary
space and shared-disk maintenance before upgrading. See the
[release and upgrade notes](../docs/releases/v0.18.0.md).
