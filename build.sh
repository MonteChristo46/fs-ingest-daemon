#!/bin/bash

# Exit on error
set -e

# Output directory for compiled binaries
OUT_DIR="build"
mkdir -p "$OUT_DIR"

echo "Building FS Ingest Daemon for multiple platforms..."

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
