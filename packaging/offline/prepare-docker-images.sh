#!/usr/bin/env bash
set -euo pipefail

OUT=${1:?usage: prepare-docker-images.sh OUTPUT_DIR [PLATFORM]}
PLATFORM=${2:-linux/amd64}
command -v docker >/dev/null || { echo "docker is required" >&2; exit 1; }
OUT=$(mkdir -p "$OUT/cache" && cd "$OUT" && pwd)

images=(
  "node:24-bookworm-slim"
  "node:24-alpine"
  "golang:1.26-alpine"
  "alpine:3.21"
  "caddy:2-alpine"
  "postgres:18-alpine"
)
for image in "${images[@]}" "picoaide-harness-runtime:3.21-amd64"; do
  if docker image inspect "$image" >/dev/null 2>&1; then
    echo "image tag already exists: $image; use a fresh, isolated Docker daemon to avoid replacing local images" >&2
    exit 1
  fi
done
for image in "${images[@]}"; do
  docker pull --platform "$PLATFORM" "$image"
done

if [[ "$PLATFORM" != linux/amd64 ]]; then
  echo "runtime base preparation currently supports linux/amd64 only" >&2
  exit 1
fi
container=$(docker create --platform "$PLATFORM" alpine:3.21 sh -c \
  'apk add --no-cache ca-certificates tzdata su-exec bubblewrap')
trap 'docker rm -f "$container" >/dev/null 2>&1 || true' EXIT
docker start -a "$container"
docker commit "$container" picoaide-harness-runtime:3.21-amd64 >/dev/null
images+=("picoaide-harness-runtime:3.21-amd64")
docker save -o "$OUT/cache/runtime-images.tar" "${images[@]}"
printf '%s\n' "${images[@]}" > "$OUT/runtime-images.txt"
(cd "$OUT" && sha256sum cache/runtime-images.tar runtime-images.txt > runtime-images.SHA256SUMS)
echo "prepared Docker image bundle at $OUT"
