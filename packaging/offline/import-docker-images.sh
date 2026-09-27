#!/usr/bin/env bash
set -euo pipefail

BUNDLE=${1:?usage: import-docker-images.sh DOCKER_IMAGE_BUNDLE_DIR}
BUNDLE=$(cd "$BUNDLE" && pwd)
command -v docker >/dev/null || { echo "docker is required" >&2; exit 1; }
[[ -f "$BUNDLE/cache/runtime-images.tar" && -f "$BUNDLE/runtime-images.txt" && -f "$BUNDLE/runtime-images.SHA256SUMS" ]] || {
  echo "Docker image bundle is incomplete" >&2
  exit 1
}
(cd "$BUNDLE" && sha256sum --check runtime-images.SHA256SUMS)
while IFS= read -r image; do
  [[ -n "$image" ]] || continue
  if docker image inspect "$image" >/dev/null 2>&1; then
    echo "image tag already exists: $image; refusing to replace existing local images" >&2
    exit 1
  fi
done < "$BUNDLE/runtime-images.txt"
docker load -i "$BUNDLE/cache/runtime-images.tar"
while IFS= read -r image; do
  [[ -n "$image" ]] || continue
  platform=$(docker image inspect "$image" --format '{{.Os}}/{{.Architecture}}')
  [[ "$platform" == linux/amd64 ]] || { echo "$image has unexpected platform $platform" >&2; exit 1; }
done < "$BUNDLE/runtime-images.txt"
echo "imported first-install Docker images from $BUNDLE"
