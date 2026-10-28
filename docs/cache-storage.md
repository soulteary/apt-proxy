# Cache behavior and S3 storage

[Project README](../README.md) · [中文](cache-storage.zh-CN.md)

## Cache Capacity and Eviction

The cache supports a size limit configured via `max_size_gb` (YAML), `--cache-max-size` (CLI), or `APT_PROXY_CACHE_MAX_SIZE` (environment variable). When the total cache size exceeds this limit, the proxy automatically evicts the **least recently used** (LRU) files until the total size is within the limit. Eviction runs both when storing new items and during periodic cleanup.

- Set a positive value (e.g. `20` for 20 GB) to enable the capacity limit and LRU eviction.
- Set `--cache-max-size=0` or `APT_PROXY_CACHE_MAX_SIZE=0` to disable the size limit; no size-based eviction is performed. YAML `max_size_gb: 0` retains the default.

Similarly, `--cache-ttl=0` / `APT_PROXY_CACHE_TTL=0` disables backend expiry,
and `--cache-cleanup-interval=0` / `APT_PROXY_CACHE_CLEANUP_INTERVAL=0` disables
periodic cleanup. Disabling backend TTL does not override HTTP freshness or
upstream cache directives; a positive size limit can still evict during stores.

After a process restart, the LRU order is approximated using file modification time until new accesses update it.

With `shared_cache.coordination_dir`, shared disk mode disables capacity/TTL eviction, background cleanup and startup staging sweeps; online global purge is rejected. Stop every sharing instance before maintenance. See [shared repository cache](../docs/shared-repository-cache.md).

## Cache Directory Layout

The disk backend keeps four things under the cache directory. They appear on the
first store, not at startup:

```
body/v1/<hashed-key>      response bodies
header/v1/<hashed-key>    status line, headers, and the store timestamp
staging/v1/               entries being written; empty when idle
stale-markers.json        invalidation state
```

An entry is a body and a header together, and the two are published as one step:
bytes are written under `staging/v1` and renamed into place, so re-storing a file
that is already cached never exposes a truncated or empty entry to a concurrent
reader. Nothing under `staging/v1` is a cache entry — **exclude it when you size
the cache directory, back it up, or rsync it.** Files a killed process left there
are swept on the next start in ordinary single-instance mode. Shared disk mode
requires offline cleanup after all instances have stopped.

In ordinary mode, `body/v1` and `header/v1` count toward `max_size_gb`; staging
and invalidation markers do not. Header files include private integrity and
generation metadata, and their size varies with the response. Staging payloads
are bounded separately by `resources.total_temp_mb`.

The S3 backend has no `staging/` prefix. Object writes go straight to the final
key, because staging needs a rename and the VFS interface has none.

## Cache Keys and Mirror Selection

By default, a cache key includes the **rewritten** upstream URL, HTTP method
and Vary selection fields, rather than only the URL the client asked for. Rewriting happens before the cache layer, so a request for
`/ubuntu/dists/noble/InRelease` is stored under the elected mirror's URL —
`http://mirrors.example.com/ubuntu/dists/noble/InRelease`.

The practical consequence: **changing the elected mirror starts a fresh cache.**
The same client request is then a different key, so it is refetched, and the
entries under the old mirror stay on disk until TTL or LRU eviction removes
them. Measured on a single object:

```
same mirror, second request   -> upstream contacted once   (cache hit)
after switching the mirror    -> refetched from the new mirror
```

Mirror election runs at startup and on an explicit refresh. Opt-in
[runtime mirror health](../docs/runtime-mirror-health.md) additionally supports
snapshot-checked failover between explicitly equivalent public APT archives,
with bounded probes, cooldown and `GET /api/mirrors/status` observability.
Unknown/divergent snapshots and private requests cannot switch automatically.
Benchmark results
are not persisted, so **a restart re-elects**, and so do `SIGHUP` and `POST
/api/mirrors/refresh`. A mirror timing out during benchmarking is dropped from
that run's candidates. If an election fails during refresh, the complete previous
routing remains active and the refresh reports an error; new match patterns and
cache rules take effect only after a successful retry.

By default, entries are not shared between mirrors because their contents may
differ. An explicit [shared_cache mapping](../docs/shared-repository-cache.md) can
reuse content-verified SHA256 by-hash indexes across equivalent roots; ordinary
packages and mutable metadata do not receive that shared identity. With
cross-process coordination, other requests use per-process identities and
rewarm after restart. To keep the default cache warm across restarts,
**pin the mirror**:

```bash
./apt-proxy --ubuntu=https://mirrors.tuna.tsinghua.edu.cn/ubuntu/ \
            --debian=https://mirrors.tuna.tsinghua.edu.cn/debian/
```

A pinned mirror is used as-is with no benchmarking, so the keys are stable for
the life of the deployment.

## S3 Storage Backend

Instead of writing the cache to a local directory, `apt-proxy` can keep every cached
body/header inside an **S3-compatible** object store. This lets the cache outlive
ephemeral compute (e.g. Kubernetes nodes) or move persistent objects off local
disk. Concurrent instances writing the same bucket/prefix have no distributed
generation-publication or miss-coordination guarantee; give instances separate
prefixes. `shared_cache.coordination_dir` is supported only on Unix disk storage.

Switch the backend with `--storage-backend=s3` (or `APT_PROXY_STORAGE_BACKEND=s3`,
or `storage.backend: s3` in YAML). `endpoint` and `bucket` are required. For authenticated access, supply
`access_key` and `secret_key` together; public buckets can use anonymous access.

**YAML example:**

```yaml
storage:
  backend: s3
  s3:
    endpoint: minio.example.com:9000     # host[:port], no scheme
    region: us-east-1                    # required for AWS S3, ignored by most MinIO services
    bucket: apt-proxy
    prefix: apt-proxy/                   # optional, default "apt-proxy/"
    access_key: ${APT_PROXY_S3_ACCESS_KEY}
    secret_key: ${APT_PROXY_S3_SECRET_KEY}
    use_ssl: true
    use_path_style: false                # MinIO/Ceph need true; AWS/R2/B2 use false
    inline_max_mb: 32                    # writes <= 32 MiB stay in memory; larger spill to TempDir
                                         # Per-write threshold; payload copies also share resources.total_temp_mb.
    temp_dir: ""                         # empty = os.TempDir()
```

**ENV example** (suitable for Kubernetes / Docker):

```bash
APT_PROXY_STORAGE_BACKEND=s3
APT_PROXY_S3_ENDPOINT=minio:9000
APT_PROXY_S3_BUCKET=apt-proxy
APT_PROXY_S3_ACCESS_KEY=...
APT_PROXY_S3_SECRET_KEY=...
APT_PROXY_S3_USE_SSL=false
APT_PROXY_S3_USE_PATH_STYLE=true
```

**CLI example:**

```bash
./apt-proxy \
  --storage-backend=s3 \
  --s3-endpoint=s3.us-west-2.amazonaws.com \
  --s3-region=us-west-2 \
  --s3-bucket=apt-proxy \
  --s3-access-key=$AWS_ACCESS_KEY_ID \
  --s3-secret-key=$AWS_SECRET_ACCESS_KEY
```

**Compatibility matrix:**

| Provider           | `endpoint`                                           | `use_ssl` | `use_path_style` | Notes                                  |
| ------------------ | ---------------------------------------------------- | --------- | ---------------- | -------------------------------------- |
| AWS S3             | `s3.<region>.amazonaws.com`                          | `true`    | `false`          | Set `region` explicitly                |
| MinIO              | `minio.local:9000` / `<host>:9000`                   | varies    | `true`           | path-style is required                 |
| OtterIO            | `otterio:9000` / `<host>:9000`                       | varies    | `true`           | Apache-2.0 fork of MinIO; same config  |
| Ceph RGW           | `rgw.example.com`                                    | varies    | `true`           | path-style is required                 |
| Cloudflare R2      | `<account>.r2.cloudflarestorage.com`                 | `true`    | `false`          | Region must be `auto`                  |
| Backblaze B2       | `s3.<region>.backblazeb2.com`                        | `true`    | `false`          | App keys with read+write to bucket     |
| Aliyun OSS         | `oss-cn-hangzhou.aliyuncs.com`                       | `true`    | `false`          | RAM keys with `oss:GetObject/PutObject`|
| Tencent COS        | `cos.ap-shanghai.myqcloud.com`                       | `true`    | `false`          | Use SecretId/SecretKey                 |
| Garage / SeaweedFS | depends                                              | varies    | `true`           | Treat as MinIO-flavoured               |

**Operational notes:**

- The bucket must already exist. `apt-proxy` performs a `BucketExists` check on
  startup and refuses to launch on misconfiguration so you don't discover the
  problem on the first cache miss.
- Health check (`/healthz`) reports the storage backend status: a HeadBucket
  round-trip for `s3`, `os.Stat` for `disk`.
- In-memory metadata LRU (8192 entries by default) absorbs the chatty
  `Header()` access pattern of `httpcache-kit`. Default integrity checks still
  read the complete body before a hit; the metadata LRU cannot skip verification.
- Writes use a "smart" upload strategy: bodies up to `inline_max_mb` stay in
  RAM and PUT in one shot; anything bigger spills to a temp file before
  `PutObject`. Tune `inline_max_mb` based on your typical package size.
- `cache.dir` / `--cachedir` / `APT_PROXY_CACHEDIR` are **ignored** when the
  S3 backend is active. Large uploads and unknown-length downloads still need
  writable local temporary space, and TLS cert/key files need local paths.
  Setting a non-default cache.dir while `storage.backend=s3` triggers a
  startup warning so the override is observable rather than silently dropped.

**Resource sizing & capacity planning (S3 backend):**

S3 inline payloads and spilled copies both count against
`resources.total_temp_mb` (4096 MiB by default). During a switch from memory to
disk, both overlapping copies are charged. `inline_max_mb` (32 MiB by default)
is a per-write threshold; `concurrent_writes × inline_max_mb` is not a process
RSS limit. Background publication can retain its charge after an upstream slot
is released. HTTP buffers, metadata and the Go runtime need additional memory
headroom. For predominantly small packages, a 4–8 MiB inline threshold may be
appropriate; measure with your workload.

Spills use anonymous files: the pathname is unlinked immediately, while closing
the descriptor or exiting the process reclaims the space.
`storage.s3.temp_dir` defaults to the system temporary directory and must be
writable. A read-only root filesystem requires a separate writable volume.
Disk admission includes held temporary reservations and `resources.min_free_mb`
(128 MiB by default); failed inspection refuses admission. Budgets are per
instance, so allocate limits for each process when sharing a volume.

On Kubernetes, mount temporary storage on an `emptyDir` and set explicit memory
and `ephemeral-storage` requests/limits. The volume must accommodate the chosen
budget, reserved disk headroom and other writes. This application configuration
is an example; validate Pod limits against measured workload:

```yaml
resources:
  max_object_mb: 1024
  total_temp_mb: 4096
  max_active_downloads: 32
  min_free_mb: 128
storage:
  backend: s3
  s3:
    inline_max_mb: 8
    temp_dir: /var/cache/apt-proxy/tmp
    # endpoint, bucket and credentials as in the complete example above
```

Uploads use streaming single PUT; `resources.max_object_mb` cannot exceed
5120 MiB on S3. See [resource limits](../docs/resource-limits.md) for admission
refusals, deadlines and budget boundaries.

A complete working example (compose stack with OtterIO + auto-provisioned bucket
+ pre-wired apt-proxy) lives in [`examples/s3-otterio/`](../examples/s3-otterio).
