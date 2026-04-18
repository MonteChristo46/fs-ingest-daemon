#!/bin/sh

# Require Root Privilege
if [ "$(id -u)" -ne 0 ]; then
    echo "[SYSTEM] ❌ Error: This script must be run as root. Please use sudo."
    exit 1
fi
echo "[SYSTEM] Running as ROOT"

INSTALL_DIR="/opt/hunt"
SYMLINK_PATH="/usr/local/bin/hunt"
echo "Uninstalling System Service (ROOT)..."

echo "Stopping and uninstalling service..."
# If the binary exists, use it to uninstall the service first
if [ -x "$INSTALL_DIR/hunt" ]; then
    "$INSTALL_DIR/hunt" uninstall || echo "[WARN] Service uninstall returned error (might not be running)"
elif command -v hunt >/dev/null 2>&1; then
    hunt uninstall || echo "[WARN] Service uninstall returned error"
fi

if [ -n "$SYMLINK_PATH" ] && [ -L "$SYMLINK_PATH" ]; then
    echo "Removing symlink: $SYMLINK_PATH"
    rm -f "$SYMLINK_PATH"
fi

echo "Removing installation directory: $INSTALL_DIR"
rm -rf "$INSTALL_DIR"

echo "[SUCCESS] Cleanup complete."
