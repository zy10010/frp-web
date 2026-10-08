#!/usr/bin/env bash
#
# Build a distributable frp-manager release tarball.
#
#   ./build-release.sh                    # build for current platform
#   ./build-release.sh --os linux --arch arm64
#   ./build-release.sh --bundle-frp       # also bundle frps/frpc (offline install)
#
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="${VERSION:-1.0.0}"
OS="$(go env GOOS)"
ARCH="$(go env GOARCH)"
BUNDLE_FRP="no"
OUT_DIR="${REPO_ROOT}/release"

while [ $# -gt 0 ]; do
  case "$1" in
    --os)          OS="$2"; shift 2 ;;
    --arch)        ARCH="$2"; shift 2 ;;
    --bundle-frp)  BUNDLE_FRP="yes"; shift ;;
    --out)         OUT_DIR="$2"; shift 2 ;;
    --version)     VERSION="$2"; shift 2 ;;
    *) echo "unknown option: $1" >&2; exit 1 ;;
  esac
done

info() { echo "[release] $*"; }

info "building frp-manager ${VERSION} for ${OS}/${ARCH}"
mkdir -p "$OUT_DIR"

ldflags="-s -w -X main.managerVersion=${VERSION}"
CGO_ENABLED=0 GOOS="$OS" GOARCH="$ARCH" go build -trimpath -ldflags "$ldflags" \
  -o "$OUT_DIR/frp-manager" "${REPO_ROOT}/cmd/frp-manager"

if [ "$BUNDLE_FRP" = "yes" ]; then
  info "bundling frps/frpc (noweb build)"
  CGO_ENABLED=0 GOOS="$OS" GOARCH="$ARCH" go build -trimpath -ldflags "-s -w" -tags noweb \
    -o "$OUT_DIR/frps" "${REPO_ROOT}/cmd/frps"
  CGO_ENABLED=0 GOOS="$OS" GOARCH="$ARCH" go build -trimpath -ldflags "-s -w" -tags noweb \
    -o "$OUT_DIR/frpc" "${REPO_ROOT}/cmd/frpc"
fi

cp -f "${REPO_ROOT}/packaging/install.sh" "$OUT_DIR/install.sh"
cp -f "${REPO_ROOT}/packaging/install-alpine.sh" "$OUT_DIR/install-alpine.sh"

TARBALL="frp-manager-${VERSION}-${OS}-${ARCH}.tar.gz"
info "packaging ${TARBALL}"
tar -C "$OUT_DIR" -czf "$OUT_DIR/${TARBALL}" \
  frp-manager install.sh install-alpine.sh \
  $( [ "$BUNDLE_FRP" = "yes" ] && echo frps frpc )

info "done: $OUT_DIR/${TARBALL}"
