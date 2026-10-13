# CentOS Metalink repositories

This endpoint was added after v0.18.0 in
[PR #158](https://github.com/soulteary/apt-proxy/pull/158). Use a build or release
containing that change; v0.18.0 binaries do not expose it. See
[verified support and version availability](client-support.md).

The optional `metalink` YAML configuration exposes a local `/metalink` endpoint.
DNF still consumes a Metalink v3 document and checks its `repomd.xml` digests;
apt-proxy rewrites the document's resource URL to a local snapshot route. Metadata
and RPM requests then use the normal download limits and shared HTTP cache.
This addresses [issue #70](https://github.com/soulteary/apt-proxy/issues/70).

```yaml
mode: centos
mirrors:
  centos: https://mirror.stream.centos.org/
metalink:
  public_url: http://apt-cache.example:3142
  source_url: https://mirrors.centos.org/metalink
  allowed_mirror_hosts:
    - mirrors.tuna.tsinghua.edu.cn
    - mirrors.bfsu.edu.cn
    - mirrors.ustc.edu.cn
  refresh_seconds: 600
  timeout_seconds: 15
```

Start with `apt-proxy --config=apt-proxy.yaml`. `public_url` must be the fixed
HTTP(S) origin reachable from every client, without credentials or a path.
Incoming Host and forwarded headers never determine generated URLs. The example
uses HTTP on the client connection and certificate-verified HTTPS upstream.
An HTTPS `public_url` also works when the proxy's TLS listener and client trust
are configured. These are direct repository URLs, so DNF does not need a forward
proxy or a CONNECT tunnel for this feature.

Change each CentOS repository's Metalink origin to apt-proxy while retaining the
existing repository, architecture and protocol parameters. For BaseOS:

```ini
metalink=http://apt-cache.example:3142/metalink?repo=centos-baseos-$stream&arch=$basearch&protocol=https,http
```

Keep the repository's existing GPG settings and keys. DNF expands `$stream` and
`$basearch`; AppStream, CRB and other repositories keep their own `repo` value.
The endpoint accepts `repo`, `arch`, optional `protocol`, and DNF's `countme`
bucket (1..4). It forwards `countme` when discovery actually fetches the source,
but does not split the snapshot cache by that statistics parameter. Other query
fields and duplicate parameters are rejected.
Do not enable a competing `baseurl` or `mirrorlist` for the same repository.
After editing the repository files, run `dnf clean all` and `dnf makecache`.

## Mirror selection and cache identity

The source must return a bounded Metalink v3 document containing exactly one
`repomd.xml` file, a size, a strong SHA256 or SHA512 digest, and resources ending
in `/repodata/repomd.xml`. The proxy preserves the strong digests and timestamp
in the generated document. It considers only HTTP(S) mirrors on the explicit
`allowed_mirror_hosts` list. Unknown hosts and unsafe URLs are never fetched.
Choose hosts that the source actually returns for the requested repository;
having a host on the list does not add it to the source's candidates.

Eligible mirrors are ordered by advertised preference. A mirror must supply a
complete `repomd.xml` matching the advertised size and strong digests before it
can serve the snapshot. If one candidate fails, another candidate can be used
only after the same verification. A redirect, partial response, private response
or digest mismatch cannot establish a snapshot. Client metadata signature and
RPM checksum/signature verification remain DNF's responsibility.

The local snapshot token includes the configured policy, repository identity
and metadata digest. All clients using that snapshot share a canonical cache
namespace even if the equivalent origin mirror changes. Distinct repository,
architecture or metadata generations cannot share entries. Old issued routes
remain bound to their original metadata generation; refresh does not repoint
them to a new snapshot. Package and child metadata bytes are streamed through
the existing bounded proxy/cache path.

The feature accepts only public, empty-body GET/HEAD requests. Credentials,
cookies and custom token/tenant headers are rejected before cache lookup.
Upstream privacy directives are never promoted into a public cache entry.
All source and mirror connections use a direct DNS-pinned transport with
normal TLS verification and no redirect following. Environment proxy settings
cannot bypass the destination checks.

When `upstream_access` is configured, its host/port restrictions also apply;
metalink cannot widen them. Non-public addresses are rejected unless the
operator explicitly configured a narrow `upstream_access.allow_cidrs`
exception. The real DNF test fixture uses such a loopback exception inside its
disposable container; public deployments do not need it.

## Refresh and limits

Repository lists are fetched on demand and coalesced between concurrent
clients. `refresh_seconds` defaults to 600. Expired lists are refreshed before
a new document is served; an unavailable or invalid source cannot silently
supply a stale document. Shorter upstream freshness caps the discovery TTL;
source `no-cache` or `no-store` responses are not reused for discovery.
Successful SIGHUP or `POST /api/mirrors/refresh`
invalidates the current lists, and the next client request fetches them again.
The existing management API authentication applies. Configuration changes
require a restart.

The default limits are a 15-second operation timeout, 2 MiB source XML, 8 MiB
`repomd.xml`, and 64 current repository query combinations. Issued snapshot
routes expire after 24 hours, with at most four times `max_repositories`
generations retained. Capacity pressure fails closed instead of changing an
active client's snapshot. The YAML fields `timeout_seconds`, `max_xml_bytes`,
`max_repomd_bytes`, and `max_repositories` can lower these bounds; the server's
object and temporary resource limits still apply.

This endpoint does not make apt-proxy an unrestricted forward proxy. Keep using
fixed `/centos/...` base URLs when dynamic discovery is unnecessary. See
[real DNF tests](dnf-client-tests.md) for the signed Stream 9/10 fixture and
cache/failure assertions.
