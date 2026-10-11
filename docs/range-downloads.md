# Package range downloads

The proxy uses httpcache-kit for byte-range handling. The repository-level
regression test `TestRepositoryRangeCache` exercises the actual mirror rewrite,
reverse proxy and shared disk-cache wiring.

- A cold Range request is forwarded upstream. A 206 partial response is not
  stored as the complete package; a subsequent full GET downloads the whole file.
- Once a complete response is cached, bounded, open-ended and suffix byte ranges
  are served from that object without another upstream request.
- Unsatisfiable ranges return 416 and the complete-object size in Content-Range.
- If-Range requests bypass the cache and let the upstream check the validator.
  A matching ETag returns the requested suffix; a changed ETag returns the full
  representation so the client can restart its download safely.

This supports client-driven resume. It does not persist incomplete download
segments or merge cold ranges into a complete cached object. The test does not
simulate a connection dropping midway through a response.

Run the focused regression with:

```sh
go test -race ./internal/proxy -run TestRepositoryRangeCache -count=10
```
