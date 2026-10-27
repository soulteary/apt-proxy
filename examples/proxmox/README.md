# Proxmox Backup Client

Cache the Proxmox Backup Client repository while retaining its official APT
source URL. This example addresses the `/debian/` path collision reported in
[issue #167](https://github.com/soulteary/apt-proxy/issues/167).

**Version availability:** the original-source setup below requires v0.19.2
or later, which includes the host-and-path routing fix for #167. The Compose
example builds this checkout locally. Older releases do not include this fix;
use the unique local path alias below when upgrading is not yet possible.
Review the [upgrade notes](../../docs/releases/v0.19.2.md) for distribution
configuration validation changes.

## Run

From this repository:

```bash
cd examples/proxmox
docker compose up --build -d
```

The example builds `docker/Dockerfile` from this checkout, mounts
[`distributions.yaml`](distributions.yaml) explicitly and enables custom
distributions with `--mode=all`.

Alternatively, build and run the binary from the repository root:

```bash
make build
./dist/apt-proxy --mode=all \
  --distributions-config=./examples/proxmox/distributions.yaml
```

## Client setup: keep the official source

The [official Proxmox Backup documentation](https://pbs.proxmox.com/docs/installation.html#apt-based-proxmox-backup-client-repository)
documents this client-only repository for Debian Trixie. Configure the Proxmox
signing key as described there, then use
`/etc/apt/sources.list.d/pbs-client.sources`:

```text
Types: deb
URIs: http://download.proxmox.com/debian/pbs-client
Suites: trixie
Components: main
Signed-By: /usr/share/keyrings/proxmox-archive-keyring.gpg
```

Set `/etc/apt/apt.conf.d/01proxy` to the address of your apt-proxy server:

```text
Acquire::http::Proxy "http://apt-proxy.example:3142";
```

Then run `apt update`. This is the HTTP repository URL documented by Proxmox;
the repository signatures remain enabled.

The Proxmox host and `/debian/pbs-client/` path together select this custom
entry before the generic Debian `/debian/` route. The final regex capture is
`dists/trixie/InRelease`, for example, and the mirror root is
`http://download.proxmox.com/debian/pbs-client/`. Their concatenation preserves
the original upstream URL.

## Optional direct proxy URL

The same configuration accepts a unique local path alias. Instead of setting
the global HTTP proxy, change only the `URIs` line above to:

```text
URIs: http://apt-proxy.example:3142/proxmox-pbs-client
```

Keep the suite, component and signing key unchanged. The dedicated
`/proxmox-pbs-client/` prefix avoids the generic Debian `/debian/` route. This
path form is also a workaround for releases that support custom distributions
but do not yet contain the host-and-path fix.

## Verify routing and upstream availability

Replace `127.0.0.1:3142` if the proxy runs on another machine. These commands
compare a direct origin request with the same request through apt-proxy:

```bash
curl --noproxy '*' -sS -D - -o /dev/null \
  http://download.proxmox.com/debian/pbs-client/dists/trixie/Release

curl --noproxy '' --proxy http://127.0.0.1:3142 -sS -D - -o /dev/null \
  http://download.proxmox.com/debian/pbs-client/dists/trixie/Release

curl --noproxy '*' -sS -D - -o /dev/null \
  http://127.0.0.1:3142/proxmox-pbs-client/dists/trixie/Release
```

A healthy reachable origin should serve the same repository metadata through
all three URLs. These are diagnostic commands, not a claim that the public
origin was contacted by the automated tests. A `404` alone does not identify
the cause: it can come from the upstream mirror, a request routed to the wrong
distribution, or an unmatched local path/cache rule. Compare the direct and
proxied responses and inspect the proxy logs; enable `--debug` when diagnosing
the rewritten URL. `cache: SKIP` by itself does not prove that the custom
configuration was not loaded.

For the original-source form, confirm that the client sends
`Host: download.proxmox.com` (an optional port is allowed) and that the running
binary contains the routing fix. Also check the explicit configuration path
and `--mode=all`.

## Repository scope and configuration details

This example defines **PBS Client only**. Proxmox's repositories share a host,
but their paths and components differ:

| Repository | Official HTTP path | Trixie component |
| --- | --- | --- |
| PBS Client | `/debian/pbs-client` | `main` |
| PBS Server without a subscription | `/debian/pbs` | `pbs-no-subscription` |
| PVE without a subscription | `/debian/pve` | `pve-no-subscription` |

See the official [PBS repository documentation](https://pbs.proxmox.com/docs/installation.html#debian-package-repositories)
and [PVE repository documentation](https://pve.proxmox.com/wiki/Package_Repositories)
before adding separate entries. Do not replace a PBS Client source with a PVE
source to work around a proxy routing problem.

- `benchmark_url` is relative to the configured mirror root. Here it produces
  `http://download.proxmox.com/debian/pbs-client/dists/trixie/Release`.
- The YAML key is `cache_control`, with an underscore. Keep the catch-all cache
  rule last so APT indexes and by-hash requests are also handled.
- `type: 11` must be unused in your combined configuration. Keep each custom
  type stable and distinct from built-in types `0` through `5`.
- `host_pattern` helps select the repository; it is not an access-control
  rule. The local alias deliberately works with the proxy's own host name.
  See [download access controls](../../docs/download-access.md) for client
  access restrictions.
