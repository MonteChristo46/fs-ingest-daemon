#!/bin/sh
set -e

# Configuration
# URL to the raw binary on GitHub
DOWNLOAD_URL="https://github.com/MonteChristo46/fs-ingest-daemon/raw/main/fsd"
INSTALL_DIR="/opt/fsd"
BIN_NAME="fsd"
SYMLINK_PATH="/usr/local/bin/fsd"

# Detect OS and Arch
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

if [ "$ARCH" = "x86_64" ]; then
    ARCH="amd64"
elif [ "$ARCH" = "aarch64" ] || [ "$ARCH" = "arm64" ]; then
    ARCH="arm64"
else
    echo "Unsupported architecture: $ARCH"
    exit 1
fi

echo "Detected: $OS / $ARCH"

# Detect Privilege Level
IS_ROOT=0
if [ "$(id -u)" -eq 0 ]; then
    IS_ROOT=1
fi

# Define Paths based on Privilege
if [ "$IS_ROOT" -eq 1 ]; then
    echo "Running as ROOT (System Install)"
    INSTALL_DIR="/opt/fsd"
    SYMLINK_DIR="/usr/local/bin"
else
    echo "Running as USER (User Install)"
    INSTALL_DIR="$HOME/fsd"
    # Try standard user bin locations
    if [ -d "$HOME/.local/bin" ]; then
        SYMLINK_DIR="$HOME/.local/bin"
    elif [ -d "$HOME/bin" ]; then
        SYMLINK_DIR="$HOME/bin"
    else
        # No standard user bin found, we might need to rely on the install dir being in PATH
        SYMLINK_DIR=""
    fi
fi

SYMLINK_PATH="${SYMLINK_DIR}/${BIN_NAME}"

# Prepare Directory
echo "Creating install directory: $INSTALL_DIR"
mkdir -p "$INSTALL_DIR"

# Download Binary
TARGET="${INSTALL_DIR}/${BIN_NAME}"

echo "Downloading ${DOWNLOAD_URL}..."
curl -fsSL "$DOWNLOAD_URL" -o "$TARGET"

if [ ! -f "$TARGET" ]; then
    echo "❌ Error: Download failed. File not found at $TARGET"
    exit 1
fi

chmod +x "$TARGET"

# Symlink to PATH
if [ -n "$SYMLINK_DIR" ]; then
    echo "Symlinking to $SYMLINK_PATH..."
    # Check write permissions for symlink dir
    if [ -w "$SYMLINK_DIR" ]; then
        rm -f "$SYMLINK_PATH"
        ln -s "$TARGET" "$SYMLINK_PATH"
    else
        echo "⚠️  Warning: Cannot write to $SYMLINK_DIR. Skipping symlink."
        echo "   Please add $INSTALL_DIR to your PATH manually."
    fi
else
    echo "ℹ️  No standard bin directory found ($HOME/.local/bin or $HOME/bin)."
    echo "   Please add $INSTALL_DIR to your PATH to run 'fsd' from anywhere."
    echo "   Example: export PATH=\"\$PATH:$INSTALL_DIR\""
fi

# Run Installer
echo "Running fsd install..."
"$TARGET" install

echo ""
echo "✅ Installation wrapper complete."
echo "You can now use 'fsd' from anywhere."
