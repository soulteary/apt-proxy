# Snapshot-checked runtime mirror failover

Runtime health is opt-in and configured in the main YAML file. It supplements
startup mirror election and explicit refresh for public Debian-family archives.
It does not change which mirror startup selects: pin the intended primary with
the ordinary `mirrors` setting, or include the elected mirror in the mapping.

```yaml
mirrors:
  ubuntu: https://primary.example/ubuntu/

mirror_health:
  enabled: true
  failure_threshold: 2
  cooldown_seconds: 30
  probe_timeout_seconds: 5
  max_manifest_bytes: 2097152
  snapshot_max_age_seconds: 3600
  repositories:
    - id: ubuntu
      mirrors:
        - https://primary.example/ubuntu/
        - https://secondary.example/archive/
      # Optional: explicitly public query fields preserved on artifact requests.
      public_query_keys: [download]
```

Each `id` must name a distribution in the running registry. Mirror URLs are
complete, ordered, equivalent archive roots ending in `/`, without credentials,
query strings or fragments. Duplicate/overlapping bases within a mapping are
rejected. Only requests rewritten to an exactly mapped scheme, authority and
path prefix participate. An unrelated mirror, third-party passthrough origin or
unlisted Debian Security root keeps its normal behavior. To manage a separate
security archive, define it as a separate distribution and mapping.

The proxy streams successful, full `GET dists/<suite>/InRelease` and `Release`
bodies through SHA256. It learns only complete 200 responses without private,
no-store, Set-Cookie, Content-Range or Content-Encoding headers. HEAD, cached
hits, 304 responses and aborted/over-limit bodies cannot teach a fingerprint.
Starting to consume newer metadata invalidates the previous fingerprint until
EOF; an incomplete new version cannot leave the old version eligible. There
are at most 64 manifest paths per repository. Exceeding that limit disables
switching until explicit refresh. Suite names use letters/digits, dots,
underscores and hyphens and are limited to 128 characters.

After the configured number of failed request series (a transport error,
incomplete response body or 5xx after ordinary retries), candidates are checked
in configured order. Successful headers do not reset failures: only a complete
body EOF with the declared length establishes recovery. A truncated body already
being delivered cannot be replayed in place; its failure can authorize a verified
switch for the next request. Client cancellation does not count as mirror failure.
Every known manifest must match the observed SHA256 exactly, including all observed
suites. Missing, divergent, oversized, incomplete, private or redirected
candidate manifests are refused. A `dists/<suite>/...` request additionally
requires that suite to be known. A `pool/...` artifact has no suite in its path,
so every observed suite must be known and current. An unknown or expired
snapshot prevents switching. Each selection has one total probe deadline,
shared across selection waiting, all candidate/manifest probes and artifact
replay through response headers, and each manifest body read is bounded. After
replay headers arrive, the artifact body retains the original download timeout
rather than the shorter probe deadline; closing it releases its child context.
Probes use the same upstream connection policy as downloads and never follow
redirects. Only empty-body GET/HEAD are replayed; Range and conditional headers, escaped
paths and explicitly public query parameters are preserved.

Credentials never move between mirrors. Authorization, Cookie, URL userinfo,
proxy authorization and unknown/custom headers disable participation. Operational
request IDs and framing fields are permitted locally but stripped from a
replacement request. Other query fields disable switching, and credential-like
names cannot be declared public. A manifest request with any query is excluded
because it may represent a different snapshot. Probe requests carry no client
headers or query. Configured mirrors containing credentials are rejected.

The selected replacement remains active while its verified fingerprints apply.
New or partially read metadata blocks unverified reuse; newly observed suites
must be verified before pool downloads can use that replacement. There is no
background polling or automatic failback. The cooldown bounds repeated failed
selections. State is process-local: a restart forgets fingerprints, and a
successful SIGHUP/API refresh resets health with a new routing epoch. Late
responses from the previous epoch cannot teach or switch the new routing.
Restart to change `mirror_health` configuration itself.

`GET /api/mirrors/status` returns enabled state, ordered public mappings,
source/active roots, per-mirror failure counts, snapshot count, cooldown deadline
and the last decision. It follows the existing management API authentication
policy and is read-only. Successful changes also emit an Info log containing
the repository ID and public source/target roots. Refusals are visible in status
and Debug logs; a refused selection preserves the original upstream response.

Limits accept zero as the documented default. Supported ranges are threshold
1..20, cooldown 1..3600 seconds, total probe timeout 1..30 seconds, manifest size
1 byte..8 MiB, and snapshot age 1..86400 seconds, with at most 32 repositories and
2..8 mirrors per repository. Fingerprint storage is bounded; observing a large
manifest does not truncate the response served to the client.

This is equality checking against metadata actually observed from the active
mirror, not a PGP verifier or an atomic repository snapshot service. APT still
verifies signatures and artifact checksums. Repository contents can change
between a probe and a download, and only observed suites are covered; configured
roots must therefore be operator-approved equivalents. Existing cache entries
need no migration, and fingerprints are not recovered from cached bodies.
