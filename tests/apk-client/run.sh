#!/usr/bin/env bash
set -euo pipefail
mkdir -p /fixture/build/apt-proxy-fixture /fixture/keys /fixture/valid /fixture/traces /fixture/client-cache /evidence /root/.abuild
proxy_pid= origin_pid=
cleanup() {
  if [ -n "$origin_pid" ]; then curl -fsS http://127.0.0.1:8080/__control/status > /evidence/final-status.json 2>/dev/null || true; fi
  if [ -n "$proxy_pid" ]; then kill "$proxy_pid" 2>/dev/null || true; wait "$proxy_pid" 2>/dev/null || true; fi
  if [ -n "$origin_pid" ]; then kill "$origin_pid" 2>/dev/null || true; wait "$origin_pid" 2>/dev/null || true; fi
}
trap cleanup EXIT
exec > >(tee /evidence/apk-client.log) 2>&1
cat /etc/os-release
apk --version
apk --version > /evidence/client-version.txt
arch=$(apk --print-arch)
repo="/fixture/repo/alpine/fixture/main/$arch"
mkdir -p "$repo"
openssl genrsa -out /fixture/keys/fixture.rsa 2048
openssl rsa -in /fixture/keys/fixture.rsa -pubout -out /fixture/keys/fixture.rsa.pub
cp /fixture/keys/fixture.rsa.pub /etc/apk/keys/fixture.rsa.pub
printf 'PACKAGER_PRIVKEY=/fixture/keys/fixture.rsa\nPACKAGER="APK Fixture <fixture@example.invalid>"\nREPODEST=/fixture/packages\n' > /root/.abuild/abuild.conf
python3 - <<'PY'
from pathlib import Path
import random
Path('/fixture/build/apt-proxy-fixture/payload').write_bytes(random.Random(80).randbytes(512*1024))
PY
cat > /fixture/build/apt-proxy-fixture/APKBUILD <<'EOF'
pkgname=apt-proxy-fixture
pkgver=1.0
pkgrel=0
pkgdesc="Local signed APK cache fixture"
url="https://example.invalid"
arch="noarch"
license="MIT"
options="!check !strip !tracedeps"
source="payload"
package() {
  install -D -m644 "$srcdir/payload" "$pkgdir/usr/share/apt-proxy-fixture/payload"
}
EOF
# The image already contains the build tools for this local data recipe. -d
# skips dependency installation, while source verification and signing remain.
(cd /fixture/build/apt-proxy-fixture && abuild -F checksum && abuild -F -d)
find /fixture/packages -name 'apt-proxy-fixture-*.apk' -exec cp '{}' "$repo/" ';'
package=$(find "$repo" -name '*.apk' | head -1)
test -n "$package"
make_index() {
  # APK chooses the package subdirectory from the index architecture. Match
  # abuild's repository layout by indexing this noarch package for the client.
  (cd "$repo" && apk index --rewrite-arch "$arch" -o APKINDEX.tar.gz ./*.apk && abuild-sign -k /fixture/keys/fixture.rsa -p /fixture/keys/fixture.rsa.pub APKINDEX.tar.gz)
}
make_index
cp "$repo/APKINDEX.tar.gz" /fixture/valid/APKINDEX.tar.gz
cp "$package" /fixture/valid/package.apk
openssl req -x509 -newkey rsa:2048 -nodes -keyout /fixture/key.pem -out /fixture/cert.pem \
  -days 2 -subj '/CN=APK Fixture' -addext 'subjectAltName=IP:127.0.0.1,DNS:apk-fixture.invalid' >/dev/null 2>&1
printf '127.0.0.1 apk-fixture.invalid\n' >> /etc/hosts
cp /fixture/cert.pem /usr/local/share/ca-certificates/apk-fixture.crt
update-ca-certificates
python3 /tests/fixture.py > /evidence/origin.log 2>&1 & origin_pid=$!
for _ in {1..100}; do
  if curl -fsS http://127.0.0.1:8080/__control/status >/dev/null; then break; fi
  sleep .05
done
start_proxy() {
  if [ -n "$proxy_pid" ]; then kill "$proxy_pid"; wait "$proxy_pid"; proxy_pid=; fi
  local cache_dir; cache_dir=$(mktemp -d /fixture/cache.XXXXXX)
  cat > /fixture/apt-proxy.yaml <<EOF
server:
  host: 127.0.0.1
  port: 3142
mode: alpine
mirrors:
  alpine: https://127.0.0.1:8443/alpine/
cache:
  dir: "$cache_dir"
tls:
  enabled: true
  cert_file: /fixture/cert.pem
  key_file: /fixture/key.pem
upstream_access:
  enabled: true
  allowed_hosts: [127.0.0.1]
  allowed_ports: [8443]
  allow_cidrs: [127.0.0.0/8]
EOF
  cp /fixture/apt-proxy.yaml /evidence/proxy-config.yaml
  /usr/local/bin/apt-proxy --config=/fixture/apt-proxy.yaml >> /evidence/proxy.log 2>&1 & proxy_pid=$!
  for _ in {1..100}; do
    if curl -fsS https://127.0.0.1:3142/healthz >/dev/null; then return; fi
    if ! kill -0 "$proxy_pid" 2>/dev/null; then cat /evidence/proxy.log; return 1; fi
    sleep .05
  done
  return 1
}
# Native repositories only: no public archive may hide an unsupported fixture.
printf 'https://apk-fixture.invalid:3142/alpine/fixture/main\n' > /etc/apk/repositories
if [ -d /etc/apk/repositories.d ]; then rm -f /etc/apk/repositories.d/*; fi
apk_fixture() {
  local phase=$1; shift
  strace -f -e trace=connect -s 512 -o "/fixture/traces/$phase.connect.log" \
    apk --cache-dir /fixture/client-cache "$@"
}
start_proxy
apk_fixture update update
apk_fixture install add apt-proxy-fixture
cmp /usr/share/apt-proxy-fixture/payload /fixture/build/apt-proxy-fixture/payload
curl -fsS http://127.0.0.1:8080/__control/status > /evidence/first-install.json
apk_fixture remove del apt-proxy-fixture
rm -rf /fixture/client-cache /var/cache/apk/*
mkdir -p /fixture/client-cache
apk_fixture second-update update
apk_fixture reinstall add apt-proxy-fixture
cmp /usr/share/apt-proxy-fixture/payload /fixture/build/apt-proxy-fixture/payload
curl -fsS http://127.0.0.1:8080/__control/status > /evidence/second-install.json
python3 - <<'PY'
import json
def package_gets(path):
    return sum(r['method']=='GET' and r['path'].endswith('.apk') for r in json.load(open(path))['requests'])
before=package_gets('/evidence/first-install.json'); after=package_gets('/evidence/second-install.json')
assert before >= 1 and after == before, (before,after)
json.dump({'first_origin_package_gets':before,'second_origin_package_gets':after,
           'client_cache_cleared':True,'payload_matches':True},open('/evidence/cache-hit.json','w'),indent=2)
PY
apk_fixture final-remove del apt-proxy-fixture
tamper_signature() {
  python3 - "$1" <<'PY'
import gzip,sys,zlib
from pathlib import Path
path=Path(sys.argv[1]); data=path.read_bytes()
stream=zlib.decompressobj(31); fragment=bytearray(stream.decompress(data))
assert fragment[:100].split(b'\0')[0].startswith(b'.SIGN.'), 'Not a signed legacy APK gzip member'
assert int(fragment[124:136].strip(b'\0 '),8)>0 and len(fragment)>512
fragment[512] ^= 1
path.write_bytes(gzip.compress(bytes(fragment),mtime=0)+stream.unused_data)
PY
}
tamper_signature "$repo/APKINDEX.tar.gz"
rm -rf /fixture/client-cache; mkdir -p /fixture/client-cache
start_proxy
if apk_fixture rejected-index update > /evidence/rejected-index.log 2>&1; then
  echo 'APK accepted an invalid index signature' >&2; exit 1
fi
grep -Ei 'signature|UNTRUSTED' /evidence/rejected-index.log
cp /fixture/valid/APKINDEX.tar.gz "$repo/APKINDEX.tar.gz"
tamper_signature "$package"
# Rebuild the signed index so its advertised size reflects the corrupted
# signature member; rejection must come from package verification itself.
make_index
rm -rf /fixture/client-cache; mkdir -p /fixture/client-cache
start_proxy
apk_fixture valid-index update
if apk_fixture rejected-package add apt-proxy-fixture > /evidence/rejected-package.log 2>&1; then
  echo 'APK accepted an invalid package signature' >&2; exit 1
fi
grep -Ei 'signature|UNTRUSTED' /evidence/rejected-package.log
python3 - <<'PY'
import json
from pathlib import Path
connections=[]
phases=('update','install','remove','second-update','reinstall','final-remove','rejected-index','valid-index','rejected-package')
for phase in phases:
    trace=Path('/fixture/traces') / (phase+'.connect.log')
    for line in trace.read_text().splitlines():
        if 'sa_family=AF_INET' in line:
            assert 'sa_family=AF_INET,' in line and 'sin_port=htons(3142)' in line, line
            assert 'sin_addr=inet_addr("127.0.0.1")' in line, line
            connections.append({'phase':trace.name,'syscall':line})
    (Path('/evidence')/trace.name).write_bytes(trace.read_bytes())
assert connections
json.dump(connections,open('/evidence/client-connections.json','w'),indent=2)
json.dump({'signed_index':True,'signed_package':True,'tampered_index_rejected':True,
           'tampered_package_rejected':True,'tls_verification':True},open('/evidence/signatures.json','w'),indent=2)
PY
printf 'PASS: native APK signed update/install, proxy package cache hit after client-cache removal, index/package signature rejection, verified TLS, no direct origin connections\n' | tee /evidence/result.txt
