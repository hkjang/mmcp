#!/usr/bin/env bash
# Imports scripts/dev/keycloak-realm.json into a dev Keycloak.
# Usage: KC=http://127.0.0.1:18190 scripts/dev/seed-keycloak.sh
set -euo pipefail
KC=${KC:-http://127.0.0.1:18190}
dir=$(cd "$(dirname "$0")" && pwd)
until curl -sf "$KC/realms/master" >/dev/null; do sleep 3; done
T=$(curl -sS -d grant_type=password -d client_id=admin-cli -d username=${KC_ADMIN:-admin} -d password=${KC_ADMIN_PASSWORD:-admin} "$KC/realms/master/protocol/openid-connect/token" | python3 -c 'import json,sys;print(json.load(sys.stdin)["access_token"])')
curl -sS -X DELETE -H "Authorization: Bearer $T" "$KC/admin/realms/company" >/dev/null || true
curl -sS -f -X POST -H "Authorization: Bearer $T" -H 'Content-Type: application/json' --data @"$dir/keycloak-realm.json" "$KC/admin/realms"
echo "realm company imported: issuer $KC/realms/company"
