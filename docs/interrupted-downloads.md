# Interrupted upstream downloads

The reverse proxy runs inside httpcache-kit's upstream worker. Go's
ReverseProxy can panic with http.ErrAbortHandler on response-copy failure;
that worker is outside net/http's per-request panic recovery. The proxy now
contains that intentional signal at the reverse-proxy boundary so the cache
can close its stream and finish the miss flight. Other panics still propagate.

Known-length responses continue to stream. The existing cache write check
rejects a body shorter than Content-Length. The client can observe an incomplete
response and retry; the proxy does not splice a retry into a partially sent
response. Waiting clients can fetch a replacement when the failed leader leaves
no usable cache entry.

Unknown-length responses, including chunked and automatically decompressed
responses, are fully spooled to a temporary file before headers are exposed to
the cache or client. A failed body read returns an upstream error (502); a clean
EOF produces a fixed-length response. Trailer values received at EOF are
promoted to ordinary response headers, and the trailer announcement is removed
so fields such as Digest remain visible to clients and cache hits. On the released
Linux and macOS platforms, the temporary filename is unlinked before any body
bytes are written. The open descriptor remains readable and seekable, and the
operating system reclaims its storage on close or process exit, including SIGKILL.
Disk deployments spool in `cache_dir`; S3 deployments use
`storage.s3.temp_dir` (or the system temporary directory when unset). The
selected directory must already exist and be writable. HEAD and no-body responses retain their existing framing. Protocol upgrades
bypass spooling and preserve bidirectional bodies; the total upstream timeout
also applies to their connection context.

This intentionally delays the first response byte for unknown-length downloads
and temporarily consumes space proportional to their size. It keeps memory
bounded. [Download limits](download-limits.md) now bound the guard's per-response
and shared temporary usage and upstream fetch time; persisted partial-segment
resume is still unsupported. Known-length streaming is unchanged. Successful
unknown-length responses subsequently use the normal cache storage path, which
can require an additional temporary copy. Startup does not sweep the temporary
directory, because another proxy instance may be using it. Named
`apt-proxy-download-*.tmp` files left by earlier versions should be removed only
after stopping every instance using that directory; this change prevents new
body-bearing crash residues rather than guessing which legacy files are active.

Real HTTP connection tests cover a truncated fixed-length response, an unfinished
chunked response, a complete replacement and its cache reuse, and canceling a
cold-download leader while another client requests the same package. Focused
unit tests cover successful temporary-file cleanup and selective panic handling.
These checks establish response framing completeness; they do not validate
repository signatures, checksums or the meaning of a clean but incorrect body.

```sh
go test -race ./internal/proxy -run 'Test(InterruptedUpstreamDownloadRecovers|CanceledColdLeaderDoesNotStrandFollowers|UnknownLengthDownloadTempFileCleanup|AbortSafeHandlerDoesNotHideUnexpectedPanics)' -count=10
```
