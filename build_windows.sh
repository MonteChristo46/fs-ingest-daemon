#!/bin/bash

# Define output name
OUTPUT_NAME="fsd.exe"

echo "Building for Windows (amd64)..."

# Run cross-compilation
env GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o $OUTPUT_NAME cmd/fsd/main.go

if [ $? -eq 0 ]; then
    echo "✅ Success! Created $OUTPUT_NAME"
    echo "Transfer this file to your Windows machine and run:"
    echo "  .\fsd.exe install"
else
    echo "❌ Build failed."
    exit 1
fi
