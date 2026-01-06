#!/bin/bash

# Check if fs-ingest-daemon is running
if ! pgrep -x "fsd" > /dev/null; then
    echo "Error: 'fsd' service is NOT running."
    echo "Please start the daemon first: ./fsd start (or ./fsd run)"
    exit 1
fi

# Configuration
ROOT_DIR=${1:-"./data"}
NUM_CAMS=${2:-5}
FILES_PER_CAM=${3:-20}
DELAY=${4:-0} # Set to 0 for max speed (stress test)

echo "------------------------------------------------"
echo "FS Ingest Daemon - Stress Test Generator"
echo "------------------------------------------------"
echo "Target: $ROOT_DIR"
echo "Cameras: $NUM_CAMS"
echo "Files/Cam: $FILES_PER_CAM"
echo "Delay: ${DELAY}s"
echo "------------------------------------------------"

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
        # Unique filename
        FILENAME="img_$(date +%s%N)_$j.jpg"
        FILE_PATH="$CAM_DIR/$FILENAME"
        
        # Create file with random size between 1KB and 10KB
        SIZE=$(( ( RANDOM % 9000 ) + 1000 ))
        head -c $SIZE /dev/urandom > "$FILE_PATH"

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
