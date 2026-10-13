#!/usr/bin/env bash
set -euo pipefail
mkdir -p /fixture/build /fixture/repo/pacman/x86_64 /fixture/valid /fixture/gnupg /evidence
proxy_pid= origin_pid=
cleanup() {
  if [ -n "$origin_pid" ]; then curl -fsS http://127.0.0.1:8080/__control/status > /evidence/final-status.json 2>/dev/null || true; fi
  if [ -n "$proxy_pid" ]; then kill "$proxy_pid" 2>/dev/null || true; wait "$proxy_pid" 2>/dev/null || true; fi
  if [ -n "$origin_pid" ]; then kill "$origin_pid" 2>/dev/null || true; wait "$origin_pid" 2>/dev/null || true; fi
}
trap cleanup EXIT
exec > >(tee /evidence/pacman-client.log) 2>&1
cat /etc/os-release
pacman --version
pacman --version > /evidence/client-version.txt
useradd --create-home builder
chown -R builder:builder /fixture/build /fixture/gnupg
chmod 700 /fixture/gnupg
export GNUPGHOME=/fixture/gnupg
su builder -c "gpg --batch --pinentry-mode loopback --passphrase '' --quick-generate-key 'Pacman Fixture <fixture@example.invalid>' rsa2048 sign 1d"
fingerprint=$(su builder -c 'gpg --batch --with-colons --list-keys' | awk -F: '$1=="fpr" {print $10; exit}')
su builder -c "gpg --batch --armor --export '$fingerprint'" > /fixture/repository.asc
python3 - <<'PY'
from pathlib import Path
import random
Path('/fixture/build/payload').write_bytes(random.Random(90).randbytes(512*1024))
PY
chown builder:builder /fixture/build/payload
cat > /fixture/build/PKGBUILD <<'EOF'
pkgname=apt-proxy-fixture
pkgver=1.0
pkgrel=1
pkgdesc='Local signed pacman cache fixture'
arch=('any')
url='https://example.invalid'
license=('MIT')
options=('!strip' '!debug')
source=('payload')
sha256sums=('SKIP')
package() {
  install -Dm644 "$srcdir/payload" "$pkgdir/usr/share/apt-proxy-fixture/payload"
}
EOF
payload_hash=$(sha256sum /fixture/build/payload | cut -d' ' -f1)
sed -i "s/sha256sums=('SKIP')/sha256sums=('$payload_hash')/" /fixture/build/PKGBUILD
# Makepkg runs as an unprivileged user, with a local data-only source.
su builder -c "cd /fixture/build && makepkg --nodeps --nocheck --sign --key '$fingerprint'"
repo=/fixture/repo/pacman/x86_64
cp /fixture/build/*.pkg.tar.* "$repo/"
chown -R builder:builder "$repo"
package=$(find "$repo" -name '*.pkg.tar.zst' | head -1)
test -n "$package"
make_database() {
  rm -f "$repo"/fixture.db* "$repo"/fixture.files*
  su builder -c "repo-add --sign --key '$fingerprint' '$repo/fixture.db.tar.gz' '$package'"
}
make_database
cp -L "$repo/fixture.db" /fixture/valid/fixture.db
cp -L "$repo/fixture.db.sig" /fixture/valid/fixture.db.sig
cp "$package.sig" /fixture/valid/package.sig
pacman-key --init
pacman-key --add /fixture/repository.asc
pacman-key --lsign-key "$fingerprint"
openssl req -x509 -newkey rsa:2048 -nodes -keyout /fixture/key.pem -out /fixture/cert.pem \
  -days 2 -subj '/CN=Pacman Fixture' -addext 'subjectAltName=IP:127.0.0.1' >/dev/null 2>&1
cp /fixture/cert.pem /etc/ca-certificates/trust-source/anchors/pacman-fixture.crt
update-ca-trust
python3 /tests/fixture.py > /evidence/origin.log 2>&1 & origin_pid=$!
for _ in {1..100}; do
  if curl -fsS http://127.0.0.1:8080/__control/status >/dev/null; then break; fi
  sleep .05
done
# This operator mapping explicitly replaces the selected routing slot with a
# bounded pacman path. It does not add a built-in public Arch mirror mode.
cat > /fixture/distributions.yaml <<'EOF'
distributions:
  - id: alpine
    name: Local pacman fixture mapping
    type: 5
    url_pattern: '^/pacman/(.+)$'
    benchmark_url: health.txt
    cache_rules:
      - pattern: '[.]db([.]sig)?$'
        cache_control: 'max-age=3600'
        rewrite: true
      - pattern: '[.]pkg[.]tar[.](zst|xz|gz)([.]sig)?$'
        cache_control: 'max-age=3600'
        rewrite: true
    mirrors:
      official: ['https://127.0.0.1:8443/pacman/']
EOF
start_proxy() {
  if [ -n "$proxy_pid" ]; then kill "$proxy_pid"; wait "$proxy_pid"; proxy_pid=; fi
  local cache_dir; cache_dir=$(mktemp -d /fixture/cache.XXXXXX)
  cat > /fixture/apt-proxy.yaml <<EOF
server:
  host: 127.0.0.1
  port: 3142
mode: alpine
distributions_config: /fixture/distributions.yaml
mirrors:
  alpine: https://127.0.0.1:8443/pacman/
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
  cp /fixture/distributions.yaml /evidence/distributions.yaml
  /usr/local/bin/apt-proxy --config=/fixture/apt-proxy.yaml >> /evidence/proxy.log 2>&1 & proxy_pid=$!
  for _ in {1..100}; do
    if curl -fsS https://127.0.0.1:3142/healthz >/dev/null; then return; fi
    if ! kill -0 "$proxy_pid" 2>/dev/null; then cat /evidence/proxy.log; return 1; fi
    sleep .05
  done
  return 1
}
cat > /etc/pacman.conf <<'EOF'
[options]
Architecture = auto
SigLevel = Required
LocalFileSigLevel = Required
CacheDir = /fixture/client-cache
LogFile = /evidence/pacman.log
[fixture]
Server = https://127.0.0.1:3142/pacman/$arch
EOF
cp /etc/pacman.conf /evidence/pacman.conf
start_proxy
pacman --noconfirm -Syy
pacman --noconfirm -S apt-proxy-fixture
cmp /usr/share/apt-proxy-fixture/payload /fixture/build/payload
curl -fsS http://127.0.0.1:8080/__control/status > /evidence/first-install.json
pacman --noconfirm -R apt-proxy-fixture
rm -rf /fixture/client-cache /var/lib/pacman/sync/*
mkdir -p /fixture/client-cache
pacman --noconfirm -Syy
pacman --noconfirm -S apt-proxy-fixture
cmp /usr/share/apt-proxy-fixture/payload /fixture/build/payload
curl -fsS http://127.0.0.1:8080/__control/status > /evidence/second-install.json
python3 - <<'PY'
import json
def package_gets(path):
    return sum(r['method']=='GET' and r['path'].endswith('.pkg.tar.zst') for r in json.load(open(path))['requests'])
before=package_gets('/evidence/first-install.json'); after=package_gets('/evidence/second-install.json')
assert before>=1 and after==before, (before,after)
json.dump({'first_origin_package_gets':before,'second_origin_package_gets':after,
           'client_cache_cleared':True,'payload_matches':True},open('/evidence/cache-hit.json','w'),indent=2)
PY
pacman --noconfirm -R apt-proxy-fixture
tamper_signature() {
  python3 - "$1" <<'PY'
from pathlib import Path
import sys
p=Path(sys.argv[1]); signature=bytearray(p.read_bytes()); signature[-1]^=1; p.write_bytes(signature)
PY
}
tamper_signature "$repo/fixture.db.sig"
rm -rf /fixture/client-cache /var/lib/pacman/sync/*; mkdir -p /fixture/client-cache
start_proxy
if pacman --noconfirm -Syy > /evidence/rejected-database.log 2>&1; then
  echo 'Pacman accepted an invalid database signature' >&2; exit 1
fi
grep -Ei 'signature|corrupted' /evidence/rejected-database.log
cp /fixture/valid/fixture.db "$repo/fixture.db.tar.gz"
cp /fixture/valid/fixture.db.sig "$repo/fixture.db.tar.gz.sig"
tamper_signature "$package.sig"
make_database
rm -rf /fixture/client-cache /var/lib/pacman/sync/*; mkdir -p /fixture/client-cache
start_proxy
pacman --noconfirm -Syy
if pacman --noconfirm -S apt-proxy-fixture > /evidence/rejected-package.log 2>&1; then
  echo 'Pacman accepted an invalid package signature' >&2; exit 1
fi
grep -Ei 'signature|corrupted' /evidence/rejected-package.log
python3 - <<'PY'
import json
json.dump({'signed_database':True,'signed_package':True,'tampered_database_rejected':True,
           'tampered_package_rejected':True,'tls_verification':True},open('/evidence/signatures.json','w'),indent=2)
PY
printf 'PASS: native pacman signed database/package install, proxy package cache hit after client-cache removal, database/package signature rejection, verified TLS\n' | tee /evidence/result.txt
