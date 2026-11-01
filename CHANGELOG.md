# Changelog

## v0.19.5 - 2026-11-02

- Refresh cli, health, HTTP, HTTP cache, logging, metrics, middleware, tracing and version kit dependencies with their compatible transitive requirements.
- Adopt http-kit/v2 v2.1.0 while retaining the existing retry API and request replay, cancellation, status handling and cache semantics.
- Refresh bilingual entry points, dependency acknowledgments and deployment examples.
- Go remains 1.27.3; no configuration or cache migration is required from v0.19.4.

See [upgrade notes](docs/releases/v0.19.5.md) and the [full comparison](https://github.com/soulteary/apt-proxy/compare/v0.19.4...v0.19.5). Earlier release notes remain in [docs/releases](docs/releases/).
