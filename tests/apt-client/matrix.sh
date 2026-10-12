#!/usr/bin/env bash
set -euo pipefail
# Build on the host, execute the actual distro's APT/dpkg in an isolated container.
repo_root=$(cd "$(dirname "$0")/../.." && pwd)
client_image_id() {
  local image_id digest
  image_id=$(printf '%s' "$1" | LC_ALL=C tr '[:upper:]' '[:lower:]' | LC_ALL=C tr -c 'a-z0-9_.-' '-')
  case "$image_id" in ""|.|..) echo "Invalid evidence directory name" >&2; return 1 ;; esac
  case "$image_id" in [a-z0-9_]*) ;; *) image_id="image-$image_id" ;; esac
  # Docker tags have at most 128 characters. Keep ordinary client IDs stable,
  # and retain a digest of the complete normalized ID when truncation is needed.
  if [ "${#image_id}" -gt 128 ]; then
    if command -v sha256sum >/dev/null 2>&1; then
      digest=$(printf '%s' "$image_id" | sha256sum) || return
    else
      digest=$(printf '%s' "$image_id" | shasum -a 256) || return
    fi
    digest=${digest%% *}
    image_id="${image_id:0:63}-$digest"
  fi
  printf '%s' "$image_id"
}
if [ "$#" -eq 0 ]; then set -- debian:bookworm-slim debian:trixie-slim ubuntu:22.04 ubuntu:24.04; fi
evidence_root=${APT_CLIENT_EVIDENCE_DIR:-"$repo_root/apt-client-evidence"}
mkdir -p "$evidence_root"
evidence_root=$(cd "$evidence_root" && pwd -P)
image_ids=()
for client_image in "$@"; do
  image_id=$(client_image_id "$client_image")
  # Validate the whole selection before clearing any previous evidence.
  for previous_id in "${image_ids[@]-}"; do
    if [ "$image_id" = "$previous_id" ]; then
      echo "Client images share an evidence identifier: $image_id" >&2
      exit 1
    fi
  done
  image_ids+=("$image_id")
done
for image_id in "${image_ids[@]}"; do
  # Clear evidence before host compilation or image builds can fail.
  rm -rf -- "$evidence_root/$image_id"
  mkdir -p "$evidence_root/$image_id"
done
build_context=$(mktemp -d)
trap 'rm -rf "$build_context"' EXIT
arch=$(docker info --format '{{.Architecture}}')
case "$arch" in aarch64|arm64) go_arch=arm64 ;; x86_64|amd64) go_arch=amd64 ;; *) echo "Unsupported Docker architecture: $arch" >&2; exit 1 ;; esac
cd "$repo_root"
CGO_ENABLED=0 GOOS=linux GOARCH="$go_arch" go build -o "$build_context/apt-proxy" ./cmd/apt-proxy
cp tests/apt-client/{Dockerfile,fixture.py,run.sh} "$build_context/"
for client_image in "$@"; do
  image_id=$(client_image_id "$client_image")
  docker build --platform "linux/$go_arch" --build-arg "APT_CLIENT_IMAGE=$client_image" -t "apt-proxy-client-test:$image_id" "$build_context"
  docker run --platform "linux/$go_arch" --rm --network bridge --mount "type=bind,src=$evidence_root/$image_id,dst=/evidence" "apt-proxy-client-test:$image_id"
done
