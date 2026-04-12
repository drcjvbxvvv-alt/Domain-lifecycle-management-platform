#!/usr/bin/env bash
# dev-stop.sh — 停止 server + worker，並可選停止 Docker Compose infra
# 用法: ./scripts/dev-stop.sh [--infra]
#   --infra  同時停止 docker compose (postgres / redis / minio)

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
STOP_INFRA=false
for arg in "$@"; do
  [[ $arg == "--infra" ]] && STOP_INFRA=true
done

GREEN='\033[0;32m'; NC='\033[0m'
info() { echo -e "${GREEN}[STOP]${NC} $*"; }

for proc in server worker; do
  pids=$(pgrep -f "bin/$proc" 2>/dev/null || true)
  if [ -n "$pids" ]; then
    kill $pids 2>/dev/null && info "Stopped $proc (pid $pids)"
  else
    info "$proc is not running"
  fi
done

if [ "$STOP_INFRA" = true ]; then
  info "Stopping Docker Compose..."
  docker compose -f "$REPO_ROOT/deploy/docker-compose.yml" down
  info "Infra stopped."
fi

info "Done."
