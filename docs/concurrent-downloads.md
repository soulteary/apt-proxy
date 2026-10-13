# Concurrent cold package downloads

`TestRepositoryConcurrentColdDownloads` verifies the production handler ordering:
mirror rewriting, a shared httpcache-kit handler, and a disk cache around the
reverse proxy.

For one cacheable package URL, the first miss downloads from upstream. Followers
wait for that download and the completed cache write, then receive the complete
package. The test starts a blocked leader and fifteen other requests, verifies
one upstream fetch and identical complete bodies, and checks reuse after the
batch finishes. It also verifies that a canceled follower exits without
canceling the leader or stranding other clients.

Coordinated-fill retries use a quiet cache recheck: verification, generation
ordering and private-key isolation still run, but these internal lookups do not
add duplicate native hit/miss counts. Management statistics therefore reflect
the client-facing lookups rather than repeated follower polling.

The guarantee is scoped to one process and one rewritten upstream cache key.
Different elected mirrors have different keys. Responses that cannot be cached
may require separate upstream downloads. This test does not establish
cross-instance locking, leader cancellation recovery, upstream truncation
recovery or a throughput benchmark.
For opt-in cross-process Unix disk coordination and its narrower verified
by-hash scope, see [shared repository cache](shared-repository-cache.md).

Run the focused regression with:

```sh
go test -race ./internal/proxy -run TestRepositoryConcurrentColdDownloads -count=10
```
