#!/bin/sh

INSTALL_DIR="/opt/fsd"
SYMLINK_PATH="/usr/local/bin/fsd"

if [ "$(id -u)" -ne 0 ]; then
    echo "Please run with sudo."
    exit 1
fi

echo "Stopping and uninstalling service..."
if command -v fsd >/dev/null 2>&1; then
    fsd uninstall || echo "Service uninstall warning (might not be running)"
fi

echo "Removing symlink..."
rm -f "$SYMLINK_PATH"

echo "Removing installation directory..."
rm -rf "$INSTALL_DIR"

echo "✅ Cleanup complete. 'fsd' has been uninstalled."
