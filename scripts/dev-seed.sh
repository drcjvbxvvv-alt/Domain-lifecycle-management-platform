#!/usr/bin/env bash
# dev-seed.sh — 快速建立 Phase 1 acceptance demo 所需的最小資料
# 依照 PHASE1_TASKLIST.md §"What Phase 1 done looks like" 的步驟順序執行
#
# 前提：server 必須已在 localhost:8080 執行
# 用法: ./scripts/dev-seed.sh

set -euo pipefail

BASE="http://localhost:8080/api/v1"
GREEN='\033[0;32m'; YELLOW='\033[1;33m'; NC='\033[0m'
ok()   { echo -e "${GREEN}[OK]${NC}   $*"; }
info() { echo -e "${YELLOW}[INFO]${NC} $*"; }

# ── 輔助函式 ──────────────────────────────────────────────────────────────────
post() {
  local path=$1; shift
  curl -sf -X POST "$BASE$path" \
    -H "Content-Type: application/json" \
    -H "Authorization: Bearer $TOKEN" \
    -d "$@"
}

get() {
  curl -sf "$BASE$1" -H "Authorization: Bearer $TOKEN"
}

# ── Step 1: Login ─────────────────────────────────────────────────────────────
info "Logging in as admin..."
RESP=$(curl -sf -X POST "$BASE/auth/login" \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"admin123"}')
TOKEN=$(echo "$RESP" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['token'])")
ok "Token acquired."

# ── Step 2: Create Project ────────────────────────────────────────────────────
info "Creating project 'demo'..."
PROJECT=$(post /projects '{"name":"demo","slug":"demo","description":"Phase 1 acceptance demo project"}')
PROJECT_ID=$(echo "$PROJECT" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['id'])")
ok "Project id=$PROJECT_ID"

# ── Step 3: Register Domain ───────────────────────────────────────────────────
info "Registering domain demo.example.com..."
DOMAIN=$(post /domains \
  "{\"project_id\":$PROJECT_ID,\"fqdn\":\"demo.example.com\",\"dns_provider\":\"manual\"}")
DOMAIN_UUID=$(echo "$DOMAIN" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['uuid'])")
ok "Domain uuid=$DOMAIN_UUID (state=requested)"

# Approve → provisioned → active
for transition in approved provisioned active; do
  post "/domains/$DOMAIN_UUID/transition" \
    "{\"to\":\"$transition\",\"reason\":\"seed script\"}" >/dev/null
  ok "  → $transition"
done

# ── Step 4: Create Template + Version ─────────────────────────────────────────
info "Creating template 'homepage'..."
TMPL=$(post "/projects/$PROJECT_ID/templates" \
  '{"name":"homepage","description":"Phase 1 demo template"}')
TMPL_ID=$(echo "$TMPL" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['id'])")
ok "Template id=$TMPL_ID"

info "Publishing template version v1..."
VER=$(post "/projects/$PROJECT_ID/templates/$TMPL_ID/versions" \
  '{"version_label":"v1","html_content":"<html><body>Hello Phase 1</body></html>","nginx_content":""}')
VER_ID=$(echo "$VER" | python3 -c "import sys,json; print(json.load(sys.stdin)['data']['id'])")
ok "TemplateVersion id=$VER_ID"

# ── Summary ───────────────────────────────────────────────────────────────────
echo ""
echo "Seed complete. Next steps:"
echo "  1. Register an agent (or use the acceptance demo manually)"
echo "  2. Create a release:"
echo "     POST $BASE/projects/$PROJECT_ID/releases"
echo "     {\"project_id\":$PROJECT_ID,\"template_version_id\":$VER_ID,"
echo "      \"release_type\":\"html\",\"domain_ids\":[<domain_db_id>]}"
