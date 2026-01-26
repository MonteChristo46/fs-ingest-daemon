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

# Check for root/sudo
if [ "$(id -u)" -ne 0 ]; then
    echo "This script requires root privileges to install to $INSTALL_DIR and set up the service."
    echo "Please run with sudo."
    exit 1
fi

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
echo "Symlinking to $SYMLINK_PATH..."
rm -f "$SYMLINK_PATH"
ln -s "$TARGET" "$SYMLINK_PATH"

# Run Installer
echo "Running fsd install..."
"$TARGET" install

echo ""
echo "✅ Installation wrapper complete."
echo "You can now use 'fsd' from anywhere."
