#!/bin/sh
set -e

VERSION="0.9.4-alpha"

# Print colored banner
printf "\033[38;2;156;39;176m██╗  ██╗██╗   ██╗███╗   ██╗████████╗    ██████╗  █████╗ ███████╗███╗   ███╗ ██████╗ ███╗   ██╗\033[0m
\033[38;2;125;61;168m██║  ██║██║   ██║████╗  ██║╚══██╔══╝    ██╔══██╗██╔══██╗██╔════╝████╗ ████║██╔═══██╗████╗  ██║\033[0m
\033[38;2;94;83;160m███████║██║   ██║██╔██╗ ██║   ██║       ██║  ██║███████║█████╗  ██╔████╔██║██║   ██║██╔██╗ ██║\033[0m
\033[38;2;63;105;152m██╔══██║██║   ██║██║╚██╗██║   ██║       ██║  ██║██╔══██║██╔══╝  ██║╚██╔╝██║██║   ██║██║╚██╗██║\033[0m
\033[38;2;32;127;144m██║  ██║╚██████╔╝██║ ╚████║   ██║       ██████╔╝██║  ██║███████╗██║ ╚═╝ ██║╚██████╔╝██║ ╚████║\033[0m
\033[38;2;0;150;136m╚═╝  ╚═╝ ╚═════╝ ╚═╝  ╚═══╝   ╚═╝       ╚═════╝ ╚═╝  ╚═╝╚══════╝╚═╝     ╚═╝ ╚═════╝ ╚═╝  ╚═══╝\033[0m
"
printf " \033[38;2;200;200;200mDAEMON INSTALLER | v%s\033[0m\n" "$VERSION"

echo ""
echo "╔══════════════════════════════════════════════════════╗"
echo "║  Glitch Hunt — Edge Daemon Installer                ║"
echo "║                                                      ║"
echo "║  What this will do:                                 ║"
echo "║  • Download the daemon binary                       ║"
echo "║  • Install it as a background service               ║"
echo "║  • Guide you through setup (press Enter for default)║"
echo "╚══════════════════════════════════════════════════════╝"
echo ""

# Confirm before proceeding (handle piped stdin)
if [ -t 0 ]; then
    printf "Press [Enter] to continue or Ctrl+C to cancel... "
    read -r _
elif [ -c /dev/tty ]; then
    printf "Press [Enter] to continue or Ctrl+C to cancel... "
    read -r _ < /dev/tty
fi
echo ""

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

echo "  ✓ System check passed  ($OS / $ARCH)"

# Require Root Privilege
if [ "$(id -u)" -ne 0 ]; then
    echo "  ✖ Requires root privileges. Re-run with sudo."
    exit 1
fi
echo "  ✓ Running with administrator privileges"

# Prepare Directory
echo "  ⚙ Install directory: $INSTALL_DIR"
mkdir -p "$INSTALL_DIR"

# Download Binary
TARGET="${INSTALL_DIR}/${BIN_NAME}"

echo "  ↓ Downloading daemon..."
curl -fL --progress-bar "$DOWNLOAD_URL" -o "$TARGET"

if [ ! -f "$TARGET" ]; then
    echo "  ✖ Download failed. File not found at $TARGET"
    exit 1
fi

chmod +x "$TARGET"

# macOS (Darwin) Fix: Apply ad-hoc signature to prevent "Killed: 9"
if [ "$(uname -s)" = "Darwin" ]; then
    echo "  🔏 Applying macOS security fix..."
    # Remove quarantine attribute if present
    xattr -d com.apple.quarantine "$TARGET" 2>/dev/null || true
    # Force ad-hoc signing
    codesign --force --deep -s - "$TARGET" >/dev/null 2>&1
fi

# Symlink to PATH
echo "  🔗 Creating symlink at $SYMLINK_PATH"
mkdir -p /usr/local/bin
rm -f "$SYMLINK_PATH"
ln -s "$TARGET" "$SYMLINK_PATH"

# Run Installer
echo ""
echo "  ── Configuration ──"
echo "  (Press Enter to accept each [default])"
echo ""
if [ -t 0 ]; then
    "$TARGET" install
else
    if [ -c /dev/tty ]; then
        "$TARGET" install < /dev/tty
    else
        echo "  ⚠ No TTY detected. Running in non-interactive mode."
        "$TARGET" install
    fi
fi

echo ""
echo "  Done. Use 'hunt' to manage the daemon."