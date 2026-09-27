#!/usr/bin/env bash
set -euo pipefail

BUNDLE=${1:?usage: verify-bundle.sh BUNDLE_DIR}
if [[ ! -f "$BUNDLE/SHA256SUMS" ]]; then
  echo "missing SHA256SUMS" >&2
  exit 1
fi
(cd "$BUNDLE" && sha256sum --check SHA256SUMS)
if [[ -x "$BUNDLE/verify-toolchain.sh" ]]; then
  echo "bundle checksum verification passed: $BUNDLE"
fi
