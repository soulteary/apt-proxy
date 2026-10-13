#!/usr/bin/env bash
set -euo pipefail
export DEBIAN_FRONTEND=noninteractive
mkdir -p /fixture/repo/ubuntu/pool/main/a/apt-proxy-fixture /evidence /fixture/gnupg
chmod 700 /fixture/gnupg
export GNUPGHOME=/fixture/gnupg
proxy_pid= origin_pid= offline_pid=
cleanup() {
  if [ -n "$offline_pid" ]; then kill "$offline_pid" 2>/dev/null || true; wait "$offline_pid" 2>/dev/null || true; fi
  if [ -n "$proxy_pid" ]; then kill "$proxy_pid" 2>/dev/null || true; wait "$proxy_pid" 2>/dev/null || true; fi
  if [ -n "$origin_pid" ]; then kill "$origin_pid" 2>/dev/null || true; wait "$origin_pid" 2>/dev/null || true; fi
}
trap cleanup EXIT
exec > >(tee /evidence/apt-client.log) 2>&1
cat /etc/os-release
apt-get --version
arch=$(dpkg --print-architecture)
pool=/fixture/repo/ubuntu/pool/main/a/apt-proxy-fixture
mkdir -p /fixture/binary/DEBIAN /fixture/binary/usr/share/apt-proxy-fixture
cat > /fixture/binary/DEBIAN/control <<'EOF'
Package: apt-proxy-fixture
Version: 1.0
Architecture: all
Maintainer: Fixture <fixture@example.invalid>
Description: APT proxy compatibility fixture
EOF
python3 - <<'PY'
from pathlib import Path
import random
Path('/fixture/binary/usr/share/apt-proxy-fixture/payload').write_bytes(random.Random(124).randbytes(2*1024*1024))
PY
dpkg-deb --build /fixture/binary "$pool/apt-proxy-fixture_1.0_all.deb"
mkdir -p /fixture/source/apt-proxy-fixture-1.0/debian/source
cat > /fixture/source/apt-proxy-fixture-1.0/debian/control <<'EOF'
Source: apt-proxy-fixture
Section: misc
Priority: optional
Maintainer: Fixture <fixture@example.invalid>
Standards-Version: 4.6.2

Package: apt-proxy-fixture
Architecture: all
Description: APT proxy compatibility fixture
EOF
cat > /fixture/source/apt-proxy-fixture-1.0/debian/changelog <<'EOF'
apt-proxy-fixture (1.0) unstable; urgency=low

  * Local compatibility fixture.

 -- Fixture <fixture@example.invalid>  Mon, 12 Oct 2026 00:00:00 +0000
EOF
printf '3.0 (native)\n' > /fixture/source/apt-proxy-fixture-1.0/debian/source/format
printf '#!/usr/bin/make -f\n%%:\n\t@true\n' > /fixture/source/apt-proxy-fixture-1.0/debian/rules
chmod +x /fixture/source/apt-proxy-fixture-1.0/debian/rules
cp /fixture/binary/usr/share/apt-proxy-fixture/payload /fixture/source/apt-proxy-fixture-1.0/payload
(cd /fixture/source && dpkg-source -b apt-proxy-fixture-1.0)
cp /fixture/source/*.dsc /fixture/source/*.tar.xz "$pool/"
dist=/fixture/repo/ubuntu/dists/test
mkdir -p "$dist/main/binary-$arch" "$dist/main/source"
(cd /fixture/repo/ubuntu && dpkg-scanpackages pool /dev/null) > "$dist/main/binary-$arch/Packages"
(cd /fixture/repo/ubuntu && dpkg-scansources pool /dev/null) > "$dist/main/source/Sources"
for index in "$dist/main/binary-$arch/Packages" "$dist/main/source/Sources"; do gzip -k -n "$index"; xz -k "$index"; done
python3 - "$arch" <<'PY'
import datetime, hashlib, pathlib, sys
root=pathlib.Path('/fixture/repo/ubuntu/dists/test')
now=datetime.datetime.now(datetime.timezone.utc)
header=f'Origin: APT proxy fixture\nLabel: APT proxy fixture\nSuite: test\nCodename: test\nDate: {now:%a, %d %b %Y %H:%M:%S %Z}\nValid-Until: {now+datetime.timedelta(days=1):%a, %d %b %Y %H:%M:%S %Z}\nArchitectures: {sys.argv[1]} all\nComponents: main\nAcquire-By-Hash: yes\nSHA256:\n'
for index in sorted(root.glob('main/*/*')):
    if not index.is_file(): continue
    data=index.read_bytes(); digest=hashlib.sha256(data).hexdigest()
    header+=f' {digest} {len(data)} {index.relative_to(root)}\n'
    alias=index.parent/'by-hash'/'SHA256'/digest
    alias.parent.mkdir(parents=True,exist_ok=True); alias.write_bytes(data)
(root/'Release').write_text(header)
PY
gpg --batch --pinentry-mode loopback --passphrase '' --quick-gen-key 'APT Fixture <fixture@example.invalid>' rsa2048 sign 1d
gpg --batch --export > /fixture/repository.gpg
gpg --batch --yes --pinentry-mode loopback --passphrase '' --clearsign -o "$dist/InRelease" "$dist/Release"
gpg --batch --yes --pinentry-mode loopback --passphrase '' --detach-sign -o "$dist/Release.gpg" "$dist/Release"
openssl req -x509 -newkey rsa:2048 -nodes -keyout /fixture/key.pem -out /fixture/cert.pem -days 2 -subj '/CN=APT Fixture' -addext 'subjectAltName=IP:127.0.0.1' > /dev/null 2>&1
cp /fixture/cert.pem /usr/local/share/ca-certificates/apt-fixture.crt
update-ca-certificates
chmod 644 /fixture/repository.gpg
python3 /tests/fixture.py > /evidence/origin.log 2>&1 & origin_pid=$!
for _ in {1..100}; do if curl -fsS http://127.0.0.1:8080/__control/status > /dev/null; then break; fi; sleep .05; done
start_proxy() {
  local mirror=$1 tls=${2:-false}
  if [ -n "$proxy_pid" ]; then kill "$proxy_pid"; wait "$proxy_pid"; fi
  # Isolate each phase's cache; prevents an earlier warm hit masking faults.
  local cache_dir; cache_dir=$(mktemp -d /fixture/cache.XXXXXX)
  cat > /fixture/apt-proxy.yaml <<EOF
server:
  host: 127.0.0.1
  port: 3142
mode: ubuntu
mirrors:
  ubuntu: "$mirror"
cache:
  dir: "$cache_dir"
tls:
  enabled: $tls
  cert_file: /fixture/cert.pem
  key_file: /fixture/key.pem
download:
  timeout_seconds: 30
EOF
  /usr/local/bin/apt-proxy --config=/fixture/apt-proxy.yaml >> /evidence/proxy.log 2>&1 & proxy_pid=$!
  for _ in {1..100}; do
    if curl --cacert /fixture/cert.pem -fsS "${client_scheme:-http}://127.0.0.1:3142/healthz" > /dev/null; then return; fi
    if ! kill -0 "$proxy_pid" 2>/dev/null; then cat /evidence/proxy.log; return 1; fi
    sleep .05
  done
  return 1
}
# Use only the fixture's signed archive. No external archive can hide a miss.
rm -f /etc/apt/sources.list /etc/apt/sources.list.d/*
cat > /etc/apt/sources.list <<'EOF'
deb [signed-by=/fixture/repository.gpg] http://127.0.0.1:3142/ubuntu test main
deb-src [signed-by=/fixture/repository.gpg] http://127.0.0.1:3142/ubuntu test main
EOF
# Reject altered signed metadata with the actual client's verification enabled.
cp "$dist/InRelease" /fixture/InRelease.valid
sed -i 's/APT proxy fixture/Altered fixture/g' "$dist/InRelease"
start_proxy http://127.0.0.1:8080/ubuntu/
rm -rf /var/lib/apt/lists/*
if apt-get -o APT::Update::Error-Mode=any update; then echo 'Tampered signature unexpectedly accepted' >&2; exit 1; fi
cp /fixture/InRelease.valid "$dist/InRelease"
start_proxy http://127.0.0.1:8080/ubuntu/
apt-get -o Acquire::Retries=2 -o Acquire::By-Hash=force update
curl -fsS http://127.0.0.1:8080/__control/status > /evidence/by-hash.json
python3 - <<'PY'
import json
assert any('/by-hash/SHA256/' in p for p in json.load(open('/evidence/by-hash.json'))['paths'])
PY
rm -rf /var/lib/apt/lists/*
curl -fsS http://127.0.0.1:8080/__control/status > /evidence/gzip-before.json
apt-get -o Acquire::By-Hash=false -o Acquire::CompressionTypes::Order::=gz update
curl -fsS http://127.0.0.1:8080/__control/status > /evidence/gzip.json
python3 - <<'PY'
import json
before = json.load(open('/evidence/gzip-before.json'))['paths']
after = json.load(open('/evidence/gzip.json'))['paths']
assert after[:len(before)] == before, 'Fixture request history changed'
indexes = [p for p in after[len(before):]
           if '/main/binary-' in p and '/Packages' in p
           or '/main/source/Sources' in p]
assert any(p.endswith('/Packages.gz') for p in indexes), indexes
assert any(p.endswith('/Sources.gz') for p in indexes), indexes
assert all(p.endswith('.gz') for p in indexes), indexes
PY
apt-get install -y --no-install-recommends apt-proxy-fixture
cmp /usr/share/apt-proxy-fixture/payload /fixture/binary/usr/share/apt-proxy-fixture/payload
mkdir -p /fixture/source-download
(cd /fixture/source-download && apt-get source --download-only apt-proxy-fixture)
cmp /fixture/source-download/apt-proxy-fixture_1.0.dsc "$pool/apt-proxy-fixture_1.0.dsc"
cmp /fixture/source-download/apt-proxy-fixture_1.0.tar.xz "$pool/apt-proxy-fixture_1.0.tar.xz"
object=ubuntu/pool/main/a/apt-proxy-fixture/apt-proxy-fixture_1.0_all.deb
curl -fsS http://127.0.0.1:8080/__control/status > /evidence/cache-before.json
apt-get clean
test ! -f /var/cache/apt/archives/apt-proxy-fixture_1.0_all.deb
apt-get install -y --reinstall --no-install-recommends apt-proxy-fixture
cmp /usr/share/apt-proxy-fixture/payload /fixture/binary/usr/share/apt-proxy-fixture/payload
curl -fsS http://127.0.0.1:8080/__control/status > /evidence/cache-after.json
python3 - <<'PY'
import json
from pathlib import Path
package = '/ubuntu/pool/main/a/apt-proxy-fixture/apt-proxy-fixture_1.0_all.deb'
def count(name):
    return sum(r['method'] == 'GET' and r['path'] == package
               for r in json.loads(Path('/evidence/' + name).read_text())['requests'])
first, second = count('cache-before.json'), count('cache-after.json')
assert first >= 1 and second == first, (first, second)
Path('/evidence/cache-hit.json').write_text(json.dumps({
    'first_origin_package_gets': first, 'second_origin_package_gets': second,
    'client_cache_cleared': True, 'payload_matches': True,
}, indent=2) + '\n')
PY
curl -fsS -H 'Range: bytes=100-299' -D /evidence/range.headers "http://127.0.0.1:3142/$object" -o /fixture/range.bin
grep -q '206' /evidence/range.headers
python3 - <<'PY'
from pathlib import Path
assert Path('/fixture/range.bin').read_bytes()==Path('/fixture/repo/ubuntu/pool/main/a/apt-proxy-fixture/apt-proxy-fixture_1.0_all.deb').read_bytes()[100:300]
PY
for fault in fail interrupt; do
  start_proxy http://127.0.0.1:8080/ubuntu/
  curl -fsS -X POST "http://127.0.0.1:8080/__control/$fault"
  mkdir -p "/fixture/$fault"
  if ! (cd "/fixture/$fault" && apt-get -o Acquire::Retries=3 download apt-proxy-fixture); then
    # APT may classify a truncated file as a checksum error rather than a
    # transport retry. A second actual APT invocation must recover safely.
    (cd "/fixture/$fault" && apt-get -o Acquire::Retries=3 download apt-proxy-fixture)
  fi
  cmp "/fixture/$fault/apt-proxy-fixture_1.0_all.deb" "$pool/apt-proxy-fixture_1.0_all.deb"
done
client_scheme=https
start_proxy https://127.0.0.1:8443/private/ubuntu/ true
sed -i 's|http://127.0.0.1:3142|https://127.0.0.1:3142|' /etc/apt/sources.list
rm -rf /var/lib/apt/lists/*
if apt-get -o APT::Update::Error-Mode=any update; then echo 'Unauthenticated private repository unexpectedly accepted' >&2; exit 1; fi
mkdir -p /etc/apt/auth.conf.d
printf 'machine https://127.0.0.1:3142 login fixture password fixture-password\n' > /etc/apt/auth.conf.d/fixture.conf
chmod 600 /etc/apt/auth.conf.d/fixture.conf
apt-get -o APT::Update::Error-Mode=any update
mkdir -p /fixture/private-download
(cd /fixture/private-download && apt-get download apt-proxy-fixture)
cmp /fixture/private-download/apt-proxy-fixture_1.0_all.deb "$pool/apt-proxy-fixture_1.0_all.deb"
curl -fsS http://127.0.0.1:8080/__control/status > /evidence/final-status.json
python3 - <<'PY'
import json
assert json.load(open('/evidence/final-status.json'))['private'] > 0
PY
if grep -q 'fixture-password' /evidence/proxy.log; then echo 'Credential leaked in proxy log' >&2; exit 1; fi
# Prefetch a complete binary/source selection, round-trip it through an archive,
# then stop the origin and install through the offline server.
/usr/local/bin/apt-proxy repository prefetch --url http://127.0.0.1:8080/ubuntu/ --suite test --components main --architectures "$arch" --sources --dir /fixture/offline
/usr/local/bin/apt-proxy repository verify --dir /fixture/offline
/usr/local/bin/apt-proxy repository export --dir /fixture/offline --file /fixture/offline.tar
/usr/local/bin/apt-proxy repository import --dir /fixture/imported --file /fixture/offline.tar
kill "$origin_pid"; wait "$origin_pid" || true; origin_pid=
kill "$proxy_pid"; wait "$proxy_pid" || true; proxy_pid=
/usr/local/bin/apt-proxy repository serve --dir /fixture/imported --listen 127.0.0.1:3144 >> /evidence/offline.log 2>&1 & offline_pid=$!
for _ in {1..100}; do if curl -fsS http://127.0.0.1:3144/dists/test/InRelease > /dev/null; then break; fi; sleep .05; done
cat > /etc/apt/sources.list <<'EOF'
deb [signed-by=/fixture/repository.gpg] http://127.0.0.1:3144 test main
deb-src [signed-by=/fixture/repository.gpg] http://127.0.0.1:3144 test main
EOF
rm -rf /var/lib/apt/lists/* /var/cache/apt/archives/*.deb
apt-get -o Acquire::By-Hash=force -o APT::Update::Error-Mode=any update
apt-get install -y --reinstall --no-install-recommends apt-proxy-fixture
cmp /usr/share/apt-proxy-fixture/payload /fixture/binary/usr/share/apt-proxy-fixture/payload
mkdir -p /fixture/offline-source
(cd /fixture/offline-source && apt-get source --download-only apt-proxy-fixture)
cmp /fixture/offline-source/apt-proxy-fixture_1.0.dsc "$pool/apt-proxy-fixture_1.0.dsc"
cmp /fixture/offline-source/apt-proxy-fixture_1.0.tar.xz "$pool/apt-proxy-fixture_1.0.tar.xz"
printf 'PASS: proxy cache hit after client cache cleanup, signature rejection, offline installation and source, signed update, by-hash and gzip indexes, install, source, range, retry/interruption, authenticated HTTPS\n' | tee /evidence/result.txt
