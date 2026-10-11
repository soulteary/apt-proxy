# Cached object length verification

The daemon checks a retrieved GET object's seekable body size against its stored
Content-Length before serving it from the disk or S3/VFS cache. The check seeks
to the end and restores the body position; it does not scan the entire package
or allocate a package-sized buffer.

A malformed length or a size mismatch is treated as a cache miss, with the
retrieved body closed. A normal request fetches a replacement from upstream; an
only-if-cached request returns 504 without downloading. Successful cache writes
replace the damaged entry using the existing backend publication mechanism.
There is no destructive cache-wide purge. If replacement fails, the damaged
entry continues to be rejected on subsequent requests.

The wrapper retains persisted invalidation marker and snapshot support. HEAD
entries are excluded because their Content-Length describes a GET body that
is absent from the HEAD response. GET entries without Content-Length remain
usable and cannot be verified by this check. Unknown-length bodies, same-length
bit corruption, repository checksum validation and upstream connection-drop
recovery require separate work. This check is not a cryptographic integrity
verification or a signature verification mechanism.

Regression tests mutate real cached body files by truncating or appending bytes,
verify replacement, exercise cache-only rejection and normal re-download, and
check invalidation marker forwarding and metadata exceptions.

```sh
go test -race ./internal/storage ./internal/proxy -run 'Test(VerifiedCache|DamagedPackage)' -count=10
```
