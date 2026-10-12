# Download client access control

Package download routes can be restricted by the immediate TCP connection
peer. This policy runs before repository routing, cache lookup and upstream
fetching, including cached hits and third-party passthrough downloads.

```yaml
security:
  download_allow_cidrs:
    - 192.0.2.0/24
    - 2001:db8::/32
  download_deny_cidrs:
    - 192.0.2.42/32
```

Both lists default to empty, preserving existing access. An empty allow list
allows every peer except those denied. A nonempty allow list requires a match.
Deny always takes precedence. Use `/32` for a single IPv4 address or `/128` for
a single IPv6 address. Network host bits are normalized; IPv4-mapped IPv6 peers
match ordinary IPv4 rules. Use ordinary IPv4 CIDRs rather than mapped IPv6 CIDRs.

Malformed rules (including empty entries, bare IPs and surrounding whitespace)
fail configuration validation and server construction. With an active policy,
a missing or malformed connection peer fails closed. Denials return HTTP 403
with `Cache-Control: no-store`, without reading a cached object or fetching
upstream. The package-download policy is independent of API keys.

## Reverse proxies and operational routes

`X-Forwarded-For`, `Forwarded` and `X-Real-IP` never grant package-download
access, even when `security.trusted_proxies` is configured for API logging and
rate limiting. Behind a reverse proxy, the policy sees the proxy's connection
address. Restrict end clients at that proxy and prevent direct access to
apt-proxy; allowing a proxy network grants download access to all clients it
forwards. This avoids relying on a potentially attacker-controlled forwarded
header or an incompletely configured proxy chain.

The root page, static logo, ping, health, version, metrics and management API
routes retain their existing policies. Download ACLs do not protect those
endpoints. API authentication and rate limiting remain separate controls;
use network controls or an authenticated reverse proxy for operational
endpoints when required.

## Configuration and scope

These options are YAML-only. Restart after changing them: SIGHUP currently
refreshes repository/mirror settings, not download ACLs. Existing CLI/environment
settings do not overwrite these YAML lists. The legacy programmatic merge
replaces a list when its override is non-nil, including an explicit empty list;
nil keeps the base list.

The immutable policy belongs to each daemon instance and applies to the
production HTTP/HTTPS download route. Direct embedding of `PackageStruct` is
outside that daemon boundary and must install its own access wrapper. This
feature does not add per-user download authentication, forwarded-client ACLs,
upstream address/port restrictions, DNS-rebinding protection or redirect-target
validation. Repository/passthrough matching continues to determine supported
upstream routes.
