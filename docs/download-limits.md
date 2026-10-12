# Download time and spool limits

The proxy limits unknown-length response spooling by both response size and a
budget shared by all downloads in the same proxy instance. Known-length bodies
continue to stream without consuming this spool budget.

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
files charged until the downstream response body closes. Failed downloads remove
the temporary file and release their reservation. Released budget can be used by
later requests. Limit failures before forwarding headers return the reverse
proxy's upstream-error response (502) and do not cache a partial object.

The timeout starts before the upstream request, including its retries and header
wait, and remains active during reads of a known-length body or spooling of an
unknown-length body. A stalled upstream is canceled through its request context.
The existing shorter response-header timeout still applies. A known-length body
whose headers have already been forwarded cannot be changed to a 502; its
incomplete transfer ends and clients must retry. Unknown-length downloads fail
before their headers are forwarded. The timer is released when the upstream body
closes; it does not impose a deadline on delivering a completed spool file to a
slow client. Protocol-upgrade responses bypass spooling and preserve their bidirectional body interface; the upstream timeout also applies to their connection context.

The shared budget is per instance, not distributed across processes. It covers
only the unknown-length guard's temporary files, not the permanent cache,
httpcache-kit's separate temporary copy or S3 write buffers. It is not a global
filesystem quota or a limit on known-length package sizes. Operators should
leave space for these other consumers and configure limits for their workload.

Regression tests cover exact/oversized response admission, eight simultaneous
spools competing for a two-response budget, release and reuse, temporary-file
cleanup, real stalled known/unknown upstream bodies and YAML loading/validation.

```sh
go test -race ./internal/proxy ./internal/config -run 'Test(SpoolPerResponseLimitAndCleanup|ConcurrentSpoolBudgetAndReuse|UpstreamTotalDeadline|DownloadYAMLAndMerge|ValidateDownloadLimits)' -count=10
```
