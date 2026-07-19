#!/usr/bin/env bash
set -euo pipefail

# ============================================================
# Generic Game Scoreboard Backend — Install Script
# ============================================================
# Downloads the latest release from GitHub and sets up a
# hardened systemd service.
# ============================================================

REPO="furkanbaytekin/generic-game-scoreboard-backend"
BIN_NAME="scoreboard"
INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/scoreboard"
DATA_DIR="/var/lib/scoreboard"
SERVICE_FILE="/etc/systemd/system/${BIN_NAME}.service"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log()  { echo -e "${GREEN}[+]${NC} $*"; }
warn() { echo -e "${YELLOW}[!]${NC} $*"; }
err()  { echo -e "${RED}[x]${NC} $*"; exit 1; }

# --- Pre-flight checks ---
[[ "$(uname -s)" == "Linux" ]] || err "This install script only supports Linux."

if [[ "$EUID" -ne 0 ]]; then
    err "Please run as root: sudo ./install.sh"
fi

# --- Detect architecture ---
ARCH=$(uname -m)
case "$ARCH" in
    x86_64)  GOARCH="amd64" ;;
    aarch64) GOARCH="arm64" ;;
    *)       err "Unsupported architecture: $ARCH" ;;
esac
log "Detected architecture: $ARCH → $GOARCH"

# --- Download latest release ---
log "Fetching latest release from GitHub..."
API_URL="https://api.github.com/repos/${REPO}/releases/latest"
DOWNLOAD_URL=$(curl -sL "$API_URL" | grep -o "https://.*/${BIN_NAME}-linux-${GOARCH}\"" | tr -d '"' | head -1)

if [[ -z "$DOWNLOAD_URL" ]]; then
    err "Could not find a release asset for linux-${GOARCH}. Check: https://github.com/${REPO}/releases"
fi

log "Downloading: $DOWNLOAD_URL"
TMPFILE=$(mktemp)
curl -sL -o "$TMPFILE" "$DOWNLOAD_URL"

# --- Install binary ---
log "Installing to ${INSTALL_DIR}/${BIN_NAME}"
install -m 755 "$TMPFILE" "${INSTALL_DIR}/${BIN_NAME}"
rm -f "$TMPFILE"

# --- Config ---
if [[ ! -f "${CONFIG_DIR}/.env" ]]; then
    log "Creating default config at ${CONFIG_DIR}/.env"
    mkdir -p "$CONFIG_DIR"
    cat > "${CONFIG_DIR}/.env" <<'EOF'
PORT=8080
DB_PATH=/var/lib/scoreboard/scores.db
HMAC_SECRET=change-me-to-a-random-secret
TITLE=Game Scoreboard
CACHE_TTL=60s
SCORE_WEIGHT_FLOOR=100
SCORE_WEIGHT_DAMAGE_DEALT=2
SCORE_WEIGHT_DAMAGE_TAKEN=1
SCORE_WEIGHT_REVIVE=50
SCORE_WEIGHT_QUEST=200
EOF
    warn "Config created. Edit ${CONFIG_DIR}/.env and set HMAC_SECRET!"
else
    log "Config already exists at ${CONFIG_DIR}/.env — skipping"
fi

# --- Copy assets (if bundled) ---
# Asset files (logo.png, favicon.ico) should be placed in the same directory
# as the binary or served from a different location. The binary looks for
# ./assets/ relative to its working directory.

# --- Install systemd service ---
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
if [[ -f "${SCRIPT_DIR}/scoreboard.service" ]]; then
    log "Installing systemd service from ${SCRIPT_DIR}/scoreboard.service"
    cp "${SCRIPT_DIR}/scoreboard.service" "$SERVICE_FILE"
elif [[ -f /tmp/scoreboard.service ]]; then
    log "Installing systemd service from /tmp/scoreboard.service"
    cp /tmp/scoreboard.service "$SERVICE_FILE"
else
    log "Downloading service file from GitHub..."
    curl -sL "https://raw.githubusercontent.com/${REPO}/main/scoreboard.service" -o "$SERVICE_FILE"
fi

# --- Enable and start ---
log "Reloading systemd..."
systemctl daemon-reload
systemctl enable "${BIN_NAME}.service"
systemctl restart "${BIN_NAME}.service"

# --- Status ---
echo ""
log "Installation complete!"
echo ""
echo "  Config:   ${CONFIG_DIR}/.env"
echo "  Data:     ${DATA_DIR}/"
echo "  Binary:   ${INSTALL_DIR}/${BIN_NAME}"
echo ""
echo "  Status:   sudo systemctl status ${BIN_NAME}"
echo "  Logs:     sudo journalctl -u ${BIN_NAME} -f"
echo ""
systemctl --no-pager status "${BIN_NAME}.service" || true
