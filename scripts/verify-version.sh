#!/bin/sh
# Release metadata must agree everywhere the version is written.
set -eu
root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
fail() { echo "version check failed: $*" >&2; exit 1; }
version=$(sed -n '1p' "$root/VERSION")
printf '%s\n' "$version" | grep -Eq '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z.-]+)?$' || fail "VERSION must be SemVer without a leading v"
pkg=$(sed -n 's/^  "version": "\(.*\)",$/\1/p' "$root/web/package.json" | head -1)
[ "$pkg" = "$version" ] || fail "web/package.json version $pkg != $version"
lock=$(sed -n 's/^  "version": "\(.*\)",$/\1/p' "$root/web/package-lock.json" | head -1)
[ "$lock" = "$version" ] || fail "web/package-lock.json version $lock != $version"
grep -q "image: mmcp:v${version}$" "$root/compose.offline.yml" || fail "compose.offline.yml image tag"
grep -q "^## v${version}" "$root/CHANGELOG.md" || fail "CHANGELOG.md has no v${version} section"
if [ -f "$root/docs/index.html" ]; then
  grep -q "\"softwareVersion\": \"${version}\"" "$root/docs/index.html" || fail "docs/index.html softwareVersion"
  grep -q "mmcp-v${version}.tar.gz" "$root/docs/index.html" || fail "docs/index.html archive name"
fi
echo "Version metadata is consistent: $version"
