#!/usr/bin/env bash
set -euo pipefail

ROOT=${1:?usage: register-windows-installer.sh RELEASE_ROOT INSTALLER VERSION [CHANNEL]}
INSTALLER=${2:?usage: register-windows-installer.sh RELEASE_ROOT INSTALLER VERSION [CHANNEL]}
VERSION=${3:?usage: register-windows-installer.sh RELEASE_ROOT INSTALLER VERSION [CHANNEL]}
CHANNEL=${4:-inner}
ROOT=$(cd "$ROOT" && pwd)
RELEASE_DIR="$ROOT/client-release"
[[ -d "$RELEASE_DIR" ]] || { echo "missing client-release directory: $RELEASE_DIR" >&2; exit 1; }
[[ -f "$INSTALLER" ]] || { echo "installer not found: $INSTALLER" >&2; exit 1; }
INSTALLER=$(cd "$(dirname "$INSTALLER")" && pwd)/$(basename "$INSTALLER")
FILENAME=$(basename "$INSTALLER")
[[ "$FILENAME" =~ \.[Ee][Xx][Ee]$ ]] || { echo "installer must be a .exe: $FILENAME" >&2; exit 1; }
[[ "$FILENAME" != .* && "$FILENAME" != *..* && "$FILENAME" != */* && "$FILENAME" != *\\* ]] || {
  echo "installer filename is unsafe: $FILENAME" >&2
  exit 1
}
[[ "$VERSION" =~ ^[0-9A-Za-z][0-9A-Za-z._-]*$ ]] || { echo "invalid version: $VERSION" >&2; exit 1; }
[[ "$CHANNEL" =~ ^[0-9A-Za-z][0-9A-Za-z._-]*$ ]] || { echo "invalid channel: $CHANNEL" >&2; exit 1; }

TARGET="$RELEASE_DIR/$FILENAME"
TMP_ASSET="$RELEASE_DIR/.$FILENAME.tmp.$$"
TMP_MANIFEST="$RELEASE_DIR/.CLIENT-RELEASE.json.tmp.$$"
cleanup() { rm -f "$TMP_ASSET" "$TMP_MANIFEST"; }
trap cleanup EXIT
cp "$INSTALLER" "$TMP_ASSET"
mv -f "$TMP_ASSET" "$TARGET"
SHA256=$(sha256sum "$TARGET" | awk '{print $1}')
SIZE=$(stat -c '%s' "$TARGET" 2>/dev/null || stat -f '%z' "$TARGET")
python3 - "$TMP_MANIFEST" "$CHANNEL" "$VERSION" "$FILENAME" "$SHA256" "$SIZE" <<'PY'
import json
import pathlib
import sys

manifest_path, channel, version, filename, sha256, size = sys.argv[1:]
data = {
    "schema": 1,
    "channel_id": channel,
    "client": {
        "version": version,
        "assets": {
            "win-x64": {
                "file": filename,
                "sha256": sha256,
                "size": int(size),
            }
        },
    },
}
pathlib.Path(manifest_path).write_text(json.dumps(data, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
PY
mv -f "$TMP_MANIFEST" "$RELEASE_DIR/CLIENT-RELEASE.json"
"$ROOT/offline-build/verify-windows-only.sh" "$ROOT"
echo "registered Windows installer: $TARGET"
