#!/usr/bin/env bash
set -euo pipefail

OUT=${1:?usage: prepare-ubuntu-toolchain.sh OUTPUT_DIR [GO_VERSION] [NODE_VERSION] [ARCH]}
GO_VERSION=${2:-1.27.1}
NODE_VERSION=${3:-22.19.0}
ARCH=${4:-amd64}

[[ "$GO_VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "invalid GO_VERSION=$GO_VERSION" >&2; exit 1; }
[[ "$NODE_VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo "invalid NODE_VERSION=$NODE_VERSION" >&2; exit 1; }

case "$ARCH" in
  amd64) GO_ARCH=amd64; NODE_ARCH=x64 ;;
  arm64) GO_ARCH=arm64; NODE_ARCH=arm64 ;;
  *) echo "unsupported ARCH=$ARCH (use amd64 or arm64)" >&2; exit 1 ;;
esac

command -v curl >/dev/null || { echo "curl is required" >&2; exit 1; }
command -v sha256sum >/dev/null || { echo "sha256sum is required" >&2; exit 1; }
OUT=$(mkdir -p "$OUT/toolchain" && cd "$OUT" && pwd)
GO_ARCHIVE="go${GO_VERSION}.linux-${GO_ARCH}.tar.gz"
NODE_ARCHIVE="node-v${NODE_VERSION}-linux-${NODE_ARCH}.tar.xz"

curl --fail --location --retry 3 -o "$OUT/toolchain/$GO_ARCHIVE" "https://go.dev/dl/$GO_ARCHIVE"
curl --fail --location --retry 3 -o "$OUT/toolchain/$NODE_ARCHIVE" "https://nodejs.org/dist/v${NODE_VERSION}/$NODE_ARCHIVE"

{
  echo "GO_VERSION=$GO_VERSION"
  echo "NODE_VERSION=$NODE_VERSION"
  echo "ARCH=$ARCH"
} > "$OUT/toolchain/manifest.txt"

cat > "$OUT/install-ubuntu-toolchain.sh" <<'INSTALL'
#!/usr/bin/env bash
set -euo pipefail
BUNDLE=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
PREFIX=${1:-/opt/picoaide/toolchain}
(cd "$BUNDLE" && sha256sum --check SHA256SUMS)
GO_VERSION=''
NODE_VERSION=''
ARCH=''
while IFS='=' read -r key value; do
  case "$key" in
    GO_VERSION) GO_VERSION=$value ;;
    NODE_VERSION) NODE_VERSION=$value ;;
    ARCH) ARCH=$value ;;
  esac
done < "$BUNDLE/toolchain/manifest.txt"
[[ "$GO_VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo 'invalid Go version in toolchain manifest' >&2; exit 1; }
[[ "$NODE_VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]] || { echo 'invalid Node version in toolchain manifest' >&2; exit 1; }
case "$ARCH" in
  amd64|arm64) ;;
  *) echo 'invalid architecture in toolchain manifest' >&2; exit 1 ;;
esac
command -v tar >/dev/null || { echo 'tar is required' >&2; exit 1; }
command -v xz >/dev/null || { echo 'xz-utils is required (install it on Ubuntu before unpacking)' >&2; exit 1; }
HOST_ARCH=$(uname -m)
case "$HOST_ARCH" in
  x86_64) HOST_ARCH=amd64 ;;
  aarch64) HOST_ARCH=arm64 ;;
esac
[[ "$HOST_ARCH" == "$ARCH" ]] || { echo "toolchain architecture $ARCH does not match host $HOST_ARCH" >&2; exit 1; }
GO_ARCHIVE="$BUNDLE/toolchain/go${GO_VERSION}.linux-${ARCH}.tar.gz"
case "$ARCH" in
  amd64) NODE_ARCH=x64 ;;
  arm64) NODE_ARCH=arm64 ;;
esac
NODE_ARCHIVE="$BUNDLE/toolchain/node-v${NODE_VERSION}-linux-${NODE_ARCH}.tar.xz"
[[ -f "$GO_ARCHIVE" && -f "$NODE_ARCHIVE" ]] || { echo 'toolchain archives are missing' >&2; exit 1; }
mkdir -p "$PREFIX"
GO_DEST="$PREFIX/go-$GO_VERSION"
NODE_DEST="$PREFIX/node-$NODE_VERSION"
ACTIVATE="$PREFIX/activate-$GO_VERSION-$NODE_VERSION.sh"
[[ ! -e "$GO_DEST" && ! -e "$NODE_DEST" && ! -e "$ACTIVATE" ]] || {
  echo "versioned toolchain destination already exists under $PREFIX; choose another PREFIX or remove only that version after review" >&2
  exit 1
}
TMP=$(mktemp -d "$PREFIX/.toolchain-install.XXXXXX")
trap 'rm -rf "$TMP"' EXIT
tar -xzf "$GO_ARCHIVE" -C "$TMP"
tar -xJf "$NODE_ARCHIVE" -C "$TMP"
NODE_DIR="node-v${NODE_VERSION}-linux-${NODE_ARCH}"
[[ -x "$TMP/go/bin/go" && -x "$TMP/$NODE_DIR/bin/node" ]] || { echo 'toolchain archive layout is unexpected' >&2; exit 1; }
mv "$TMP/go" "$GO_DEST"
mv "$TMP/$NODE_DIR" "$NODE_DEST"
cat > "$ACTIVATE" <<ACTIVATE_SCRIPT
#!/usr/bin/env bash
export PATH="$GO_DEST/bin:$NODE_DEST/bin:\$PATH"
ACTIVATE_SCRIPT
chmod 0644 "$ACTIVATE"
echo "installed Go $GO_VERSION and Node $NODE_VERSION under $PREFIX"
echo "activate with: source '$ACTIVATE'"
INSTALL
chmod +x "$OUT/install-ubuntu-toolchain.sh"

cat > "$OUT/verify-toolchain.sh" <<'VERIFY'
#!/usr/bin/env bash
set -euo pipefail
BUNDLE=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
PREFIX=${1:-"$BUNDLE/.verify-install"}
"$BUNDLE/install-ubuntu-toolchain.sh" "$PREFIX"
source "$PREFIX/activate-$(sed -n 's/^GO_VERSION=//p' "$BUNDLE/toolchain/manifest.txt")-$(sed -n 's/^NODE_VERSION=//p' "$BUNDLE/toolchain/manifest.txt").sh"
command -v go >/dev/null
command -v node >/dev/null
go version
node --version
rm -rf "$PREFIX"
echo "verified Ubuntu toolchain bundle: $BUNDLE"
VERIFY
chmod +x "$OUT/verify-toolchain.sh"

(cd "$OUT" && find . -type f ! -name SHA256SUMS -print0 | sort -z | xargs -0 sha256sum > SHA256SUMS)
echo "prepared Ubuntu toolchain bundle at $OUT"
