#!/usr/bin/env bash
# Generates realistic activity in a dev mmcp (for screenshots/demos).
# Requires seed-mattermost.sh + seed-keycloak.sh and a configured mmcp.
set -euo pipefail
B=${B:-http://127.0.0.1:18088}
KC=${KC:-http://127.0.0.1:18190/realms/company}
MM=${MM:-http://127.0.0.1:18065}
PW='Passw0rd!demo'
J=$(mktemp); trap 'rm -f $J' EXIT
H=(-H 'Content-Type: application/json' -H 'X-Requested-With: mmcp')
adm() { curl -s -b $J -c $J "${H[@]}" -X "$1" "$B$2" ${3:+-d "$3"}; }
kct() { curl -s -d grant_type=password -d client_id=mmcp -d client_secret=mmcp-dev-client-secret -d username=$1 -d password="$PW" -d scope=openid $KC/protocol/openid-connect/token | python3 -c 'import json,sys;print(json.load(sys.stdin)["access_token"])'; }
mcp() { curl -s $B/mcp -H "Authorization: Bearer $1" -H 'Content-Type: application/json' -H "X-Request-Id: demo-$RANDOM" -d "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"$2\",\"params\":${3:-{\}}}"; }
tool() { mcp "$1" tools/call "{\"name\":\"$2\",\"arguments\":${3:-{\}}}"; }
ct() { python3 -c 'import json,sys;d=json.load(sys.stdin);print(d["result"]["structuredContent"]["error"]["details"]["confirm_token"])'; }

adm POST /api/auth/login '{"username":"admin","password":"Admin!2345678"}' >/dev/null
for u in hkjang kim park test01; do
  T=$(kct $u)
  mcp "$T" initialize '{"protocolVersion":"2025-11-25","clientInfo":{"name":"qwenpaw-portal","version":"3.2.0"},"capabilities":{}}' >/dev/null
  tool "$T" mattermost_me >/dev/null
  tool "$T" mattermost_unread >/dev/null
  tool "$T" mattermost_search_posts '{"query":"배포","limit":10}' >/dev/null
  tool "$T" mattermost_channels >/dev/null
done
HK=$(kct hkjang)
tool "$HK" mattermost_get_thread '{"post_id":"'$(tool "$HK" mattermost_search_posts '{"query":"결정"}' | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d["result"]["structuredContent"]["data"]["results"][0]["post_id"])')'"}' >/dev/null
tool "$HK" mattermost_read_dm '{"user":"kim"}' >/dev/null
tool "$HK" mattermost_recent_dms >/dev/null
tool "$HK" mattermost_send_dm '{"recipient":"김철수","message":"확인 부탁드립니다"}' >/dev/null      # ambiguous
tool "$HK" mattermost_recent_posts '{"channel":"없는채널"}' >/dev/null                                  # not found
C=$(tool "$HK" mattermost_post_message '{"channel":"ai-project","message":"[AI 요약] 이번 주 MCP 게이트웨이 PoC가 완료되었습니다."}' | ct)
tool "$HK" mattermost_post_message "{\"channel\":\"ai-project\",\"message\":\"[AI 요약] 이번 주 MCP 게이트웨이 PoC가 완료되었습니다.\",\"confirm_token\":\"$C\"}" >/dev/null
C=$(tool "$HK" mattermost_send_dm '{"recipient":"lee","message":"18시 배포 고생하셨습니다. 내일 회고 10시에 진행해요."}' | ct)
tool "$HK" mattermost_send_dm "{\"recipient\":\"lee\",\"message\":\"18시 배포 고생하셨습니다. 내일 회고 10시에 진행해요.\",\"confirm_token\":\"$C\"}" >/dev/null
tool "$HK" mattermost_send_group_dm '{"recipients":["kim","park","choi"],"message":"MCP 데모 회의 화요일 10시로 확정입니다."}' >/dev/null   # stays pending
R=$(tool "$HK" mattermost_send_dm '{"recipient":"park","message":"보안 검토 결과 공유 부탁드립니다."}' | ct)
tool "$HK" mattermost_reply '{"post_id":"'$(tool "$HK" mattermost_search_posts '{"query":"커넥션"}' | python3 -c 'import json,sys;d=json.load(sys.stdin);print(d["result"]["structuredContent"]["data"]["results"][0]["post_id"])')'","message":"모니터링 결과 p95 1.2초로 안정화되었습니다."}' >/dev/null
KT=$(kct kim); tool "$KT" mattermost_send_dm '{"recipient":"hkjang","message":"보고서 v2 올렸습니다"}' >/dev/null   # kim has no writer role -> denied
# hkjang becomes a console admin with a local password for screenshots
HKID=$(adm GET '/api/admin/users?q=hkjang' | python3 -c 'import json,sys;print(json.load(sys.stdin)["items"][0]["user"]["id"])')
adm PUT /api/admin/users/$HKID '{"roles":["admin","mcp-channel-admin"]}' >/dev/null
adm POST /api/admin/users/$HKID/reset-password '{"password":"Demo!Passw0rd"}' >/dev/null
# local users: one without a Mattermost account (not_found), one that will conflict
adm POST /api/admin/users '{"username":"ghost","display_name":"외부 협력사","password":"Ghost!Passw0rd","roles":["mcp-user"]}' >/dev/null || true
adm POST /api/admin/users '{"username":"choi","display_name":"최유진","email":"choi@example.com","password":"Choi!Passw0rd","roles":["mcp-user","mcp-writer"]}' >/dev/null || true
adm POST /api/admin/users '{"username":"honggd","display_name":"홍길동","password":"Hong!Passw0rd1","roles":["mcp-user"]}' >/dev/null || true
HG=$(adm GET '/api/admin/users?q=honggd' | python3 -c 'import json,sys;print(json.load(sys.stdin)["items"][0]["user"]["id"])')
adm POST /api/admin/mappings "{\"user_id\":\"$HG\",\"mm_username\":\"kimc2\"}" >/dev/null
adm POST /api/admin/mappings/auto >/dev/null
# API keys for hkjang (login as hkjang)
J2=$(mktemp)
curl -s -c $J2 "${H[@]}" -X POST $B/api/auth/login -d '{"username":"hkjang","password":"Demo!Passw0rd"}' >/dev/null
for k in '{"name":"노트북 Claude Code","expires_days":90}' '{"name":"Codex CLI","expires_days":30}' '{"name":"대시보드 (읽기 전용)","read_only":true,"expires_days":180}' '{"name":"배포 스크립트","expires_days":365}'; do
  curl -s -b $J2 "${H[@]}" -X POST $B/api/me/api-keys -d "$k" >/dev/null
done
KID=$(curl -s -b $J2 $B/api/me/api-keys | python3 -c 'import json,sys;print([k for k in json.load(sys.stdin)["items"] if k["name"]=="Codex CLI"][0]["id"])')
curl -s -b $J2 "${H[@]}" -X POST $B/api/me/api-keys/$KID/rotate -d '{"grace_minutes":1440}' >/dev/null
SECRET=$(curl -s -b $J2 "${H[@]}" -X POST $B/api/me/api-keys -d '{"name":"CI 테스트","expires_days":30}' | python3 -c 'import json,sys;print(json.load(sys.stdin)["secret"])')
mcp "$SECRET" initialize '{"protocolVersion":"2025-06-18","clientInfo":{"name":"codex-mcp-client","version":"0.48.0"},"capabilities":{}}' >/dev/null
tool "$SECRET" mattermost_pinned_posts '{"channel":"text2sql"}' >/dev/null
curl -s -b $J2 "${H[@]}" -X POST $B/api/me/encryption-key/rotate >/dev/null
rm -f $J2
# OAuth clients
for c in 'Claude Code' 'Cursor' 'Codex' 'VS Code Copilot'; do
  curl -s -X POST $B/oauth/register -H 'Content-Type: application/json' -d "{\"client_name\":\"$c\",\"redirect_uris\":[\"http://127.0.0.1:33418/callback\"]}" >/dev/null
done
echo "demo data seeded"
