#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
exec "$ROOT/final-http-release/offline-build/setup-http.sh" "$@"
