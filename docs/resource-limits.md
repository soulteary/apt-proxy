# Shared download and temporary-storage limits

The daemon uses one per-process resource manager for unknown-length download
spools, HTTP cache response copies, native disk body/header staging, and S3
inline/spilled copies. Known-length responses also require an active download
slot and obey the object limit; a Content-Length declaration does not bypass
admission.

```yaml
resources:
  max_object_mb: 1024
  total_temp_mb: 4096
  max_active_downloads: 32
  min_free_mb: 128
```

All units are MiB. Omitted or zero values select these bounded defaults.
Negative values and unit overflow are rejected. If `resources.total_temp_mb` is
omitted, an explicit legacy `download.total_spool_mb` supplies the shared total.
The legacy unknown-length spool budget remains an additional sub-limit;
`download.max_spool_mb` still bounds that preliminary response copy.
`download.timeout_seconds` bounds upstream transfer time and S3 PUT time
(default 20 minutes).

Admission is nonblocking: a copy cannot wait for capacity occupied by another
copy of the same response. Retained bytes count until the anonymous descriptor
closes, a native stage publishes, or a named temporary file is successfully
removed. Failed removal retains its charge. S3 switches from exact-size inline
chunks to an anonymous disk file; both copies are charged during that switch,
then the inline charge is released. An expired, failed or canceled transfer
releases its active slot and closes owned response bodies. HTTP cache writes
keep the response-copy charge until background publication finishes.

Every disk reservation checks the cache/staging filesystem's available space.
It conservatively subtracts all held temporary reservations as well as the
configured headroom, preventing concurrent writers from each promising the
same free space. This can refuse admission before the physical filesystem is
full. Inspection failures refuse admission; Linux and macOS provide this
check. Temporary space retained by unrelated programs or processes is visible
through filesystem free space, but the byte and download counters are local to
one daemon. Operators sharing a volume should allocate per-instance limits.

An active-download or shared temporary-resource admission refusal produces 503
and `Retry-After: 1`. The additional legacy unknown-length spool size/budget
sub-limit still produces 502. An upstream object exceeding the limit is rejected before
headers when its length is known; an unknown-length object must complete its
bounded spool first. A falsely declared stream is capped as it is read.
A later cache-copy refusal preserves the complete response already delivered
from upstream and skips publication. Cache hits need no upstream slot.

S3 cache writes use streaming single PUT rather than SDK multipart buffering,
so uncharged large part buffers do not accumulate outside this budget.
`resources.max_object_mb` must be at most 5120 on S3, and the writer independently
rejects bytes beyond that single-PUT cap. Disk storage may use a larger explicit
object cap. Upload deadlines, errors and spill failures release retained
inline/disk charges.

`cache.max_size_gb` governs admitted persistent entries separately. Temporary
limits do not count persistent cache objects, HTTP/network framing buffers,
small bookkeeping structures or invalidation-marker journals. They cover
payload copies, including S3 inline payload memory; they are not a total Go
heap or process RSS limit. Ordinary process exit and SIGKILL reclaim anonymous
spools and S3 spills. Shared-disk native publication may require offline orphan
cleanup as described by the cache coordination configuration.
