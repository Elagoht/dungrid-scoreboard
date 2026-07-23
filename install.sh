#!/usr/bin/env bash
set -euo pipefail

# ============================================================
# Generic Game Scoreboard Backend — Install Script
# ============================================================
# Downloads the latest release from GitHub and sets up a
# hardened systemd service.
# ============================================================

REPO="Elagoht/dungrid-scoreboard"
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

# --- Sanitizers ---
sanitize_port() {
    local val="$1"
    [[ "$val" =~ ^[0-9]+$ ]] && [[ "$val" -ge 1 ]] && [[ "$val" -le 65535 ]] && echo "$val"
}
sanitize_int() {
    local val="$1"
    [[ "$val" =~ ^-?[0-9]+$ ]] && echo "$val"
}
sanitize_duration() {
    local val="$1"
    # Accept strings like 30s, 5m, 1h, 300ms
    [[ "$val" =~ ^[0-9]+(ms|s|m|h)$ ]] && echo "$val"
}
sanitize_title() {
    local val="$1"
    # Strip anything that isn't a printable character — max 128 chars
    val=$(echo "$val" | tr -cd '[:print:]' | head -c 128)
    echo "$val"
}
sanitize_secret() {
    local val="$1"
    # Allow alphanumeric, dash, underscore — max 256 chars
    [[ "$val" =~ ^[a-zA-Z0-9_/-]+$ ]] && echo "$val"
}
sanitize_password() {
    local val="$1"
    # Strip newlines, carriage returns, and null bytes — max 128 chars
    val=$(echo "$val" | tr -d '\n\r\0' | head -c 128)
    echo "$val"
}

# --- Config ---
if [[ ! -f "${CONFIG_DIR}/.env" ]]; then
    echo ""
    echo "============================================"
    echo "  Scoreboard Configuration"
    echo "============================================"
    echo ""
    echo "Press Enter to accept the default value shown in brackets."
    echo ""
    mkdir -p "$CONFIG_DIR"

    # Generate random HMAC secret
    RANDOM_SECRET=$(openssl rand -hex 32 2>/dev/null || (head -c 32 /dev/urandom | xxd -p) 2>/dev/null || echo "change-me-$(date +%s)")

    # --- Title ---
    while true; do
        printf "Scoreboard title [Game Scoreboard]: "
        read -r TITLE_INPUT
        TITLE_INPUT=${TITLE_INPUT:-"Game Scoreboard"}
        TITLE_INPUT=$(sanitize_title "$TITLE_INPUT")
        [[ -n "$TITLE_INPUT" ]] && break
        warn "Title cannot be empty."
    done

    # --- Port ---
    while true; do
        printf "Port [8080]: "
        read -r PORT_INPUT
        PORT_INPUT=${PORT_INPUT:-"8080"}
        PORT_INPUT=$(sanitize_port "$PORT_INPUT")
        [[ -n "$PORT_INPUT" ]] && break
        warn "Invalid port. Enter a number between 1 and 65535."
    done

    # --- HMAC Secret ---
    while true; do
        printf "HMAC secret [generated]: "
        read -r HMAC_INPUT
        HMAC_INPUT=${HMAC_INPUT:-"$RANDOM_SECRET"}
        HMAC_INPUT=$(sanitize_secret "$HMAC_INPUT")
        [[ -n "$HMAC_INPUT" ]] && break
        warn "Secret contains invalid characters. Use a-z, A-Z, 0-9, dash, underscore, slash."
    done

    # --- Admin Password ---
    echo ""
    echo "--- Admin Panel Password ---"
    echo "Set a password to access the feedback/ratings panel at /panel."
    echo "Leave empty to disable the admin panel."
    while true; do
        printf "Admin password [disabled]: "
        read -r ADMIN_PW_INPUT
        ADMIN_PW_INPUT=$(sanitize_password "$ADMIN_PW_INPUT")
        if [[ -z "$ADMIN_PW_INPUT" ]]; then
            log "Admin panel will be disabled."
            ADMIN_PW_FINAL=""
            break
        fi
        if [[ ${#ADMIN_PW_INPUT} -lt 4 ]]; then
            warn "Password must be at least 4 characters."
            continue
        fi
        # Hash the password using the installed binary
        HASHED=$( "${INSTALL_DIR}/${BIN_NAME}" -hash-password "$ADMIN_PW_INPUT" 2>/dev/null ) || true
        if [[ -n "$HASHED" ]]; then
            ADMIN_PW_FINAL="$HASHED"
            log "Password hashed with bcrypt."
        else
            ADMIN_PW_FINAL="$ADMIN_PW_INPUT"
            warn "Could not hash password (binary not found); stored as plaintext."
        fi
        break
    done

    # --- Cache TTL ---
    while true; do
        printf "Cache TTL (e.g. 30s, 5m, 1h) [60s]: "
        read -r CACHE_INPUT
        CACHE_INPUT=${CACHE_INPUT:-"60s"}
        CACHE_INPUT=$(sanitize_duration "$CACHE_INPUT")
        [[ -n "$CACHE_INPUT" ]] && break
        warn "Invalid duration. Use format like 30s, 5m, or 1h."
    done

    # --- Score Weights ---
    echo ""
    echo "--- Score Weight Multipliers ---"

    while true; do
        printf "  Floor weight [100]: "
        read -r FLOOR_INPUT
        FLOOR_INPUT=${FLOOR_INPUT:-"100"}
        FLOOR_INPUT=$(sanitize_int "$FLOOR_INPUT")
        [[ -n "$FLOOR_INPUT" ]] && break
        warn "Must be an integer."
    done

    while true; do
        printf "  Damage dealt weight [2]: "
        read -r DEALT_INPUT
        DEALT_INPUT=${DEALT_INPUT:-"2"}
        DEALT_INPUT=$(sanitize_int "$DEALT_INPUT")
        [[ -n "$DEALT_INPUT" ]] && break
        warn "Must be an integer."
    done

    while true; do
        printf "  Damage taken weight (deducted) [1]: "
        read -r TAKEN_INPUT
        TAKEN_INPUT=${TAKEN_INPUT:-"1"}
        TAKEN_INPUT=$(sanitize_int "$TAKEN_INPUT")
        [[ -n "$TAKEN_INPUT" ]] && break
        warn "Must be an integer."
    done

    while true; do
        printf "  Revive weight [50]: "
        read -r REVIVE_INPUT
        REVIVE_INPUT=${REVIVE_INPUT:-"50"}
        REVIVE_INPUT=$(sanitize_int "$REVIVE_INPUT")
        [[ -n "$REVIVE_INPUT" ]] && break
        warn "Must be an integer."
    done

    while true; do
        printf "  Quest weight [200]: "
        read -r QUEST_INPUT
        QUEST_INPUT=${QUEST_INPUT:-"200"}
        QUEST_INPUT=$(sanitize_int "$QUEST_INPUT")
        [[ -n "$QUEST_INPUT" ]] && break
        warn "Must be an integer."
    done

    cat > "${CONFIG_DIR}/.env" <<EOF
PORT=${PORT_INPUT}
DB_PATH=${DATA_DIR}/scores.db
HMAC_SECRET=${HMAC_INPUT}
TITLE=${TITLE_INPUT}
CACHE_TTL=${CACHE_INPUT}
SCORE_WEIGHT_FLOOR=${FLOOR_INPUT}
SCORE_WEIGHT_DAMAGE_DEALT=${DEALT_INPUT}
SCORE_WEIGHT_DAMAGE_TAKEN=${TAKEN_INPUT}
SCORE_WEIGHT_REVIVE=${REVIVE_INPUT}
SCORE_WEIGHT_QUEST=${QUEST_INPUT}
EOF
    # Append admin password separately (bcrypt hashes contain $ signs)
    if [[ -n "$ADMIN_PW_FINAL" ]]; then
        echo "ADMIN_PASSWORD=${ADMIN_PW_FINAL}" >> "${CONFIG_DIR}/.env"
    fi
    echo ""
    echo "-------------------------------------------"
    log "Config saved to ${CONFIG_DIR}/.env"
    echo "-------------------------------------------"
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
