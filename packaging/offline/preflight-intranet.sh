#!/usr/bin/env bash
set -euo pipefail

ROOT=${1:-/opt/picoaide}
fail=0
check_cmd() { command -v "$1" >/dev/null 2>&1 || { echo "MISSING command: $1"; fail=1; }; }
check_cmd docker
check_cmd curl
check_cmd sha256sum
check_cmd jq
if command -v docker >/dev/null 2>&1; then
  docker compose version >/dev/null 2>&1 || { echo 'MISSING: docker compose plugin'; fail=1; }
  docker info >/dev/null 2>&1 || { echo 'UNAVAILABLE: docker daemon'; fail=1; }
fi
if [[ -d "$ROOT" ]]; then
  [[ -f "$ROOT/.env" ]] || { echo "MISSING: $ROOT/.env"; fail=1; }
  [[ -f "$ROOT/docker-compose.yml" ]] || { echo "MISSING: $ROOT/docker-compose.yml"; fail=1; }
  [[ -f "$ROOT/Caddyfile.internal" ]] || { echo "MISSING: $ROOT/Caddyfile.internal"; fail=1; }
  if [[ -f "$ROOT/.env" ]]; then
    grep -q '^PICOAI_ADMIN_PASSWORD=..' "$ROOT/.env" || { echo 'INVALID: PICOAI_ADMIN_PASSWORD is empty'; fail=1; }
    grep -q '^PG_PASSWORD=..' "$ROOT/.env" || { echo 'INVALID: PG_PASSWORD is empty'; fail=1; }
    if grep -q '^TLS_MODE=http' "$ROOT/.env"; then
      grep -q '^PICOAI_PUBLIC_BASE_URL=http://' "$ROOT/.env" || echo 'WARN: HTTP mode should set PICOAI_PUBLIC_BASE_URL=http://...'
      [[ -f "$ROOT/Caddyfile.http" ]] || { echo "MISSING: $ROOT/Caddyfile.http"; fail=1; }
      [[ -f "$ROOT/docker-compose.http.yml" ]] || { echo "MISSING: $ROOT/docker-compose.http.yml"; fail=1; }
    else
      grep -Eq '^PICOAI_PUBLIC_BASE_URL=https://' "$ROOT/.env" || echo 'WARN: PICOAI_PUBLIC_BASE_URL is not absolute HTTPS; client update manifest may be unavailable'
    fi
  fi
fi
if [[ $fail -ne 0 ]]; then exit 1; fi
echo "preflight passed: $ROOT"
