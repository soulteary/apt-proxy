# Download time and spool limits

The proxy limits unknown-length response spooling by both response size and a
budget shared by all downloads in the same proxy instance. Known-length bodies
stream without consuming this legacy spool sub-limit, while the
[shared resource manager](resource-limits.md) also bounds their object size,
active downloads and cache staging copies.

Configure limits in the existing YAML configuration file:

```yaml
download:
  max_spool_mb: 1024
  total_spool_mb: 4096
  timeout_seconds: 1200
```

The size units are MiB (1,048,576 bytes). These are the defaults. An omitted or
zero value selects the corresponding default; zero does not disable the bound.
Negative values and values that overflow byte/duration conversion are rejected
at startup. The settings are YAML-only; no new CLI flags or environment-variable
overrides are provided.

Reservations are made before writing a chunk to disk. An unknown-length response
that exceeds its per-response size limit or the available shared budget fails
rather than waiting indefinitely for space. The guard keeps completed temporary
files charged until the downstream response body closes, even though the filename
has already been unlinked. Process exit closes these anonymous files, reclaiming
their disk storage without an application cleanup pass. Failed downloads close
the temporary file and release their reservation. Released budget can be used by
later requests. If the filename cannot be unlinked, the proxy closes the file
and fails before copying any body bytes or reserving body capacity. Named files
from older versions require an offline cleanup; see
[interrupted downloads](interrupted-downloads.md).
Legacy spool size/budget failures before forwarding headers return 502 and do
not cache a partial object. A refusal by the shared temporary-resource or active
download budget returns 503 with `Retry-After: 1`.

The timeout starts before the upstream request, including its retries and header
wait, and remains active during reads of a known-length body or spooling of an
unknown-length body. A stalled upstream is canceled through its request context.
The existing shorter response-header timeout still applies. A known-length body
whose headers have already been forwarded cannot be changed to a 502; its
incomplete transfer ends and clients must retry. Unknown-length downloads fail
before their headers are forwarded. The timer is released when the upstream body
closes; it does not impose a deadline on delivering a completed spool file to a
slow client. Protocol-upgrade responses bypass spooling and preserve their bidirectional body interface; the upstream timeout also applies to their connection context.

The legacy spool sub-limit is per instance and covers the unknown-length guard's
temporary files. The additional shared resource budget covers that spool,
httpcache-kit's response/native staging copies and S3 payload buffers together.
Neither counter is a global filesystem quota; persistent cache admission remains
separate. See [resource limits](resource-limits.md) for known-length admission,
disk headroom checks and the scope of each counter.

Regression tests cover exact/oversized response admission, eight simultaneous
spools competing for a two-response budget, release and reuse, temporary-file
cleanup, real stalled known/unknown upstream bodies and YAML loading/validation.

```sh
go test -race ./internal/proxy ./internal/config -run 'Test(SpoolPerResponseLimitAndCleanup|ConcurrentSpoolBudgetAndReuse|UpstreamTotalDeadline|DownloadYAMLAndMerge|ValidateDownloadLimits)' -count=10
```
