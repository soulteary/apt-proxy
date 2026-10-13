#!/usr/bin/env bash
set -euo pipefail
mkdir -p /fixture/repo/Packages /fixture/gnupg /fixture/rpmbuild/{BUILD,RPMS,SOURCES,SPECS,SRPMS} /evidence
chmod 700 /fixture/gnupg
export GNUPGHOME=/fixture/gnupg
proxy_pid= origin_pid=
cleanup() {
  # Keep origin counters on failure too; otherwise an early client error could
  # erase the evidence needed to distinguish XML, routing, and TLS failures.
  if [ -n "$origin_pid" ]; then curl -fsS http://127.0.0.1:8080/__control/status > /evidence/final-status.json 2>/dev/null || true; fi
  if [ -n "$proxy_pid" ]; then kill "$proxy_pid" 2>/dev/null || true; wait "$proxy_pid" 2>/dev/null || true; fi
  if [ -n "$origin_pid" ]; then kill "$origin_pid" 2>/dev/null || true; wait "$origin_pid" 2>/dev/null || true; fi
}
trap cleanup EXIT
exec > >(tee /evidence/dnf-client.log) 2>&1
cat /etc/os-release
dnf --version
rpm --eval '%{_arch}' > /fixture/arch
source /etc/os-release
printf '%s\n' "${VERSION_ID%%.*}" > /fixture/stream
python3 - <<'PY'
from pathlib import Path
import random
Path('/fixture/rpmbuild/SOURCES/payload').write_bytes(random.Random(70).randbytes(2 * 1024 * 1024))
PY
cat > /fixture/rpmbuild/SPECS/apt-proxy-fixture.spec <<'EOF'
Name: apt-proxy-fixture
Version: 1.0
Release: 1
Summary: Local apt-proxy Metalink compatibility fixture
License: MIT
BuildArch: noarch
AutoReqProv: no
Source0: payload

%description
A dependency-free fixture for real DNF repository and cache validation.

%prep
%build
%install
mkdir -p %{buildroot}/usr/share/apt-proxy-fixture
cp %{SOURCE0} %{buildroot}/usr/share/apt-proxy-fixture/payload

%files
/usr/share/apt-proxy-fixture/payload
EOF
rpmbuild --define '_topdir /fixture/rpmbuild' -bb /fixture/rpmbuild/SPECS/apt-proxy-fixture.spec
gpg --batch --pinentry-mode loopback --passphrase '' --quick-generate-key 'DNF Fixture <fixture@example.invalid>' rsa2048 sign 1d
fingerprint=$(gpg --batch --with-colons --list-keys | awk -F: '$1 == "fpr" {print $10; exit}')
gpg --batch --armor --export "$fingerprint" > /fixture/repository.asc
rpmsign --define '_gpg_name '"$fingerprint" --define '_gpg_path /fixture/gnupg' \
  --define '__gpg /usr/bin/gpg' --define '_gpg_digest_algo sha256' \
  --addsign /fixture/rpmbuild/RPMS/noarch/apt-proxy-fixture-1.0-1.noarch.rpm
cp /fixture/rpmbuild/RPMS/noarch/*.rpm /fixture/repo/Packages/
rpm --import /fixture/repository.asc
rpm --checksig /fixture/repo/Packages/*.rpm | tee /evidence/rpm-signature.txt
createrepo_c --checksum sha256 /fixture/repo
gpg --batch --yes --pinentry-mode loopback --passphrase '' --armor --detach-sign \
  -o /fixture/repo/repodata/repomd.xml.asc /fixture/repo/repodata/repomd.xml
openssl req -x509 -newkey rsa:2048 -nodes -keyout /fixture/key.pem -out /fixture/cert.pem \
  -days 2 -subj '/CN=DNF Fixture' -addext 'subjectAltName=IP:127.0.0.1' >/dev/null 2>&1
cp /fixture/cert.pem /etc/pki/ca-trust/source/anchors/dnf-fixture.crt
update-ca-trust
python3 /tests/fixture.py > /evidence/origin.log 2>&1 & origin_pid=$!
for _ in {1..100}; do
  if curl -fsS http://127.0.0.1:8080/__control/status >/dev/null; then break; fi
  sleep .05
done
curl -fsS https://127.0.0.1:8443/mirror-b/repodata/repomd.xml -o /fixture/tls-repomd.xml
cmp /fixture/tls-repomd.xml /fixture/repo/repodata/repomd.xml
start_proxy() {
  if [ -n "$proxy_pid" ]; then kill "$proxy_pid"; wait "$proxy_pid"; proxy_pid=; fi
  local cache_dir; cache_dir=$(mktemp -d /fixture/cache.XXXXXX)
  cat > /fixture/apt-proxy.yaml <<EOF
server:
  host: 127.0.0.1
  port: 3142
mode: centos
mirrors:
  centos: http://127.0.0.1:8080/mirror-b/
cache:
  dir: "$cache_dir"
download:
  timeout_seconds: 30
upstream_access:
  enabled: true
  allowed_hosts: [127.0.0.1]
  allowed_ports: [8080, 8443]
  allow_cidrs: [127.0.0.0/8]
metalink:
  public_url: http://127.0.0.1:3142
  source_url: http://127.0.0.1:8080/metalink
  allowed_mirror_hosts: [127.0.0.1]
  refresh_seconds: 600
EOF
  /usr/local/bin/apt-proxy --config=/fixture/apt-proxy.yaml >> /evidence/proxy.log 2>&1 & proxy_pid=$!
  for _ in {1..100}; do
    if curl -fsS http://127.0.0.1:3142/healthz >/dev/null; then return; fi
    if ! kill -0 "$proxy_pid" 2>/dev/null; then cat /evidence/proxy.log; return 1; fi
    sleep .05
  done
  return 1
}
# Retain native DNF variables and metalink discovery; only the URL's origin
# changes. Public repositories are unavailable to every DNF test invocation.
cat > /etc/yum.repos.d/apt-proxy-fixture.repo <<'EOF'
[apt-proxy-fixture]
name=Local apt-proxy Metalink fixture
metalink=http://127.0.0.1:3142/metalink?repo=centos-baseos-$stream&arch=$basearch&protocol=https,http
enabled=1
gpgcheck=1
repo_gpgcheck=1
gpgkey=file:///fixture/repository.asc
sslverify=1
metadata_expire=0
countme=1
skip_if_unavailable=0
EOF
dnf_fixture() {
  local trace=$1; shift
  # DNF's GPG subprocess can leave gpg-agent alive after the transaction.
  # Keep DNF as the shell's child while its detached tracer follows children;
  # a signing-agent lifetime must not keep a completed DNF command waiting.
  strace -D -f -e trace=connect -s 512 -o "/evidence/$trace.connect.log" \
    dnf --disablerepo='*' --enablerepo=apt-proxy-fixture --setopt=metadata_timer_sync=0 -y "$@"
}
curl -fsS -X POST http://127.0.0.1:8080/__control/reset
curl -fsS -X POST http://127.0.0.1:8080/__control/fail-first
start_proxy
dnf_fixture makecache makecache --refresh
dnf_fixture install install apt-proxy-fixture
rpm -V apt-proxy-fixture
cmp /usr/share/apt-proxy-fixture/payload /fixture/rpmbuild/SOURCES/payload
curl -fsS http://127.0.0.1:8080/__control/status > /evidence/first-install.json
arch=$(cat /fixture/arch); stream=$(cat /fixture/stream)
curl -fsS "http://127.0.0.1:3142/metalink?repo=centos-baseos-$stream-stream&arch=$arch&protocol=https,http" > /evidence/rewritten-metalink.xml
python3 - <<'PY'
import json
import urllib.parse
import xml.etree.ElementTree as ET
state = json.load(open('/evidence/first-install.json'))
requests = state['requests']
assert any(r['path'] == '/mirror-a/repodata/repomd.xml' for r in requests), requests
assert any(r['path'] == '/mirror-b/repodata/repomd.xml' for r in requests), requests
assert any(r['path'].endswith('.rpm') and r['method'] == 'GET' for r in requests), requests
assert any(r['path'].endswith('/repomd.xml.asc') for r in requests), requests
urls = [e.text for e in ET.parse('/evidence/rewritten-metalink.xml').iter()
        if e.tag.rsplit('}', 1)[-1] == 'url']
assert urls, 'No rewritten resource URL'
for url in urls:
    parsed = urllib.parse.urlsplit(url)
    assert parsed.scheme == 'http' and parsed.netloc == '127.0.0.1:3142', url
    assert parsed.path.startswith('/metalink/repos/') and parsed.path.endswith('/repodata/repomd.xml'), url
PY
# Remove the client's package and metadata caches. A second real download must
# come from apt-proxy, rather than DNF's own package cache.
dnf_fixture clean clean all
dnf_fixture reinstall reinstall apt-proxy-fixture
cmp /usr/share/apt-proxy-fixture/payload /fixture/rpmbuild/SOURCES/payload
curl -fsS http://127.0.0.1:8080/__control/status > /evidence/second-install.json
python3 - <<'PY'
import json
def rpm_gets(filename):
    return sum(r['method'] == 'GET' and r['path'].endswith('.rpm')
               for r in json.load(open(filename))['requests'])
before = rpm_gets('/evidence/first-install.json')
after = rpm_gets('/evidence/second-install.json')
assert before > 0 and after == before, (before, after)
with open('/evidence/cache-hit.json', 'w') as out:
    json.dump({'first_origin_rpm_gets': before, 'second_origin_rpm_gets': after,
               'client_cache_cleared': True, 'payload_matches': True}, out, indent=2)
PY
# A fresh proxy cache prevents a previous validated snapshot from masking an
# invalid advertised hash. Neither mirror matches the Metalink's digest.
curl -fsS -X POST http://127.0.0.1:8080/__control/bad-hash
start_proxy
dnf_fixture integrity-clean clean all
if dnf_fixture integrity makecache --refresh; then
  echo 'DNF unexpectedly accepted a mismatched Metalink repomd digest' >&2
  exit 1
fi
curl -fsS http://127.0.0.1:8080/__control/status > /evidence/rejected-hash.json
python3 - <<'PY'
import json
from pathlib import Path
state = json.load(open('/evidence/rejected-hash.json'))
assert any(r['path'].endswith('/repomd.xml') for r in state['requests']), state
assert not any(r['path'].endswith('.rpm') or (r['path'].startswith('/mirror-')
              and not r['path'].endswith('/repomd.xml')) for r in state['requests']), state
# DNF and all descendants can connect only to the proxy. The proxy's separate
# process contacts the HTTP source and certificate-verified HTTPS mirrors.
internet_connections = []
for phase in ('makecache', 'install', 'clean', 'reinstall', 'integrity-clean', 'integrity'):
    trace = Path('/evidence') / (phase + '.connect.log')
    for line in trace.read_text().splitlines():
        if 'sa_family=AF_INET' in line:
            internet_connections.append({'trace': trace.name, 'syscall': line})
            assert 'sa_family=AF_INET,' in line, line
            assert 'sin_port=htons(3142)' in line, line
            assert 'sin_addr=inet_addr("127.0.0.1")' in line, line
assert internet_connections, 'DNF made no observed network connections'
with open('/evidence/client-connections.json', 'w') as out:
    json.dump(internet_connections, out, indent=2)
PY
cp /var/log/dnf.librepo.log /evidence/dnf.librepo.log
printf 'PASS: native DNF Metalink, signed metadata and RPM installation, verified upstream TLS, mirror fallback, warm proxy cache, digest rejection, no direct DNF origin connections\n' | tee /evidence/result.txt
