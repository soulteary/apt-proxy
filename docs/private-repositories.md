# Private repository cache isolation

Requests carrying Authorization or Cookie bypass apt-proxy's shared cache even
when the upstream explicitly declares a response public. They neither read
anonymous entries nor enter cold-download sharing/storage. Credentials still
reach the configured upstream. Their responses receive an additional no-store
directive to prevent downstream storage.

URL userinfo is converted to Basic Authorization before cache lookup and removed
from the request URL. An explicit Authorization header retains precedence.
Incoming URL credentials are normalized on the detached request before routing;
configured mirror URL credentials are normalized after rewriting. All URL
userinfo (including username-only access tokens) and all query values are
redacted in mirror configuration/selection/benchmark logs, request and
upstream URL spans, rewrite debug fields and the daemon's query log field.
URL-bearing wrapped errors are sanitized at those diagnostic boundaries without
changing routing or the errors returned to callers. This does not constitute a
complete audit of arbitrary headers, bodies or other configuration logging.

Authenticated/cookie-bearing only-if-cached requests return 504 without touching
the upstream, including when an anonymous object exists for the same URL.
The no-store bypass is placed first in a combined Cache-Control field, including
when the client already supplied repeated directives. A response containing
Set-Cookie is never stored, regardless of its upstream
public/max-age policy. Ordinary anonymous package caching remains enabled.

## Existing cache migration

The daemon's verified cache now prefixes storage keys with a private-isolation
namespace. Pre-upgrade entries are unreachable from current requests, including
old authenticated responses whose headers cannot identify their origin. The
upgrade intentionally starts a cold logical cache once. It does not delete old
files; normal backend size eviction/expiry cleanup can reclaim them. Management
statistics may include old objects until that cleanup occurs. Administrative
purge retains its existing backend-wide meaning.

Header lookup, storage, invalidation, freshening and persisted marker/snapshot
lookups all use the same namespace. All instances sharing an S3 bucket should
be upgraded to use this policy; older running instances retain their old
behavior and cache namespace. Storage retrieval additionally rejects session
responses with Set-Cookie as defense against incorrectly populated entries.

## Scope

This is a conservative cache policy rather than per-user cache partitioning.
Even identical authenticated public packages are fetched separately. It does
not add upstream credential storage/rotation, validate repository permissions,
choose safe mirrors for private credentials or detect arbitrary custom auth
headers/query tokens. Operators remain responsible for trusted mirror/origin
configuration. Repository signature/checksum verification is separate work.

Real shared-disk-cache tests cover anonymous warming followed by two private
users, private cache-only requests, anonymous entry preservation, Set-Cookie
isolation and eight simultaneous authenticated clients. Additional tests cover
URL credential precedence, non-destructive legacy exclusion and namespaced
invalidation markers.

```sh
go test -race ./internal/proxy ./internal/storage -run 'Test(PrivateRepositoryRequestsBypassSharedCache|SetCookieResponsesAreNotShared|ConcurrentPrivateRepositoryRequestsStaySeparate|RepositoryURLCredentialsBecomePrivateHeaders|VerifiedCache|PrivateCacheNamespace)' -count=10
```
