#!/usr/bin/env bash
set -euo pipefail
# Build on the host, execute the actual distro's APT/dpkg in an isolated container.
repo_root=$(cd "$(dirname "$0")/../.." && pwd)
build_context=$(mktemp -d)
trap 'rm -rf "$build_context"' EXIT
arch=$(docker info --format '{{.Architecture}}')
case "$arch" in aarch64|arm64) go_arch=arm64 ;; x86_64|amd64) go_arch=amd64 ;; *) echo "Unsupported Docker architecture: $arch" >&2; exit 1 ;; esac
cd "$repo_root"
CGO_ENABLED=0 GOOS=linux GOARCH="$go_arch" go build -o "$build_context/apt-proxy" ./cmd/apt-proxy
cp tests/apt-client/{Dockerfile,fixture.py,run.sh} "$build_context/"
if [ "$#" -eq 0 ]; then set -- debian:bookworm-slim debian:trixie-slim ubuntu:22.04 ubuntu:24.04; fi
evidence_root=${APT_CLIENT_EVIDENCE_DIR:-"$repo_root/apt-client-evidence"}
mkdir -p "$evidence_root"
for client_image in "$@"; do
  image_id=$(printf '%s' "$client_image" | tr '/:' '--')
  docker build --platform "linux/$go_arch" --build-arg "APT_CLIENT_IMAGE=$client_image" -t "apt-proxy-client-test:$image_id" "$build_context"
  mkdir -p "$evidence_root/$image_id"
  docker run --platform "linux/$go_arch" --rm --network bridge --mount "type=bind,src=$evidence_root/$image_id,dst=/evidence" "apt-proxy-client-test:$image_id"
done
