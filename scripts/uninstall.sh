#!/bin/sh

# Detect Privilege Level
IS_ROOT=0
if [ "$(id -u)" -eq 0 ]; then
    IS_ROOT=1
fi

if [ "$IS_ROOT" -eq 1 ]; then
    INSTALL_DIR="/opt/fsd"
    SYMLINK_PATH="/usr/local/bin/fsd"
    echo "Uninstalling System Service (ROOT)..."
else
    INSTALL_DIR="$HOME/fsd"
    # Try to guess symlink location
    if [ -f "$HOME/.local/bin/fsd" ]; then
        SYMLINK_PATH="$HOME/.local/bin/fsd"
    elif [ -f "$HOME/bin/fsd" ]; then
        SYMLINK_PATH="$HOME/bin/fsd"
    else
        SYMLINK_PATH=""
    fi
    echo "Uninstalling User Service..."
fi

echo "Stopping and uninstalling service..."
# If the binary exists, use it to uninstall the service first
if [ -x "$INSTALL_DIR/fsd" ]; then
    "$INSTALL_DIR/fsd" uninstall || echo "Warning: Service uninstall returned error (might not be running)"
elif command -v fsd >/dev/null 2>&1; then
    fsd uninstall || echo "Warning: Service uninstall returned error"
fi

if [ -n "$SYMLINK_PATH" ] && [ -L "$SYMLINK_PATH" ]; then
    echo "Removing symlink: $SYMLINK_PATH"
    rm -f "$SYMLINK_PATH"
fi

echo "Removing installation directory: $INSTALL_DIR"
rm -rf "$INSTALL_DIR"

echo "✅ Cleanup complete."
