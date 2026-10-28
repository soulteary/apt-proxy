# Persistent cache integrity

The daemon requires private SHA256 cache records by default:

```yaml
cache:
  integrity: sha256
```

A record binds the complete cached body, its byte size, request key, HTTP status,
response headers and the full-precision private generation timestamp. Cache
retrieval verifies the record and body before serving a hit. This catches
same-length body changes, header or status damage, truncation, appended bytes,
and mixed body/header generations left by interrupted publication. Only the
observed damaged key generation is discarded, then the normal handler refetches
it; a concurrently published newer replacement is preserved. Neighboring
objects are unaffected. HTTP Freshen keeps the original body checksum and
updates the private header generation. These checks work through the disk and
S3 cache backends and continue after restart.

Private digests live in a `HTTPCACHE/2` storage preamble, not response headers.
They never appear as client-visible HTTP fields. An upstream `Digest` header
cannot substitute for this record. This detects accidental storage corruption;
it does not establish package authenticity against an attacker who can rewrite
the cache and recompute digests. APT signature/package validation and by-hash
upstream validation remain separate checks.

Existing entries without private digest metadata are treated as misses and
refetched lazily. No bulk purge is required. This creates an expected temporary
increase in upstream traffic after upgrading a previously populated cache.
Verification reads the complete cached body before a hit, including HEAD hits,
and therefore increases local/S3 read I/O and first-byte latency. On S3 it can
require a full object transfer before responding. Operators accepting the
legacy performance tradeoff can explicitly opt out of required digests:

```yaml
cache:
  integrity: none
```

In that mode new cache writes use the legacy record format; existing verified
records remain verified when read. Unknown modes are rejected at startup.
The historical Content-Length/session isolation checks remain active. The
private cache namespace and real upstream request credentials are unaffected.

Enabling `shared_cache` requires SHA256 integrity and overrides the `none`
opt-out. Shared identities therefore cannot reuse unverified legacy records.

## Vary migration and resource cost

The released httpcache-kit v4.1.0 dependency uses a versioned Vary namespace.
Persisted variants from an older namespace safely become misses and are lazily
refetched; ordinary non-Vary base keys are unchanged by that library migration.
This is additional to the daemon's required-digest and private-isolation checks
above. No manual purge is required, but plan for temporary extra upstream
traffic. See the [v0.18.0 upgrade notes](releases/v0.18.0.md).

Native disk retrieval retains an open header descriptor until the corresponding
cache resource closes, allowing generation checks to distinguish a peer's
replacement even when timestamps and body digests match. Include this additional
descriptor per live retrieved resource when sizing process file-descriptor
limits. Integrity hashing uses streaming reads rather than a package-sized
in-memory buffer; it still adds complete-body I/O before a hit.
