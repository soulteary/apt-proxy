# Explicit repository identity and shared disk cache

This optional YAML feature shares public SHA256 by-hash indexes between explicitly
mapped mirror bases. Repository IDs and the complete sorted base list define the
identity; method, relative index path, query string and HTTP Vary stay distinct.
Repository IDs must be nonempty, unique, at most 128 bytes and contain no NUL.
The full body must match the requested SHA256 before it can populate a shared key.
Mapped error, redirect and partial responses receive `no-store`, even if a mirror
marks them public or fresh. A bodyless 304 can freshen an existing verified entry;
HEAD retains its normal metadata behavior.
Mutable Release/InRelease, packages, unmapped paths and other requests keep
per-process identities when coordination is enabled.

```yaml
shared_cache:
  repositories:
    - id: ubuntu-archive
      bases:
        - https://archive.ubuntu.com/ubuntu/
        - https://mirror.example/ubuntu/
  coordination_dir: /var/cache/apt-proxy-locks
  max_index_mb: 32
```

Only empty-body GET/HEAD requests with recognized public HTTP headers participate.
Authorization, Cookie, custom token/tenant headers, unknown headers, request bodies,
transfer encodings and trailers bypass cache reads and writes and receive no-store.
Local request IDs and adapter framing are removed before shared upstream delivery.
Bases cannot contain credentials, query parameters or fragments. Configuration
changes require restarting every sharing instance with the same policy.
Enabling this policy forces SHA256 cache integrity, even when
`cache.integrity: none` is selected for ordinary mode. Omitting `shared_cache`
leaves this identity policy disabled; a configured policy without
`coordination_dir` supplies the explicit identities without cross-process locks.

Coordination uses kernel file locks on Unix, held through the asynchronous cache
commit. Followers recheck cache after acquiring the miss lock. Header/body operations
use separate data locks. Per-key durable invalidation markers are shared across
instances and restarts; old Store/Freshen operations cannot clear a newer barrier. Kernel locks release on process exit or SIGKILL; there is
no expiry that could permit a second active writer. Keep both the cache and lock
directories private and on a filesystem that supports process-shared flock.
Never delete lock files while any instance is running.

Shared disk mode disables per-instance TTL eviction, MaxSize eviction, background
cleanup and automatic staging-file sweeping, because those operations cannot safely
infer another instance's active files. Capacity planning and quiesced maintenance
are required; statistics are per-instance estimates. Online global purge is rejected.
Stop every instance before removing old cache entries or orphan native staging files.
S3 coordination is rejected: this implementation does not supply the conditional
immutable-generation publication needed for safe shared object-storage writes.
Windows coordination is unsupported. For ordinary S3 deployments, use a separate
bucket prefix per independently running instance rather than relying on shared
publication guarantees.

Use the global resource budget to bound temporary copies and preserve disk headroom.
The by-hash verification spool supports the same staging budget and retains its
reservation until its response body closes. The default index limit is 32 MiB,
with a maximum of 1024 MiB. The smaller of this limit and
`resources.max_object_mb` applies before verification can spool an object. Each
instance has its own resource budget; disk headroom checks observe the shared
filesystem but are not a cluster-wide reservation service. Checksums detect accidental corruption; they do not
replace APT signature trust or its expiry checks.
