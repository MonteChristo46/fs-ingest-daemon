#!/bin/sh
set -e

VERSION="0.8.1-alpha"

# Print colored banner
printf "\033[38;2;156;39;176m   __                __ \033[0m
\033[38;2;117;66;166m  / /_  __  ______  / /_\033[0m
\033[38;2;78;93;156m / __ \/ / / / __ \/ __/\033[0m
\033[38;2;39;120;146m/ / / / /_/ / / / / /_  \033[0m
\033[38;2;0;150;136m/_/ /_/\__,_/_/ /_/\__/ \033[0m
"
printf " \033[38;2;200;200;200mDAEMON INSTALLER | v%s\033[0m\n\n" "$VERSION"

# Configuration
INSTALL_DIR="/opt/hunt"
BIN_NAME="hunt"
SYMLINK_PATH="/usr/local/bin/hunt"

# Detect OS and Arch
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

if [ "$ARCH" = "x86_64" ]; then
    ARCH="amd64"
elif [ "$ARCH" = "aarch64" ] || [ "$ARCH" = "arm64" ]; then
    ARCH="arm64"
else
    echo "[SYSTEM] ❌ Unsupported architecture: $ARCH"
    exit 1
fi

DOWNLOAD_URL="https://github.com/MonteChristo46/fs-ingest-daemon/raw/main/build/hunt-${OS}-${ARCH}"

echo "[SYSTEM] Checking system requirements... [OK] ($OS / $ARCH)"

# Require Root Privilege
if [ "$(id -u)" -ne 0 ]; then
    echo "[SYSTEM] ❌ Error: This script must be run as root. Please use sudo."
    exit 1
fi
echo "[SYSTEM] Running as ROOT"

# Prepare Directory
echo "[CONFIG] Target Directory: $INSTALL_DIR"
mkdir -p "$INSTALL_DIR"

# Download Binary
TARGET="${INSTALL_DIR}/${BIN_NAME}"

echo "[STATUS] Downloading Hunt daemon..."
curl -fsSL "$DOWNLOAD_URL" -o "$TARGET"

if [ ! -f "$TARGET" ]; then
    echo "[ERROR] Download failed. File not found at $TARGET"
    exit 1
fi

chmod +x "$TARGET"

# macOS (Darwin) Fix: Apply ad-hoc signature to prevent "Killed: 9"
if [ "$(uname -s)" = "Darwin" ]; then
    echo "[STATUS] Applying macOS security fix (ad-hoc signing)... [OK]"
    # Remove quarantine attribute if present
    xattr -d com.apple.quarantine "$TARGET" 2>/dev/null || true
    # Force ad-hoc signing
    codesign --force --deep -s - "$TARGET" >/dev/null 2>&1
fi

# Symlink to PATH
echo "[CONFIG] Creating symlink at $SYMLINK_PATH..."
mkdir -p /usr/local/bin
rm -f "$SYMLINK_PATH"
ln -s "$TARGET" "$SYMLINK_PATH"

# Run Installer
echo "[STATUS] Running hunt install..."
echo "--------------------------------------------------"
# We redirect stdin from /dev/tty to ensure interactive prompts work
# even when the script is piped via curl
if [ -t 0 ]; then
    "$TARGET" install
else
    # If not running in a terminal (e.g. piped), try to force TTY
    if [ -c /dev/tty ]; then
        "$TARGET" install < /dev/tty
    else
        echo "> ⚠️  Warning: No TTY detected. Running in non-interactive mode."
        "$TARGET" install
    fi
fi

echo "--------------------------------------------------"
echo "[SUCCESS] Installation wrapper complete. You can now use 'hunt'."