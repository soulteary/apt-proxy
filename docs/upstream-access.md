# Guarded package-download connections

`upstream_access` is an optional YAML-only policy for the daemon's package
reverse-proxy transport. It is disabled by default for compatibility with
internal repositories and existing forward-proxy deployments.

```yaml
upstream_access:
  enabled: true
  allowed_hosts:
    - archive.ubuntu.com
    - mirror.example.com
  allowed_ports: [80, 443]
  allow_cidrs:
    - 10.42.0.0/16 # explicit exception for a trusted internal repository
```

When enabled:

- Only HTTP and HTTPS download targets are accepted. A nonempty host list
  matches exact names or IP literals, case-insensitively with a single trailing
  DNS dot normalized. Wildcards, URLs and bare IPv6 zones are invalid. Use ASCII
  names or IDNA punycode. Empty host lists retain repository/passthrough routing
  as the host-selection boundary.
- Empty port lists allow 80 and 443; a supplied list replaces these defaults.
  Ports must be between 1 and 65535.
- Each new connection resolves its host once, checks every returned address,
  then dials an approved numeric IP. Mixed allowed/denied DNS answers fail
  closed; there is no second hostname lookup at dial time. Approved IPv4/IPv6 families
  race with a 300 ms fallback delay (immediate fallback after an early failure),
  with at most two concurrent dials. DNS and all dials share a 30-second budget
  or the shorter request deadline. Within each family the remaining budget is
  divided across remaining addresses. Losing connections are canceled/closed. A fresh
  connection re-resolves and rechecks DNS. Existing pooled connections retain
  their previously checked peer.
- Private, loopback, link-local, unspecified and multicast addresses are
  rejected, along with carrier-grade NAT, documentation, benchmark and selected
  reserved/transition ranges. IPv4-mapped IPv6 addresses are checked as IPv4.
  See `internal/access/upstream.go` for the exact special-range list; this is
  not a continuously synchronized IANA address registry.
- Explicit `allow_cidrs` permit trusted private/special unicast networks, but
  never unspecified or multicast destinations. Exceptions do not override
  host or port rules. Use narrowly scoped exceptions: a broad CIDR deliberately
  weakens the address restriction, including metadata-service protection.
- TLS keeps the original hostname for SNI and certificate validation; the HTTP
  Host remains the original request authority.

## Proxies, redirects and errors

Enabled download transports use direct connections and ignore HTTP_PROXY,
HTTPS_PROXY and NO_PROXY. A remote forward proxy could resolve or route to an
unchecked destination, so this implementation does not claim to enforce the
policy through one. Disabling this policy retains existing proxy behavior.
Custom transport overrides are rejected with an active policy.

The package reverse proxy returns upstream redirects to the client; it does
not follow them. No proxy-side connection is made to a Location target. If a
client sends a new request back through apt-proxy, normal repository routing
and the transport policy apply again. This feature does not validate where a
client connects after receiving a redirect directly.

Policy denials are permanent and are not retried. DNS/network failures retain
the existing retry behavior and each new attempt is checked again.

Denied new downloads return the existing reverse-proxy 502 response and are
not stored as successful cache entries. Already-cached content remains usable:
this policy guards outbound connections, not client download authorization.
Use the separate download-client CIDR policy to restrict cache access.

## Configuration and boundary

Malformed rules fail configuration validation and server construction.
Restriction lists require `enabled: true`; a disabled policy with nonempty
lists is rejected rather than silently ignoring them. Omit `upstream_access`
or use `enabled: false` without lists to retain legacy behavior. Programmatic
merging replaces an explicitly supplied policy, including an explicit disable;
ordinary CLI/environment settings preserve the YAML policy.

Restart to apply changes. SIGHUP refreshes repositories/mirrors and does not
reload this policy. Policies are immutable per proxy instance, without shared
configuration state.

This batch protects package-download HTTP transports, including rewritten
mirrors and passthrough origins. Mirror election/benchmarking, geo lookup,
S3 storage, telemetry and other background HTTP clients are outside this
boundary. In particular, treat configured mirror/benchmark sources as trusted
until a later change installs the same policy on those clients. Use network
controls if all process egress must be restricted. CONNECT tunneling and
forward-proxy policy propagation are not added here.
