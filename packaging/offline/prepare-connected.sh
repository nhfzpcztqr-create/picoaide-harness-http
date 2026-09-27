#!/usr/bin/env bash
set -euo pipefail

OUT=${1:?usage: prepare-connected.sh OUTPUT_DIR REPO_ROOT}
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
ROOT=${2:?usage: prepare-connected.sh OUTPUT_DIR REPO_ROOT}
ROOT=$(cd "$ROOT" && pwd)
[[ -f "$ROOT/package.json" && -f "$ROOT/deepseek-harness/pnpm-lock.yaml" && -f "$ROOT/server/go.mod" ]] || {
  echo "REPO_ROOT is not a complete PicoAide Harness source checkout: $ROOT" >&2
  exit 1
}
OUT=$(mkdir -p "$OUT" && cd "$OUT" && pwd)
mkdir -p "$OUT/cache" "$OUT/toolchain"

command -v go >/dev/null || { echo "go is required" >&2; exit 1; }
command -v node >/dev/null || { echo "node is required" >&2; exit 1; }
command -v corepack >/dev/null || { echo "corepack is required" >&2; exit 1; }

if [[ "${PREPARE_OFFLINE_SKIP_PLATFORM_CHECK:-0}" != 1 ]]; then
  [[ "$(uname -s)" == Linux ]] || { echo "run this script on Ubuntu 24.04; target-specific native caches are not portable" >&2; exit 1; }
  source /etc/os-release
  [[ "${ID:-}" == ubuntu && "${VERSION_ID:-}" == 24.04 ]] || {
    echo "run this script on Ubuntu 24.04 (got ${ID:-unknown} ${VERSION_ID:-unknown})" >&2
    exit 1
  }
fi

cd "$ROOT"
export COREPACK_HOME="$OUT/cache/corepack"
export COREPACK_DEFAULT_TO_LATEST=0
export COREPACK_ENABLE_NETWORK=1
GO_VERSION=$(go version | awk '{print $3}')
NODE_VERSION=$(node --version)
YARN_VERSION=$(corepack yarn --version)
PNPM_VERSION=$(cd "$ROOT/deepseek-harness" && corepack pnpm --version)

corepack yarn install --immutable
(
  cd server/webadmin
  npm ci --cache "$OUT/cache/npm"
)
(
  cd server
  go mod download
)
(
  cd deepseek-harness
  corepack pnpm install --frozen-lockfile
)

tar -czf "$OUT/cache/yarn-cache.tgz" -C "$ROOT" .yarn/cache
tar -czf "$OUT/cache/go-mod.tgz" -C "$(cd server && go env GOMODCACHE)" .
tar -czf "$OUT/cache/go-build.tgz" -C "$(cd server && go env GOCACHE)" .
tar -czf "$OUT/cache/npm-cache.tgz" -C "$OUT/cache/npm" .
PNPM_STORE=$(cd "$ROOT/deepseek-harness" && corepack pnpm store path)
tar -czf "$OUT/cache/pnpm-store.tgz" -C "$(dirname "$PNPM_STORE")" "$(basename "$PNPM_STORE")"
tar -czf "$OUT/cache/corepack.tgz" -C "$(dirname "$COREPACK_HOME")" "$(basename "$COREPACK_HOME")"
cp package.json yarn.lock .yarnrc.yml "$OUT/"
cp deepseek-harness/package.json deepseek-harness/pnpm-lock.yaml "$OUT/"
printf '%s\n%s\n%s\n%s\n' "$GO_VERSION" "$NODE_VERSION" "$YARN_VERSION" "$PNPM_VERSION" > "$OUT/toolchain.txt"
cat > "$OUT/toolchain/README.txt" <<'TOOLCHAIN'
The offline bundle contains dependency caches, not operating-system installers.
The target machine must provide the exact Go and Node versions recorded in
toolchain.txt, plus Corepack. Installers should be transferred separately.
TOOLCHAIN

cat > "$OUT/restore-offline-cache.sh" <<'RESTORE'
#!/usr/bin/env bash
set -euo pipefail
BUNDLE=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
ROOT=${1:?usage: restore-offline-cache.sh REPO_ROOT}
COREPACK_HOME=${COREPACK_HOME:-"$HOME/.cache/node/corepack"}
mkdir -p "$(dirname "$COREPACK_HOME")"
tar -xzf "$BUNDLE/cache/corepack.tgz" -C "$(dirname "$COREPACK_HOME")"
export COREPACK_HOME
mkdir -p "$ROOT/.yarn/cache"
tar -xzf "$BUNDLE/cache/yarn-cache.tgz" -C "$ROOT"
GOMODCACHE_DIR=${GOMODCACHE:-$(go env GOMODCACHE)}
GOCACHE_DIR=${GOCACHE:-$(go env GOCACHE)}
mkdir -p "$GOMODCACHE_DIR" "$GOCACHE_DIR"
tar -xzf "$BUNDLE/cache/go-mod.tgz" -C "$GOMODCACHE_DIR"
tar -xzf "$BUNDLE/cache/go-build.tgz" -C "$GOCACHE_DIR"
PNPM_STORE=$(cd "$ROOT/deepseek-harness" && corepack pnpm store path)
mkdir -p "$(dirname "$PNPM_STORE")"
tar -xzf "$BUNDLE/cache/pnpm-store.tgz" -C "$(dirname "$PNPM_STORE")"
echo "offline caches restored"
RESTORE
chmod +x "$OUT/restore-offline-cache.sh"

(cd "$OUT" && find . -type f ! -name SHA256SUMS -print0 | sort -z | xargs -0 sha256sum > SHA256SUMS)

echo "prepared bundle at $OUT"
