#!/usr/bin/env bash
set -euo pipefail

BUNDLE=${1:?usage: import-server-image.sh IMAGE_BUNDLE_DIR}
command -v docker >/dev/null || { echo "docker is required" >&2; exit 1; }
[[ -f "$BUNDLE/server-image.tar" && -f "$BUNDLE/server-image.tag" && -f "$BUNDLE/SHA256SUMS" ]] || {
  echo "server image bundle is incomplete" >&2
  exit 1
}
(cd "$BUNDLE" && sha256sum --check SHA256SUMS)
IMAGE=$(cat "$BUNDLE/server-image.tag")
[[ "$IMAGE" =~ ^[A-Za-z0-9._/-]+:[A-Za-z0-9_.-]+$ ]] || { echo "invalid image tag in bundle" >&2; exit 1; }
if docker image inspect "$IMAGE" >/dev/null 2>&1; then
  echo "image tag already exists: $IMAGE; refusing to replace it (choose a versioned, unused tag)" >&2
  exit 1
fi
docker load -i "$BUNDLE/server-image.tar"
docker image inspect "$IMAGE" >/dev/null 2>&1 || {
  echo "loaded archive does not contain declared image tag: $IMAGE" >&2
  exit 1
}
platform=$(docker image inspect "$IMAGE" --format '{{.Os}}/{{.Architecture}}')
[[ "$platform" == linux/amd64 ]] || {
  echo "$IMAGE has unexpected platform $platform (expected linux/amd64)" >&2
  exit 1
}
echo "imported server image bundle from $BUNDLE"
