#!/usr/bin/env bash
#
# frp-manager installer for Linux with systemd (Debian, Ubuntu, CentOS 7+, ...).
#
# One-click install:
#   curl -fsSL https://your-host/install.sh | bash -s -- --password YourPass
#
# Or download and run:
#   bash install.sh --port 7500 --password YourPass --mirror https://ghproxy.com/
#
set -euo pipefail

# ---------------------------------------------------------------------------
# Defaults
# ---------------------------------------------------------------------------
INSTALL_DIR="/opt/frp"
WEB_ADDR="0.0.0.0"
WEB_PORT="7500"
WEB_USER="admin"
WEB_PASSWORD=""
MIRROR=""
AUTOSTART="yes"          # install + enable the boot service (yes|no)
FRPS_AUTOSTART="yes"     # start frps when the manager boots (yes|no)
FRPC_AUTOSTART="no"      # start frpc when the manager boots (yes|no)
BIND_PORT="7000"
TOKEN=""
SERVER_ADDR="127.0.0.1"
FRP_VERSION="v0.71.0"    # pinned default; use --frp-version to override
MANAGER_URL=""           # optional URL to download frp-manager
SKIP_DOWNLOAD="no"       # use bundled frps/frpc instead of downloading
DOWNLOADER=""

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------
info()  { echo "[frp-manager] $*"; }
err()   { echo "[frp-manager] ERROR: $*" >&2; exit 1; }

have() { command -v "$1" >/dev/null 2>&1; }

# Resolve a downloader (curl preferred, then wget).
detect_downloader() {
  if have curl; then
    DOWNLOADER="curl"
  elif have wget; then
    DOWNLOADER="wget"
  else
    err "curl or wget is required"
  fi
}

download() { # $1=url $2=output
  case "$DOWNLOADER" in
    curl) curl -fsSL --retry 3 -o "$2" "$1" ;;
    wget) wget -q -O "$2" "$1" ;;
  esac
}

random_password() {
  if have openssl; then
    openssl rand -hex 8
  else
    tr -dc 'a-zA-Z0-9' < /dev/urandom 2>/dev/null | head -c 16 || echo "admin"
  fi
}

# Map uname -m to the frp release arch suffix.
detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64)          echo "amd64" ;;
    aarch64|arm64)         echo "arm64" ;;
    armv7l|armv7*|armv8*|armhf) echo "arm_hf" ;;
    armv6l|armv5*|arm*)    echo "arm" ;;
    mips)                  echo "mips" ;;
    mips64)                echo "mips64" ;;
    mips64el)              echo "mips64le" ;;
    mipsel)                echo "mipsle" ;;
    riscv64)               echo "riscv64" ;;
    loongarch64)           echo "loong64" ;;
    *)                     err "unsupported architecture: $(uname -m)" ;;
  esac
}

# ---------------------------------------------------------------------------
# Usage
# ---------------------------------------------------------------------------
usage() {
  cat <<EOF
Usage: bash install.sh [options]

Options:
  --dir PATH             Install directory (default: /opt/frp)
  --port PORT            Web panel port (default: 7500)
  --addr ADDR            Web panel bind address (default: 0.0.0.0)
  --user NAME            Web panel username (default: admin)
  --password PASS        Web panel password (default: random, printed at end)
  --mirror URL           GitHub mirror prefix, e.g. https://ghproxy.com/ (default: none)
  --bind-port PORT       frps bind port (default: 7000)
  --token TOKEN          frp auth token (default: random)
  --server-addr ADDR     frpc server address (default: 127.0.0.1)
  --frp-version VER      frp release tag to install (default: v0.71.0)
  --autostart yes|no     Install & enable boot service (default: yes)
  --frps-autostart yes|no  Auto-start frps when manager boots (default: yes)
  --frpc-autostart yes|no  Auto-start frpc when manager boots (default: no)
  --manager-url URL      Download frp-manager from URL (otherwise use bundled)
  --skip-download        Use bundled frps/frpc next to this script
  -y                     Accept all defaults, non-interactive
  -h, --help             Show this help
EOF
}

# ---------------------------------------------------------------------------
# Parse arguments
# ---------------------------------------------------------------------------
NONINTERACTIVE=""
while [ $# -gt 0 ]; do
  case "$1" in
    --dir)            INSTALL_DIR="$2"; shift 2 ;;
    --dir=*)          INSTALL_DIR="${1#*=}"; shift ;;
    --port)           WEB_PORT="$2"; shift 2 ;;
    --port=*)         WEB_PORT="${1#*=}"; shift ;;
    --addr)           WEB_ADDR="$2"; shift 2 ;;
    --addr=*)         WEB_ADDR="${1#*=}"; shift ;;
    --user)           WEB_USER="$2"; shift 2 ;;
    --user=*)         WEB_USER="${1#*=}"; shift ;;
    --password)       WEB_PASSWORD="$2"; shift 2 ;;
    --password=*)     WEB_PASSWORD="${1#*=}"; shift ;;
    --mirror)         MIRROR="$2"; shift 2 ;;
    --mirror=*)       MIRROR="${1#*=}"; shift ;;
    --bind-port)      BIND_PORT="$2"; shift 2 ;;
    --bind-port=*)    BIND_PORT="${1#*=}"; shift ;;
    --token)          TOKEN="$2"; shift 2 ;;
    --token=*)        TOKEN="${1#*=}"; shift ;;
    --server-addr)    SERVER_ADDR="$2"; shift 2 ;;
    --server-addr=*)  SERVER_ADDR="${1#*=}"; shift ;;
    --frp-version)    FRP_VERSION="$2"; shift 2 ;;
    --frp-version=*)  FRP_VERSION="${1#*=}"; shift ;;
    --autostart)      AUTOSTART="$2"; shift 2 ;;
    --autostart=*)    AUTOSTART="${1#*=}"; shift ;;
    --frps-autostart) FRPS_AUTOSTART="$2"; shift 2 ;;
    --frps-autostart=*) FRPS_AUTOSTART="${1#*=}"; shift ;;
    --frpc-autostart) FRPC_AUTOSTART="$2"; shift 2 ;;
    --frpc-autostart=*) FRPC_AUTOSTART="${1#*=}"; shift ;;
    --manager-url)    MANAGER_URL="$2"; shift 2 ;;
    --manager-url=*)  MANAGER_URL="${1#*=}"; shift ;;
    --skip-download)  SKIP_DOWNLOAD="yes"; shift ;;
    -y|--yes)         NONINTERACTIVE="1"; shift ;;
    -h|--help)        usage; exit 0 ;;
    *) err "unknown option: $1" ;;
  esac
done

[ "$(id -u)" -eq 0 ] || err "this script must be run as root"

# ---------------------------------------------------------------------------
# Prompt for values interactively when appropriate
# ---------------------------------------------------------------------------
ask() { # $1=prompt $2=default -> echoes answer
  if [ -n "$NONINTERACTIVE" ]; then
    echo "$2"
  elif [ -t 0 ]; then
    read -r -p "$1 [$2]: " ans
    echo "${ans:-$2}"
  else
    echo "$2"
  fi
}

if [ -z "$WEB_PASSWORD" ]; then
  WEB_PASSWORD="$(random_password)"
fi
if [ -z "$TOKEN" ]; then
  TOKEN="$(random_password)"
fi
AUTOSTART="$(ask "Install and enable boot service (yes/no)" "$AUTOSTART")"

# ---------------------------------------------------------------------------
# Detect platform + prepare tools
# ---------------------------------------------------------------------------
detect_downloader
ARCH="$(detect_arch)"
OS="linux"
VERSION_NO_V="${FRP_VERSION#v}"
ASSET="frp_${VERSION_NO_V}_${OS}_${ARCH}.tar.gz"
BASE_URL="https://github.com/fatedier/frp/releases/download/${FRP_VERSION}/${ASSET}"

info "installing to $INSTALL_DIR"
info "platform: ${OS}_${ARCH}  frp version: ${FRP_VERSION}"

# ---------------------------------------------------------------------------
# Create install directory
# ---------------------------------------------------------------------------
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
# Write frp configs (minimal; everything else is editable in the web UI)
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

# ---------------------------------------------------------------------------
# Write frp-manager config
# ---------------------------------------------------------------------------
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
# Install systemd service
# ---------------------------------------------------------------------------
SERVICE_NAME="frp-manager"
if have systemctl; then
  cat > /etc/systemd/system/${SERVICE_NAME}.service <<EOF
[Unit]
Description=frp-manager - web management for frp
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=${INSTALL_DIR}/bin/frp-manager -c ${INSTALL_DIR}/frp-manager.toml
WorkingDirectory=${INSTALL_DIR}
Restart=on-failure
RestartSec=3
LimitNOFILE=1048576

[Install]
WantedBy=multi-user.target
EOF
  systemctl daemon-reload
  if [ "$AUTOSTART" = "yes" ]; then
    systemctl enable "$SERVICE_NAME" >/dev/null 2>&1 || true
  fi
  systemctl restart "$SERVICE_NAME"
  info "systemd service '${SERVICE_NAME}' installed and started"
else
  # Fallback: just start in background and warn.
  info "systemd not found; starting frp-manager in background"
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
echo "  Service:    ${SERVICE_NAME} (autostart=${AUTOSTART})"
if [ "$AUTOSTART" = "yes" ]; then
  echo "  Boot autostart is enabled."
else
  echo "  Boot autostart is disabled; start manually with: systemctl start ${SERVICE_NAME}"
fi
