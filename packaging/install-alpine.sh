#!/bin/sh
#
# frp-manager installer for Alpine Linux (OpenRC).
#
# Usage:
#   sh install-alpine.sh --password YourPass --mirror https://ghproxy.com/
#
set -eu

# ---------------------------------------------------------------------------
# Defaults
# ---------------------------------------------------------------------------
INSTALL_DIR="/opt/frp"
WEB_ADDR="0.0.0.0"
WEB_PORT="7500"
WEB_USER="admin"
WEB_PASSWORD=""
MIRROR=""
AUTOSTART="yes"
FRPS_AUTOSTART="yes"
FRPC_AUTOSTART="no"
BIND_PORT="7000"
TOKEN=""
SERVER_ADDR="127.0.0.1"
FRP_VERSION="v0.71.0"
MANAGER_URL=""
SKIP_DOWNLOAD="no"
DOWNLOADER=""

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

info()  { echo "[frp-manager] $*"; }
err()   { echo "[frp-manager] ERROR: $*" >&2; exit 1; }

have() { command -v "$1" >/dev/null 2>&1; }

detect_downloader() {
  if have curl; then
    DOWNLOADER="curl"
  elif have wget; then
    DOWNLOADER="wget"
  else
    err "curl or wget is required"
  fi
}

download() {
  case "$DOWNLOADER" in
    curl) curl -fsSL --retry 3 -o "$2" "$1" ;;
    wget) wget -q -O "$2" "$1" ;;
  esac
}

random_password() {
  if have openssl; then
    openssl rand -hex 8
  else
    head -c 16 /dev/urandom | od -An -tx1 | tr -d ' \n' | head -c 16
  fi
}

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64)        echo "amd64" ;;
    aarch64|arm64)       echo "arm64" ;;
    armv7l|armv7*|armv8*|armhf) echo "arm_hf" ;;
    armv6l|armv5*|arm*)  echo "arm" ;;
    mips)                echo "mips" ;;
    mips64)              echo "mips64" ;;
    mips64el)            echo "mips64le" ;;
    mipsel)              echo "mipsle" ;;
    riscv64)             echo "riscv64" ;;
    loongarch64)         echo "loong64" ;;
    *)                   err "unsupported architecture: $(uname -m)" ;;
  esac
}

usage() {
  cat <<EOF
Usage: sh install-alpine.sh [options]

Options (same as install.sh):
  --dir PATH             Install directory (default: /opt/frp)
  --port PORT            Web panel port (default: 7500)
  --addr ADDR            Web panel bind address (default: 0.0.0.0)
  --user NAME            Web panel username (default: admin)
  --password PASS        Web panel password (default: random)
  --mirror URL           GitHub mirror prefix (default: none)
  --bind-port PORT       frps bind port (default: 7000)
  --token TOKEN          frp auth token (default: random)
  --server-addr ADDR     frpc server address (default: 127.0.0.1)
  --frp-version VER      frp release tag (default: v0.71.0)
  --autostart yes|no     Install & enable boot service (default: yes)
  --frps-autostart yes|no
  --frpc-autostart yes|no
  --manager-url URL      Download frp-manager from URL
  --skip-download        Use bundled frps/frpc next to this script
  -y                     Non-interactive
  -h, --help             Show this help
EOF
}

# ---------------------------------------------------------------------------
# Parse arguments
# ---------------------------------------------------------------------------
NONINTERACTIVE=""
while [ $# -gt 0 ]; do
  case "$1" in
    --dir)           INSTALL_DIR="$2"; shift 2 ;;
    --dir=*)         INSTALL_DIR="${1#*=}"; shift ;;
    --port)          WEB_PORT="$2"; shift 2 ;;
    --port=*)        WEB_PORT="${1#*=}"; shift ;;
    --addr)          WEB_ADDR="$2"; shift 2 ;;
    --addr=*)        WEB_ADDR="${1#*=}"; shift ;;
    --user)          WEB_USER="$2"; shift 2 ;;
    --user=*)        WEB_USER="${1#*=}"; shift ;;
    --password)      WEB_PASSWORD="$2"; shift 2 ;;
    --password=*)    WEB_PASSWORD="${1#*=}"; shift ;;
    --mirror)        MIRROR="$2"; shift 2 ;;
    --mirror=*)      MIRROR="${1#*=}"; shift ;;
    --bind-port)     BIND_PORT="$2"; shift 2 ;;
    --bind-port=*)   BIND_PORT="${1#*=}"; shift ;;
    --token)         TOKEN="$2"; shift 2 ;;
    --token=*)       TOKEN="${1#*=}"; shift ;;
    --server-addr)   SERVER_ADDR="$2"; shift 2 ;;
    --server-addr=*) SERVER_ADDR="${1#*=}"; shift ;;
    --frp-version)   FRP_VERSION="$2"; shift 2 ;;
    --frp-version=*) FRP_VERSION="${1#*=}"; shift ;;
    --autostart)     AUTOSTART="$2"; shift 2 ;;
    --autostart=*)   AUTOSTART="${1#*=}"; shift ;;
    --frps-autostart) FRPS_AUTOSTART="$2"; shift 2 ;;
    --frps-autostart=*) FRPS_AUTOSTART="${1#*=}"; shift ;;
    --frpc-autostart) FRPC_AUTOSTART="$2"; shift 2 ;;
    --frpc-autostart=*) FRPC_AUTOSTART="${1#*=}"; shift ;;
    --manager-url)   MANAGER_URL="$2"; shift 2 ;;
    --manager-url=*) MANAGER_URL="${1#*=}"; shift ;;
    --skip-download) SKIP_DOWNLOAD="yes"; shift ;;
    -y|--yes)        NONINTERACTIVE="1"; shift ;;
    -h|--help)       usage; exit 0 ;;
    *) err "unknown option: $1" ;;
  esac
done

[ "$(id -u)" -eq 0 ] || err "this script must be run as root"

if [ -z "$WEB_PASSWORD" ]; then
  WEB_PASSWORD="$(random_password)"
fi
if [ -z "$TOKEN" ]; then
  TOKEN="$(random_password)"
fi

detect_downloader
ARCH="$(detect_arch)"
OS="linux"
VERSION_NO_V="${FRP_VERSION#v}"
ASSET="frp_${VERSION_NO_V}_${OS}_${ARCH}.tar.gz"
BASE_URL="https://github.com/fatedier/frp/releases/download/${FRP_VERSION}/${ASSET}"

info "installing to $INSTALL_DIR"
info "platform: ${OS}_${ARCH}  frp version: ${FRP_VERSION}"

mkdir -p "$INSTALL_DIR/bin"

# ---------------------------------------------------------------------------
# Install frps / frpc
# ---------------------------------------------------------------------------
if [ "$SKIP_DOWNLOAD" = "yes" ] && [ -x "$SCRIPT_DIR/frps" ] && [ -x "$SCRIPT_DIR/frpc" ]; then
  info "using bundled frps/frpc"
  cp -f "$SCRIPT_DIR/frps" "$INSTALL_DIR/bin/frps"
  cp -f "$SCRIPT_DIR/frpc" "$INSTALL_DIR/bin/frpc"
else
  info "downloading frp $FRP_VERSION ($ARCH) ..."
  TMP_DIR="$(mktemp -d)"
  trap 'rm -rf "$TMP_DIR"' EXIT
  URL="$BASE_URL"
  if [ -n "$MIRROR" ]; then
    URL="${MIRROR%/}/$BASE_URL"
  fi
  if ! download "$URL" "$TMP_DIR/$ASSET"; then
    if [ -n "$MIRROR" ]; then
      info "mirror download failed, retrying directly ..."
      download "$BASE_URL" "$TMP_DIR/$ASSET"
    else
      err "failed to download $URL"
    fi
  fi
  tar -xzf "$TMP_DIR/$ASSET" -C "$TMP_DIR"
  EXTRACTED_DIR="$TMP_DIR/frp_${VERSION_NO_V}_${OS}_${ARCH}"
  cp -f "$EXTRACTED_DIR/frps" "$INSTALL_DIR/bin/frps"
  cp -f "$EXTRACTED_DIR/frpc" "$INSTALL_DIR/bin/frpc"
fi
chmod +x "$INSTALL_DIR/bin/frps" "$INSTALL_DIR/bin/frpc"
info "installed frps/frpc to $INSTALL_DIR/bin"

# ---------------------------------------------------------------------------
# Install frp-manager
# ---------------------------------------------------------------------------
if [ -x "$SCRIPT_DIR/frp-manager" ]; then
  info "installing bundled frp-manager"
  cp -f "$SCRIPT_DIR/frp-manager" "$INSTALL_DIR/bin/frp-manager"
elif [ -n "$MANAGER_URL" ]; then
  info "downloading frp-manager from $MANAGER_URL"
  download "$MANAGER_URL" "$INSTALL_DIR/bin/frp-manager"
else
  err "frp-manager binary not found next to this script; pass --manager-url"
fi
chmod +x "$INSTALL_DIR/bin/frp-manager"

# ---------------------------------------------------------------------------
# Write configs
# ---------------------------------------------------------------------------
cat > "$INSTALL_DIR/frps.toml" <<EOF
bindAddr = "0.0.0.0"
bindPort = ${BIND_PORT}
auth.method = "token"
auth.token = "${TOKEN}"
EOF

cat > "$INSTALL_DIR/frpc.toml" <<EOF
serverAddr = "${SERVER_ADDR}"
serverPort = ${BIND_PORT}
auth.method = "token"
auth.token = "${TOKEN}"
EOF

AUTOSTART_FRPS_BOOL="false"
[ "$FRPS_AUTOSTART" = "yes" ] && AUTOSTART_FRPS_BOOL="true"
AUTOSTART_FRPC_BOOL="false"
[ "$FRPC_AUTOSTART" = "yes" ] && AUTOSTART_FRPC_BOOL="true"

cat > "$INSTALL_DIR/frp-manager.toml" <<EOF
frpsPath = '${INSTALL_DIR}/bin/frps'
frpcPath = '${INSTALL_DIR}/bin/frpc'
frpsConfig = '${INSTALL_DIR}/frps.toml'
frpcConfig = '${INSTALL_DIR}/frpc.toml'
workDir = '${INSTALL_DIR}'
mirror = '${MIRROR%/}'
autostartFrps = ${AUTOSTART_FRPS_BOOL}
autostartFrpc = ${AUTOSTART_FRPC_BOOL}
logMaxBytes = 262144

[web]
addr = '${WEB_ADDR}'
port = ${WEB_PORT}
user = '${WEB_USER}'
password = '${WEB_PASSWORD}'
EOF
chmod 600 "$INSTALL_DIR/frp-manager.toml"

# ---------------------------------------------------------------------------
# Install OpenRC service
# ---------------------------------------------------------------------------
if have rc-service && have rc-update; then
  cat > /etc/init.d/frp-manager <<EOF
#!/sbin/openrc-run
name="frp-manager"
description="frp-manager - web management for frp"
command="${INSTALL_DIR}/bin/frp-manager"
command_args="-c ${INSTALL_DIR}/frp-manager.toml"
command_background=true
pidfile="/run/frp-manager.pid"

depend() {
  need net
}
EOF
  chmod +x /etc/init.d/frp-manager
  if [ "$AUTOSTART" = "yes" ]; then
    rc-update add frp-manager default >/dev/null 2>&1 || true
  fi
  rc-service frp-manager restart
  info "OpenRC service 'frp-manager' installed and started"
else
  info "OpenRC not found; starting frp-manager in background"
  nohup "$INSTALL_DIR/bin/frp-manager" -c "$INSTALL_DIR/frp-manager.toml" \
    > "$INSTALL_DIR/frp-manager.log" 2>&1 &
  echo "$!" > "$INSTALL_DIR/frp-manager.pid"
  info "WARNING: no init system detected; frp-manager will NOT auto-start on boot"
fi

# ---------------------------------------------------------------------------
# Summary
# ---------------------------------------------------------------------------
echo
info "Installation complete."
echo "  Web panel:  http://<server-ip>:${WEB_PORT}"
echo "  Username:   ${WEB_USER}"
echo "  Password:   ${WEB_PASSWORD}"
echo "  frps bind:  :${BIND_PORT}"
echo "  frp token:  ${TOKEN}"
echo "  Install dir:${INSTALL_DIR}"
echo "  Service:    frp-manager (autostart=${AUTOSTART})"
if [ "$AUTOSTART" = "yes" ]; then
  echo "  Boot autostart is enabled."
else
  echo "  Boot autostart is disabled; start manually with: rc-service frp-manager start"
fi
