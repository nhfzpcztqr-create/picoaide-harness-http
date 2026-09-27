#!/usr/bin/env bash
set -euo pipefail

BUNDLE=${1:?usage: build-server-offline.sh BUNDLE_DIR REPO_ROOT}
BUNDLE=$(cd "$BUNDLE" && pwd)
SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=${2:?usage: build-server-offline.sh BUNDLE_DIR REPO_ROOT}
ROOT=$(cd "$REPO_ROOT" && pwd)
[[ -f "$ROOT/package.json" && -f "$ROOT/deepseek-harness/pnpm-lock.yaml" && -f "$ROOT/server/go.mod" ]] || {
  echo "REPO_ROOT is not a complete PicoAide Harness source checkout: $ROOT" >&2
  exit 1
}
DOCKER_IMAGE_BUNDLE=${DOCKER_IMAGE_BUNDLE:-}
if [[ -n "$DOCKER_IMAGE_BUNDLE" ]]; then
  DOCKER_IMAGE_BUNDLE=$(cd "$DOCKER_IMAGE_BUNDLE" && pwd)
fi
if [[ -n "${WINDOWS_INSTALLER:-}" ]]; then
  WINDOWS_INSTALLER="$(cd "$(dirname "$WINDOWS_INSTALLER")" && pwd)/$(basename "$WINDOWS_INSTALLER")"
fi

command -v go >/dev/null || { echo "go is required on the offline builder" >&2; exit 1; }
command -v node >/dev/null || { echo "node is required on the offline builder" >&2; exit 1; }
command -v corepack >/dev/null || { echo "corepack is required on the offline builder" >&2; exit 1; }
[[ -f "$BUNDLE/toolchain.txt" ]] || { echo "bundle has no toolchain.txt" >&2; exit 1; }

"$SCRIPT_DIR/verify-bundle.sh" "$BUNDLE"
export YARN_ENABLE_NETWORK=0
export npm_config_offline=true
export GOPROXY=off
export GOSUMDB=off
export COREPACK_ENABLE_NETWORK=0
export COREPACK_DEFAULT_TO_LATEST=0
export COREPACK_HOME="$BUNDLE/cache/corepack"
export npm_config_cache="$BUNDLE/cache/npm"
[[ -d "$COREPACK_HOME" ]] || { echo "bundle has no Corepack cache: $COREPACK_HOME" >&2; exit 1; }

mapfile -t EXPECTED_TOOLCHAIN < "$BUNDLE/toolchain.txt"
[[ ${#EXPECTED_TOOLCHAIN[@]} -eq 4 ]] || { echo "bundle toolchain.txt is malformed" >&2; exit 1; }
ACTUAL_GO_VERSION=$(go version | awk '{print $3}')
ACTUAL_NODE_VERSION=$(node --version)
ACTUAL_YARN_VERSION=$(cd "$ROOT" && corepack yarn --version)
ACTUAL_PNPM_VERSION=$(cd "$ROOT/deepseek-harness" && corepack pnpm --version)
ACTUAL_TOOLCHAIN=("$ACTUAL_GO_VERSION" "$ACTUAL_NODE_VERSION" "$ACTUAL_YARN_VERSION" "$ACTUAL_PNPM_VERSION")
for index in "${!EXPECTED_TOOLCHAIN[@]}"; do
  [[ "${ACTUAL_TOOLCHAIN[$index]}" == "${EXPECTED_TOOLCHAIN[$index]}" ]] || {
    echo "toolchain mismatch at line $((index + 1)): expected ${EXPECTED_TOOLCHAIN[$index]}, got ${ACTUAL_TOOLCHAIN[$index]}" >&2
    exit 1
  }
done

cd "$ROOT"
if [[ -x "$BUNDLE/restore-offline-cache.sh" ]]; then
  "$BUNDLE/restore-offline-cache.sh" "$ROOT"
fi
if [[ "${BUILD_IMAGE:-0}" == 1 ]]; then
  command -v docker >/dev/null || { echo "docker is required for BUILD_IMAGE=1" >&2; exit 1; }
  if [[ -n "$DOCKER_IMAGE_BUNDLE" ]]; then
    [[ -f "$DOCKER_IMAGE_BUNDLE/cache/runtime-images.tar" && -f "$DOCKER_IMAGE_BUNDLE/runtime-images.SHA256SUMS" && -f "$DOCKER_IMAGE_BUNDLE/runtime-images.txt" ]] || {
      echo "Docker image bundle is incomplete: $DOCKER_IMAGE_BUNDLE" >&2
      exit 1
    }
    (cd "$DOCKER_IMAGE_BUNDLE" && sha256sum --check runtime-images.SHA256SUMS)
    while IFS= read -r image; do
      [[ -n "$image" ]] || continue
      if docker image inspect "$image" >/dev/null 2>&1; then
        echo "image tag already exists: $image; refusing to replace existing local images" >&2
        exit 1
      fi
    done < "$DOCKER_IMAGE_BUNDLE/runtime-images.txt"
    docker load -i "$DOCKER_IMAGE_BUNDLE/cache/runtime-images.tar"
  elif [[ -f "$BUNDLE/cache/runtime-images.tar" ]]; then
    docker load -i "$BUNDLE/cache/runtime-images.tar"
  else
    echo "BUILD_IMAGE=1 requires DOCKER_IMAGE_BUNDLE or cache/runtime-images.tar" >&2
    exit 1
  fi
fi
corepack yarn install --immutable --immutable-cache
(cd deepseek-harness && corepack pnpm install --frozen-lockfile --offline)
(cd server/webadmin && npm ci --offline --cache "$BUNDLE/cache/npm")
make -C server webadmin
mkdir -p server/bin
go build -C server -o bin/picoaide-server ./cmd/server
go build -C server -o bin/picoaide-app-compile ./cmd/picoaide-app-compile

if command -v docker >/dev/null && [[ "${BUILD_IMAGE:-0}" == 1 ]]; then
  WINDOWS_INSTALLER=${WINDOWS_INSTALLER:-}
  [[ -n "$WINDOWS_INSTALLER" && -f "$WINDOWS_INSTALLER" ]] || {
    echo "BUILD_IMAGE=1 requires WINDOWS_INSTALLER pointing to the Windows NSIS .exe" >&2
    exit 1
  }
  [[ "$WINDOWS_INSTALLER" == *.exe || "$WINDOWS_INSTALLER" == *.EXE ]] || {
    echo "WINDOWS_INSTALLER must point to a Windows .exe installer" >&2
    exit 1
  }
  [[ -s "$WINDOWS_INSTALLER" ]] || {
    echo "WINDOWS_INSTALLER is empty: $WINDOWS_INSTALLER" >&2
    exit 1
  }
  SERVER_VERSION=${VERSION:-offline}
  CHANNEL=${CHANNEL:-official}
  CLIENT_VERSION=${CLIENT_VERSION:-$SERVER_VERSION}
  IMAGE_TAG=${TAG:-$SERVER_VERSION}
  if docker image inspect "picoaide-harness-server:$IMAGE_TAG" >/dev/null 2>&1; then
    echo "server image tag already exists: picoaide-harness-server:$IMAGE_TAG; choose a new VERSION/TAG to preserve rollback images" >&2
    exit 1
  fi
  CLIENT_ASSETS=$(mktemp -d "${TMPDIR:-/tmp}/picoaide-client-assets.XXXXXX")
  trap 'rm -rf "$CLIENT_ASSETS"' EXIT
  mkdir -p "$CLIENT_ASSETS/client"
  client_filename=$(basename "$WINDOWS_INSTALLER")
  cp "$WINDOWS_INSTALLER" "$CLIENT_ASSETS/client/$client_filename"
  node - "$CLIENT_ASSETS" "$client_filename" "$CLIENT_VERSION" "$CHANNEL" <<'NODE'
const fs = require('node:fs');
const path = require('node:path');
const crypto = require('node:crypto');
const [assetsDir, filename, version, channel] = process.argv.slice(2);
if (path.basename(filename) !== filename || !filename.toLowerCase().endsWith('.exe')) {
  throw new Error('WINDOWS_INSTALLER must be a Windows .exe with a plain filename');
}
const assetPath = path.join(assetsDir, 'client', filename);
const bytes = fs.readFileSync(assetPath);
const manifest = {
  schema: 1,
  channel_id: channel,
  client: {
    version,
    assets: {
      'win-x64': {
        file: filename,
        sha256: crypto.createHash('sha256').update(bytes).digest('hex'),
        size: bytes.length,
      },
    },
  },
};
fs.writeFileSync(path.join(assetsDir, 'CLIENT-RELEASE.json'), `${JSON.stringify(manifest, null, 2)}\n`);
NODE
  npm_cache="$ROOT/server/.offline-npm-cache"
  go_cache="$ROOT/server/.offline-go-modcache"
  mkdir -p "$npm_cache" "$go_cache"
  tar -xzf "$BUNDLE/cache/npm-cache.tgz" -C "$npm_cache"
  tar -xzf "$BUNDLE/cache/go-mod.tgz" -C "$go_cache"
  make -C server docker-image IMAGE=picoaide-harness-server VERSION="$SERVER_VERSION" TAG="$IMAGE_TAG" \
    OFFLINE_BUILD=1 GO_MODULE_CACHE="$go_cache" NPM_CACHE="$npm_cache" \
    CLIENT_ASSETS="$CLIENT_ASSETS" CHANNEL="$CHANNEL" CHANNEL_CONTEXT="${CHANNEL_CONTEXT:-$ROOT/server/channels-context}" \
    RUNTIME_BASE="picoaide-harness-runtime:3.21-amd64" PLATFORM="linux/amd64"
fi

echo "offline server build complete: $ROOT/server/bin/picoaide-server"
