#!/bin/bash

# Check if fs-ingest-daemon is running
if ! pgrep -x "fsd" > /dev/null; then
    echo "Error: 'fsd' service is NOT running."
    echo "Please start the daemon first: ./fsd start (or ./fsd run)"
    exit 1
fi

# Configuration
ROOT_DIR=${1:-"./fsd-watch"}
TEST_DATA_DIR=${2:-"./test-data"}
NUM_CAMS=${3:-5}
FILES_PER_CAM=${4:-20}
DELAY=${5:-0} # Set to 0 for max speed (stress test)

echo "------------------------------------------------"
echo "FS Ingest Daemon - Stress Test Generator"
echo "------------------------------------------------"
echo "Target: $ROOT_DIR"
echo "Source: $TEST_DATA_DIR"
echo "Cameras: $NUM_CAMS"
echo "Files/Cam: $FILES_PER_CAM"
echo "Delay: ${DELAY}s"
echo "------------------------------------------------"

# Check source images
if [ ! -d "$TEST_DATA_DIR" ]; then
    echo "Error: Test data directory '$TEST_DATA_DIR' does not exist."
    exit 1
fi

# Load images into array
shopt -s nullglob
IMAGES=("$TEST_DATA_DIR"/*)
shopt -u nullglob

NUM_IMAGES=${#IMAGES[@]}

if [ "$NUM_IMAGES" -eq 0 ]; then
    echo "Error: No files found in '$TEST_DATA_DIR'."
    exit 1
fi

echo "Found $NUM_IMAGES source images."

# Ensure root exists
mkdir -p "$ROOT_DIR"

# Loop to simulate multiple cameras/sensors
for ((i=1; i<=NUM_CAMS; i++)); do
    # Create a realistic nested structure: cam_id/year/month/day
    DATE_PATH=$(date "+%Y/%m/%d")
    CAM_DIR="$ROOT_DIR/cam_$i/$DATE_PATH"
    
    echo "Initializing $CAM_DIR ..."
    mkdir -p "$CAM_DIR"

    # Generate files
    for ((j=1; j<=FILES_PER_CAM; j++)); do
        # Unique filename (keep extension from source or default to .jpg?)
        # Let's keep it simple and just use .jpg or .png based on source if possible, 
        # but the request didn't specify dynamic extensions. 
        # However, to be safe, let's grab the extension from the source file.
        
        # Pick random image
        RAND_INDEX=$(( RANDOM % NUM_IMAGES ))
        SOURCE_FILE="${IMAGES[$RAND_INDEX]}"
        EXTENSION="${SOURCE_FILE##*.}"
        
        FILENAME="img_$(date +%s%N)_$j.$EXTENSION"
        FILE_PATH="$CAM_DIR/$FILENAME"
        
        # Copy file
        cp "$SOURCE_FILE" "$FILE_PATH"

        # Optional: Print progress every 10 files
        if (( j % 10 == 0 )); then
            echo "  [cam_$i] Created $j files..."
        fi

        # Optional Delay
        if [ "$DELAY" != "0" ]; then
             sleep $DELAY
        fi
    done
done

echo "------------------------------------------------"
echo "Stress test complete. generated $((NUM_CAMS * FILES_PER_CAM)) files."
echo "------------------------------------------------"
