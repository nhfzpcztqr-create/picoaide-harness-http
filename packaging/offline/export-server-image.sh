#!/usr/bin/env bash
set -euo pipefail

IMAGE=${1:?usage: export-server-image.sh IMAGE_TAG OUTPUT_DIR}
OUT=${2:?usage: export-server-image.sh IMAGE_TAG OUTPUT_DIR}
command -v docker >/dev/null || { echo "docker is required" >&2; exit 1; }
docker image inspect "$IMAGE" >/dev/null || { echo "image not found: $IMAGE" >&2; exit 1; }
OUT=$(mkdir -p "$OUT" && cd "$OUT" && pwd)
for artifact in server-image.tar server-image.tag server-image.tags.json SHA256SUMS; do
  [[ ! -e "$OUT/$artifact" ]] || { echo "refusing to overwrite existing artifact: $OUT/$artifact" >&2; exit 1; }
done
docker save -o "$OUT/server-image.tar" "$IMAGE"
printf '%s\n' "$IMAGE" > "$OUT/server-image.tag"
docker image inspect "$IMAGE" --format '{{json .RepoTags}}' > "$OUT/server-image.tags.json"
(cd "$OUT" && sha256sum server-image.tar server-image.tag server-image.tags.json > SHA256SUMS)
test -s "$OUT/server-image.tar"
echo "exported $IMAGE to $OUT/server-image.tar"
