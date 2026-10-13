#!/usr/bin/env bash
set -euo pipefail
umask 022
mkdir -p /fixture/repo/Packages /fixture/gnupg /fixture/rpmbuild/{BUILD,RPMS,SOURCES,SPECS,SRPMS} /fixture/traces /evidence
chmod 700 /fixture/gnupg
export GNUPGHOME=/fixture/gnupg
export LC_ALL=C
proxy_pid= origin_pid=
cleanup() {
  if [ -n "$origin_pid" ]; then curl -fsS http://127.0.0.1:8080/__control/status > /evidence/final-status.json 2>/dev/null || true; fi
  if [ -f /var/log/zypper.log ]; then
    cp /var/log/zypper.log /evidence/zypper.log
    chmod 0644 /evidence/zypper.log
  fi
  if [ -n "$proxy_pid" ]; then kill "$proxy_pid" 2>/dev/null || true; wait "$proxy_pid" 2>/dev/null || true; fi
  if [ -n "$origin_pid" ]; then kill "$origin_pid" 2>/dev/null || true; wait "$origin_pid" 2>/dev/null || true; fi
}
trap cleanup EXIT
exec > >(tee /evidence/zypper-client.log) 2>&1
cat /etc/os-release
zypper --version
rpm --version
sha256sum /usr/local/bin/apt-proxy > /evidence/binary-sha256.txt
python3 - <<'PY'
from pathlib import Path
import random
Path('/fixture/rpmbuild/SOURCES/payload').write_bytes(random.Random(160).randbytes(2 * 1024 * 1024))
PY
cat > /fixture/rpmbuild/SPECS/apt-proxy-fixture.spec <<'EOF'
Name: apt-proxy-fixture
Version: 1.0
Release: 1
Summary: Local apt-proxy zypper compatibility fixture
License: MIT
BuildArch: noarch
AutoReqProv: no
Source0: payload

%description
A dependency-free fixture for real zypper repository and cache validation.

%prep
%build
%install
mkdir -p %{buildroot}/usr/share/apt-proxy-fixture
cp %{SOURCE0} %{buildroot}/usr/share/apt-proxy-fixture/payload

%files
/usr/share/apt-proxy-fixture/payload
EOF
rpmbuild --define '_topdir /fixture/rpmbuild' -bb /fixture/rpmbuild/SPECS/apt-proxy-fixture.spec
gpg --batch --pinentry-mode loopback --passphrase '' --quick-generate-key 'Zypper Fixture <fixture@example.invalid>' rsa2048 sign 1d
fingerprint=$(gpg --batch --with-colons --list-keys | awk -F: '$1 == "fpr" {print $10; exit}')
gpg --batch --armor --export "$fingerprint" > /fixture/repository.asc
rpmsign --define '_gpg_name '"$fingerprint" --define '_gpg_path /fixture/gnupg' \
  --define "__gpg $(command -v gpg)" --define '_gpg_digest_algo sha256' \
  --addsign /fixture/rpmbuild/RPMS/noarch/apt-proxy-fixture-1.0-1.noarch.rpm
cp /fixture/rpmbuild/RPMS/noarch/*.rpm /fixture/repo/Packages/
rpm --import /fixture/repository.asc
rpm --checksig /fixture/repo/Packages/*.rpm | tee /evidence/rpm-signature.txt
grep -qi 'signatures OK' /evidence/rpm-signature.txt
createrepo_c --checksum sha256 /fixture/repo
gpg --batch --yes --pinentry-mode loopback --passphrase '' --armor --detach-sign \
  -o /fixture/repo/repodata/repomd.xml.asc /fixture/repo/repodata/repomd.xml
cp /fixture/repository.asc /fixture/repo/repodata/repomd.xml.key
openssl req -x509 -newkey rsa:2048 -nodes -keyout /fixture/key.pem -out /fixture/cert.pem \
  -days 2 -subj '/CN=Zypper Fixture' -addext 'subjectAltName=IP:127.0.0.1' > /dev/null 2>&1
chmod 600 /fixture/key.pem
mkdir -p /etc/pki/trust/anchors
cp /fixture/cert.pem /etc/pki/trust/anchors/zypper-fixture.crt
update-ca-certificates
python3 /tests/fixture.py > /evidence/origin.log 2>&1 & origin_pid=$!
for _ in {1..100}; do
  if curl -fsS http://127.0.0.1:8080/__control/status >/dev/null; then break; fi
  sleep .05
 done
curl -fsS https://127.0.0.1:8443/archive/fixture/repodata/repomd.xml -o /fixture/tls-repomd.xml
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
  centos: https://127.0.0.1:8443/archive/
cache:
  dir: "$cache_dir"
download:
  timeout_seconds: 30
upstream_access:
  enabled: true
  allowed_hosts: [127.0.0.1]
  allowed_ports: [8443]
  allow_cidrs: [127.0.0.0/8]
EOF
  cp /fixture/apt-proxy.yaml /evidence/proxy-config.yaml
  /usr/local/bin/apt-proxy --config=/fixture/apt-proxy.yaml >> /evidence/proxy.log 2>&1 & proxy_pid=$!
  for _ in {1..100}; do
    if curl -fsS http://127.0.0.1:3142/healthz >/dev/null; then return; fi
    if ! kill -0 "$proxy_pid" 2>/dev/null; then cat /evidence/proxy.log; return 1; fi
    sleep .05
  done
  return 1
}
# No service or vendor repository may hide a local repository/cache failure.
rm -f /etc/zypp/repos.d/*.repo /etc/zypp/services.d/*
cat > /etc/zypp/repos.d/apt-proxy-fixture.repo <<'EOF'
[apt-proxy-fixture]
name=Local apt-proxy zypper fixture
baseurl=http://127.0.0.1:3142/centos/fixture/
enabled=1
autorefresh=0
type=rpm-md
gpgcheck=1
repo_gpgcheck=1
pkg_gpgcheck=1
gpgkey=file:///fixture/repository.asc
keeppackages=1
EOF
cp /etc/zypp/repos.d/apt-proxy-fixture.repo /evidence/client-repo.conf
zypper_fixture() {
  local trace=$1; shift
  local status_file="/evidence/$trace.exit-status" tracer_pid client_rc
  # Trace the client in a child wrapper that records its real exit status.
  # Terminate only this tracer once zypper is done, so retained GPG agents
  # cannot keep output buffers open or delay a completed command.
  ZYPPER_PHASE_STATUS="$status_file" strace -ff -e trace=connect -s 512 \
    -o "/fixture/traces/$trace.connect.log" bash -c '
      zypper --non-interactive "$@"
      result=$?
      printf "%s\n" "$result" > "$ZYPPER_PHASE_STATUS.tmp"
      mv "$ZYPPER_PHASE_STATUS.tmp" "$ZYPPER_PHASE_STATUS"
      exit "$result"
    ' zypper-phase "$@" > "/evidence/$trace.zypper.log" 2>&1 &
  tracer_pid=$!
  for _ in {1..1200}; do
    if [ -f "$status_file" ] || ! kill -0 "$tracer_pid" 2>/dev/null; then break; fi
    sleep .05
  done
  kill -TERM "$tracer_pid" 2>/dev/null || true
  wait "$tracer_pid" 2>/dev/null || true
  cat "/evidence/$trace.zypper.log"
  if [ ! -f "$status_file" ]; then
    echo "Zypper phase did not complete within its trace deadline: $trace" >&2
    return 1
  fi
  client_rc=$(cat "$status_file")
  return "$client_rc"
}
clear_client_cache() {
  local trace=$1
  zypper_fixture "$trace" clean --all
  # Explicitly verify that no retained RPM can satisfy the second installation.
  python3 - <<'PY'
from pathlib import Path
packages = list(Path('/var/cache/zypp/packages').rglob('*.rpm'))
assert not packages, packages
PY
}
curl -fsS -X POST http://127.0.0.1:8080/__control/reset
start_proxy
zypper_fixture refresh refresh --force apt-proxy-fixture
zypper_fixture install install --no-recommends apt-proxy-fixture
rpm -V apt-proxy-fixture
cmp /usr/share/apt-proxy-fixture/payload /fixture/rpmbuild/SOURCES/payload
curl -fsS http://127.0.0.1:8080/__control/status > /evidence/first-install.json
clear_client_cache clean
zypper_fixture reinstall install --force --no-recommends apt-proxy-fixture
rpm -V apt-proxy-fixture
cmp /usr/share/apt-proxy-fixture/payload /fixture/rpmbuild/SOURCES/payload
curl -fsS http://127.0.0.1:8080/__control/status > /evidence/second-install.json
python3 - <<'PY'
import json

def rpm_gets(name):
    return sum(row['method'] == 'GET' and row['path'].endswith('.rpm')
               for row in json.load(open(name))['requests'])
before = rpm_gets('/evidence/first-install.json')
after = rpm_gets('/evidence/second-install.json')
assert before > 0 and after == before, (before, after)
with open('/evidence/cache-hit.json', 'w') as out:
    json.dump({'first_origin_package_gets': before, 'second_origin_package_gets': after,
               'first_origin_rpm_gets': before, 'second_origin_rpm_gets': after,
               'client_cache_cleared': True, 'payload_matches': True}, out, indent=2)
PY
# Keep XML valid but change signed bytes; normal GPG verification must reject it.
curl -fsS -X POST http://127.0.0.1:8080/__control/tamper-metadata
start_proxy
clear_client_cache metadata-clean
if zypper_fixture bad-metadata refresh --force apt-proxy-fixture; then
  echo 'Zypper unexpectedly accepted tampered signed repomd.xml' >&2; exit 1
fi
grep -qi 'signature verification failed' /evidence/bad-metadata.zypper.log
curl -fsS http://127.0.0.1:8080/__control/status > /evidence/rejected-metadata.json
python3 - <<'PY'
import json
rows = json.load(open('/evidence/rejected-metadata.json'))['requests']
assert any(r['path'].endswith('/repomd.xml') for r in rows), rows
assert any(r['path'].endswith('/repomd.xml.asc') for r in rows), rows
assert not any(r['path'].endswith('.rpm') for r in rows), rows
PY
# Restore trusted metadata while damaging only RPM bytes; a new proxy/client
# cache prevents earlier validated content masking the checksum/signature error.
curl -fsS -X POST http://127.0.0.1:8080/__control/tamper-package
start_proxy
clear_client_cache package-clean
zypper_fixture package-refresh refresh --force apt-proxy-fixture
if zypper_fixture bad-package install --force --no-recommends apt-proxy-fixture; then
  echo 'Zypper unexpectedly installed the damaged signed RPM' >&2; exit 1
fi
grep -Eqi 'signature verification failed|checksum.*(wrong|mismatch)|does not verify' /evidence/bad-package.zypper.log
curl -fsS http://127.0.0.1:8080/__control/status > /evidence/rejected-package.json
python3 - <<'PY'
import json
rows = json.load(open('/evidence/rejected-package.json'))['requests']
assert any(r['method'] == 'GET' and r['path'].endswith('.rpm') for r in rows), rows
PY
cmp /usr/share/apt-proxy-fixture/payload /fixture/rpmbuild/SOURCES/payload
# A positive restoration control proves failed integrity phases were not merely
# a bad command, untrusted key, broken route or TLS handshake failure.
curl -fsS -X POST http://127.0.0.1:8080/__control/reset
start_proxy
clear_client_cache restored-clean
zypper_fixture restored-refresh refresh --force apt-proxy-fixture
zypper_fixture restored-install install --force --no-recommends apt-proxy-fixture
rpm -V apt-proxy-fixture
cmp /usr/share/apt-proxy-fixture/payload /fixture/rpmbuild/SOURCES/payload
python3 - <<'PY'
import json
from pathlib import Path
import shutil
connections = []
for trace in sorted(Path('/fixture/traces').glob('*.connect.log.*')):
    for line in trace.read_text().splitlines():
        if 'sa_family=AF_INET' in line:
            connections.append({'trace': trace.name, 'syscall': line})
            assert 'sa_family=AF_INET,' in line, line
            assert 'sin_port=htons(3142)' in line, line
            assert 'sin_addr=inet_addr("127.0.0.1")' in line, line
    exported = Path('/evidence') / trace.name
    shutil.copy2(trace, exported)
    exported.chmod(0o644)
assert connections, 'No zypper network connections observed'
with open('/evidence/client-connections.json', 'w') as out:
    json.dump(connections, out, indent=2)
PY
printf 'PASS: native zypper signed rpm-md refresh/install, trusted upstream TLS, byte-verified payload, warm proxy cache after client cleanup, tampered metadata/RPM rejection, restoration, no direct client origin connections\n' | tee /evidence/result.txt
