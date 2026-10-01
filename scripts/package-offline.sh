#!/bin/sh
# Builds mmcp:v<VERSION> and saves it as release/mmcp-v<VERSION>.tar.gz for
# air-gapped installation (docker load -i mmcp-v<VERSION>.tar.gz).
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
"$root/scripts/verify-version.sh"
version=$(sed -n '1p' "$root/VERSION")
image="mmcp:v${version}"
out="$root/release"
archive="$out/mmcp-v${version}.tar.gz"
mkdir -p "$out"
tmp=$(mktemp -d "$out/.pkg.XXXXXX")
trap 'rm -rf "$tmp"' EXIT INT TERM

docker build --platform linux/amd64 \
  --build-arg "VERSION=v${version}" \
  --build-arg "COMMIT=$(git -C "$root" rev-parse --short=12 HEAD 2>/dev/null || echo unknown)" \
  --build-arg "BUILD_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --tag "$image" "$root"

# no pipeline: a failed save must stop before compression (POSIX sh has no pipefail)
docker image save --output "$tmp/image.tar" "$image"
gzip -9 -c "$tmp/image.tar" > "$tmp/mmcp-v${version}.tar.gz"
gzip -t "$tmp/mmcp-v${version}.tar.gz"
mv -f "$tmp/mmcp-v${version}.tar.gz" "$archive"
( cd "$out" && sha256sum "mmcp-v${version}.tar.gz" > "mmcp-v${version}.tar.gz.sha256" )
printf '%s\n' "Created $archive ($(wc -c < "$archive" | tr -d ' ') bytes)" "Image: $image"
