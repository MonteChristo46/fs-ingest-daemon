#!/bin/bash

# Exit on error
set -e

# Output directory for compiled binaries
OUT_DIR="build"
mkdir -p "$OUT_DIR"

echo "Building FS Ingest Daemon for multiple platforms..."

# Read Version and Banner
VERSION=$(cat assets/VERSION)
# Read banner, escape backslashes for sed, and escape newlines
BANNER=$(cat assets/banner.txt | sed 's/\\/\\\\/g' | sed 's/$/\\n/' | tr -d '\n')
# PowerShell banner needs \033 replaced with $ESC, then escape backslashes for sed
PS_BANNER=$(cat assets/banner.txt | sed 's/\\033/$ESC/g' | sed 's/\\/\\\\/g' | sed 's/$/`n/' | tr -d '\n')

echo "Generating install scripts..."

# Generate install.sh
sed -e "s/{{VERSION}}/$VERSION/g" \
    -e "s|{{BANNER}}|$BANNER|g" \
    scripts/install.sh.tpl > "scripts/install.sh"
chmod +x "scripts/install.sh"

# Generate install.ps1
sed -e "s/{{VERSION}}/$VERSION/g" \
    -e "s|{{BANNER}}|$PS_BANNER|g" \
    scripts/install.ps1.tpl > "scripts/install.ps1"

# Define targets: "OS/ARCH"
TARGETS=(
    "darwin/amd64"
    "darwin/arm64"
    "linux/amd64"
    "linux/arm64"
    "windows/amd64"
)

# modernc.org/sqlite does not require CGO, so we can statically compile!
export CGO_ENABLED=0

for target in "${TARGETS[@]}"; do
    # Split OS and ARCH
    GOOS=${target%/*}
    GOARCH=${target#*/}
    
    # Define output file name
    OUTPUT_NAME="hunt-${GOOS}-${GOARCH}"
    if [ "$GOOS" = "windows" ]; then
        OUTPUT_NAME+=".exe"
    fi
    
    echo " -> Building $GOOS/$GOARCH..."
    
    # Build
    GOOS=$GOOS GOARCH=$GOARCH go build -ldflags="-s -w" -o "$OUT_DIR/$OUTPUT_NAME" ./cmd/hunt
    
    if [ $? -ne 0 ]; then
        echo "[ERROR] Failed to build $GOOS/$GOARCH"
        exit 1
    fi
done

echo ""
echo "[SUCCESS] All builds completed successfully!"
echo "Binaries are located in the '$OUT_DIR' directory:"
ls -lh "$OUT_DIR"
