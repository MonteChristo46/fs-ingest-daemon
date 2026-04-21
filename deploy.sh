#!/bin/bash
set -e

# Configuration
VERSION_FILE="assets/VERSION"
BUILD_SCRIPT="./build.sh"

# Ensure we are in the right directory
if [ ! -f "$VERSION_FILE" ]; then
    echo "Error: $VERSION_FILE not found. Are you in the fs-ingest-daemon directory?"
    exit 1
fi

CURRENT_VERSION=$(cat "$VERSION_FILE")
echo "----------------------------------------"
echo "Current Version: $CURRENT_VERSION"
echo "----------------------------------------"

# Extract base version and suffix (e.g., 0.8.5-alpha -> BASE=0.8.5, SUFFIX=-alpha)
BASE_VERSION=$(echo "$CURRENT_VERSION" | cut -d'-' -f1)
SUFFIX=$(echo "$CURRENT_VERSION" | grep -o "\-.*" || echo "")

# Split base version into parts
IFS='.' read -r -a VERSION_PARTS <<< "$BASE_VERSION"
MAJOR="${VERSION_PARTS[0]:-0}"
MINOR="${VERSION_PARTS[1]:-0}"
PATCH="${VERSION_PARTS[2]:-0}"

# Propose new versions
MAJOR_VERSION="$((MAJOR + 1)).0.0$SUFFIX"
MINOR_VERSION="${MAJOR}.$((MINOR + 1)).0$SUFFIX"
PATCH_VERSION="${MAJOR}.${MINOR}.$((PATCH + 1))$SUFFIX"

echo "Select version bump type:"
echo "1) Major ($MAJOR_VERSION)"
echo "2) Minor ($MINOR_VERSION)"
echo "3) Patch ($PATCH_VERSION)"
echo "4) Custom"
echo "5) Cancel"

read -p "Option [1-5]: " OPTION

case $OPTION in
    1) NEW_VERSION="$MAJOR_VERSION" ;;
    2) NEW_VERSION="$MINOR_VERSION" ;;
    3) NEW_VERSION="$PATCH_VERSION" ;;
    4) read -p "Enter new version: " NEW_VERSION ;;
    5) echo "Cancelled."; exit 0 ;;
    *) echo "Invalid option."; exit 1 ;;
esac

echo ""
echo "Updating $VERSION_FILE to $NEW_VERSION..."
echo "$NEW_VERSION" > "$VERSION_FILE"

echo "Executing $BUILD_SCRIPT..."
bash "$BUILD_SCRIPT"

echo ""
echo "Committing and Pushing to Git..."
git add "$VERSION_FILE"
git add scripts/
git add build/
# Force add windows exe if present, as it is usually in .gitignore
git add -f build/*.exe 2>/dev/null || true

git commit -m "chore: bump version to $NEW_VERSION and update binaries"
git push

echo ""
echo "----------------------------------------"
echo "Successfully deployed version $NEW_VERSION"
echo "----------------------------------------"
