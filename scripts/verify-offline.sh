#!/bin/sh
# Verifies the offline archive: loads it into a scratch tag namespace and
# checks the image runs "version" without network access.
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
version=$(sed -n '1p' "$root/VERSION")
archive="$root/release/mmcp-v${version}.tar.gz"
[ -f "$archive" ] || { echo "missing $archive" >&2; exit 1; }
gzip -t "$archive"
( cd "$root/release" && sha256sum -c "mmcp-v${version}.tar.gz.sha256" )
loaded=$(docker load -i "$archive" | sed -n 's/^Loaded image: //p')
[ "$loaded" = "mmcp:v${version}" ] || { echo "unexpected image tag: $loaded" >&2; exit 1; }
out=$(docker run --rm --network none "mmcp:v${version}" version)
echo "$out"
echo "$out" | grep -q "mmcp v${version}" || { echo "version mismatch" >&2; exit 1; }
echo "Offline archive OK: mmcp:v${version}"
