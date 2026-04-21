#!/bin/bash

# This script transforms the VisA dataset into a structure compatible with the simulator.
# It copies files from test-data/VisA_20220922/ to test-data/visa_converted/
# restructuring them to use 'good' and 'bad' folders.

VISA_ROOT="./test-data/VisA_20220922"
CONVERTED_ROOT="./test-data/visa_converted"

if [ ! -d "$VISA_ROOT" ]; then
    echo "Error: VisA dataset not found at $VISA_ROOT"
    exit 1
fi

echo "Creating $CONVERTED_ROOT..."
mkdir -p "$CONVERTED_ROOT"

# Iterate over each category directory in VisA_ROOT
for category_path in "$VISA_ROOT"/*; do
    if [ -d "$category_path" ]; then
        category=$(basename "$category_path")
        
        # Skip internal VisA metadata folders
        if [ "$category" == "split_csv" ]; then
            continue
        fi
        
        echo "Processing category: $category"
        
        # Create target directories
        TARGET_GOOD="$CONVERTED_ROOT/$category/good"
        TARGET_BAD="$CONVERTED_ROOT/$category/bad"
        
        mkdir -p "$TARGET_GOOD"
        mkdir -p "$TARGET_BAD"
        
        # Source directories in VisA format
        SOURCE_NORMAL="$category_path/Data/Images/Normal"
        SOURCE_ANOMALY="$category_path/Data/Images/Anomaly"
        
        # Copy Normal (good) images
        if [ -d "$SOURCE_NORMAL" ]; then
            cp "$SOURCE_NORMAL"/*.JPG "$TARGET_GOOD/" 2>/dev/null
        else
            echo "  Warning: Normal images not found for $category"
        fi
        
        # Copy Anomaly (bad) images
        if [ -d "$SOURCE_ANOMALY" ]; then
            cp "$SOURCE_ANOMALY"/*.JPG "$TARGET_BAD/" 2>/dev/null
        else
            echo "  Warning: Anomaly images not found for $category"
        fi
    fi
done

echo "Transformation complete. Converted dataset is at $CONVERTED_ROOT"
