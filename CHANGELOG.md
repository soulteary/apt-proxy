# Changelog

## v0.20.0 - 2026-11-14

- Follow redirects from passthrough origins inside the proxy and cache the file under the requested URL (`passthrough_follow_redirects`, on by default).
- Add `mirrors.election` to turn mirror election off and use each distribution's official address. With election on, the official address is the first candidate, the mirror before the first election has finished and the fallback after a failed one.
- Accept a set of distribution IDs for `mode`, including the IDs from `distributions.yaml`, and reject an unknown value at startup.
- Serve all routes with `net/http` and stream responses; Fiber and fasthttp are removed.
- Cache by-hash indexes for APT and serve immutable artifacts while the mirror is unreachable.
- Probe every mirror candidate without following redirects, prefer https, and keep a distribution's mirror when its election fails.
- Complete `repository prefetch` against the official Debian and Ubuntu archives, and serve percent-encoded paths from a snapshot.
- Fetch an allowlisted origin from the origin for paths that look like a distribution's, and keep a proxy client's credentials away from other hosts.
- Read `apt-proxy.yaml` strictly: an unknown key, a second document or an unresolved `${VAR}` reference stops startup.
- Fix `upstream_access` for HTTPS upstreams that negotiate HTTP/2.
- Go 1.27.4, which fixes GO-2026-6859.

Several defaults and error cases change. See [upgrade notes](docs/releases/v0.20.0.md) and the [full comparison](https://github.com/soulteary/apt-proxy/compare/v0.19.5...v0.20.0).

## v0.19.5 - 2026-11-02

- Refresh cli, health, HTTP, HTTP cache, logging, metrics, middleware, tracing and version kit dependencies with their compatible transitive requirements.
- Adopt http-kit/v2 v2.1.0 while retaining the existing retry API and request replay, cancellation, status handling and cache semantics.
- Refresh bilingual entry points, dependency acknowledgments and deployment examples.
- Go remains 1.27.3; no configuration or cache migration is required from v0.19.4.

See [upgrade notes](docs/releases/v0.19.5.md) and the [full comparison](https://github.com/soulteary/apt-proxy/compare/v0.19.4...v0.19.5). Earlier release notes remain in [docs/releases](docs/releases/).
