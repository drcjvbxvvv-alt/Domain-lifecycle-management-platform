#!/usr/bin/env bash
# dev-start.sh — 本地開發環境一鍵啟動
# 用法: ./scripts/dev-start.sh [--skip-infra] [--skip-build]
#
# 啟動順序:
#   1. Docker Compose (PostgreSQL + Redis + MinIO)
#   2. DB migrations
#   3. Build Go binaries (server + worker)
#   4. 前景執行 server + worker (background)
#
# 停止: Ctrl-C 或 ./scripts/dev-stop.sh

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BIN="$REPO_ROOT/bin"
CONFIG="$REPO_ROOT/configs/config.yaml"
LOG_DIR="$REPO_ROOT/.logs"

SKIP_INFRA=false
SKIP_BUILD=false
for arg in "$@"; do
  case $arg in
    --skip-infra) SKIP_INFRA=true ;;
    --skip-build) SKIP_BUILD=true ;;
  esac
done

# ── 顏色輸出 ──────────────────────────────────────────────────────────────────
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; NC='\033[0m'
info()  { echo -e "${GREEN}[START]${NC} $*"; }
warn()  { echo -e "${YELLOW}[WARN]${NC}  $*"; }
error() { echo -e "${RED}[ERROR]${NC} $*"; exit 1; }

# ── 清理函式（Ctrl-C 時呼叫）─────────────────────────────────────────────────
PIDS=()
cleanup() {
  echo ""
  info "Shutting down server and worker..."
  for pid in "${PIDS[@]}"; do
    kill "$pid" 2>/dev/null || true
  done
  wait 2>/dev/null || true
  info "Done."
}
trap cleanup INT TERM

mkdir -p "$LOG_DIR"

# ── Step 1: Docker Compose infra ──────────────────────────────────────────────
if [ "$SKIP_INFRA" = false ]; then
  info "Starting Docker Compose (postgres + redis + minio)..."
  docker compose -f "$REPO_ROOT/deploy/docker-compose.yml" up -d

  info "Waiting for postgres to be healthy..."
  for i in $(seq 1 30); do
    if docker exec domain_postgres pg_isready -U postgres -d domain_platform -q 2>/dev/null; then
      break
    fi
    if [ "$i" -eq 30 ]; then error "Postgres did not become healthy after 30s"; fi
    sleep 1
  done
  info "Postgres ready."

  info "Waiting for redis to be healthy..."
  for i in $(seq 1 15); do
    if docker exec domain_redis redis-cli ping 2>/dev/null | grep -q PONG; then
      break
    fi
    if [ "$i" -eq 15 ]; then error "Redis did not respond after 15s"; fi
    sleep 1
  done
  info "Redis ready."

  info "Waiting for minio to be healthy..."
  for i in $(seq 1 20); do
    if curl -sf http://localhost:9000/minio/health/live >/dev/null 2>&1; then
      break
    fi
    if [ "$i" -eq 20 ]; then error "MinIO did not become healthy after 20s"; fi
    sleep 1
  done
  info "MinIO ready."
else
  warn "--skip-infra: assuming Docker Compose is already running."
fi

# ── Step 2: Build ─────────────────────────────────────────────────────────────
if [ "$SKIP_BUILD" = false ]; then
  info "Building server + worker + migrate..."
  make -C "$REPO_ROOT" server worker migrate
  info "Build complete."
else
  warn "--skip-build: using existing binaries in $BIN/"
fi

# ── Step 3: DB migrations ─────────────────────────────────────────────────────
info "Running DB migrations..."
# migrate must run from repo root (bootstrap.Load reads ./configs/config.yaml
# and the binary looks for migrations at file://migrations)
cd "$REPO_ROOT" && "$BIN/migrate" up
info "Migrations done."

# ── Step 4: Start server ──────────────────────────────────────────────────────
info "Starting server  → http://localhost:8080  (log: .logs/server.log)"
cd "$REPO_ROOT" && "$BIN/server" > "$LOG_DIR/server.log" 2>&1 &
PIDS+=($!)

# ── Step 5: Start worker ──────────────────────────────────────────────────────
info "Starting worker  → asynq queues         (log: .logs/worker.log)"
cd "$REPO_ROOT" && "$BIN/worker" > "$LOG_DIR/worker.log" 2>&1 &
PIDS+=($!)

# ── Done ──────────────────────────────────────────────────────────────────────
echo ""
echo -e "${GREEN}============================================${NC}"
echo -e "${GREEN}  Platform is up${NC}"
echo -e "${GREEN}============================================${NC}"
echo "  API server  : http://localhost:8080/api/v1"
echo "  Agent port  : https://localhost:8443/agent/v1"
echo "  MinIO UI    : http://localhost:9001  (minioadmin / minioadmin)"
echo "  PG          : localhost:5432  db=domain_platform user=postgres"
echo "  Redis       : localhost:6379"
echo ""
echo "  Logs        : tail -f .logs/server.log  |  tail -f .logs/worker.log"
echo "  Stop        : Ctrl-C  or  ./scripts/dev-stop.sh"
echo ""

# 等待背景程序（Ctrl-C 觸發 cleanup）
wait "${PIDS[@]}"
