#!/bin/bash

# Configuration
ROOT_DIR=${1:-"$HOME/fsd/data"}
TEST_DATA_DIR=${2:-"./test-data"}
NUM_CAMS=${3:-5}
FILES_PER_CAM=${4:-20}
DELAY=${5:-0} # Set to 0 for max speed (stress test)
MODE="" 

# Parse optional arguments
while [[ "$#" -gt 0 ]]; do
    case $1 in
        -m|--mode) MODE="$2"; shift ;;
        *) ;;
    esac
    shift
done

# Interactive Menu if Mode not set
if [ -z "$MODE" ]; then
    echo "Select Test Mode:"
    echo "1) Strict (Images + JSON Sidecars)"
    echo "2) None   (Images Only)"
    read -p "Enter choice [1]: " CHOICE
    case $CHOICE in
        2) MODE="none" ;;
        *) MODE="strict" ;;
    esac
fi

# Check if fs-ingest-daemon is running
if ! pgrep -x "fsd" > /dev/null; then
    echo "Error: 'fsd' service is NOT running."
    echo "Please start the daemon first: ./fsd start (or ./fsd run)"
    exit 1
fi

echo "------------------------------------------------"
echo "FS Ingest Daemon - Stress Test Generator"
echo "------------------------------------------------"
echo "Target: $ROOT_DIR"
echo "Source: $TEST_DATA_DIR"
echo "Cameras: $NUM_CAMS"
echo "Files/Cam: $FILES_PER_CAM"
echo "Delay: ${DELAY}s"
echo "Mode: $MODE"
echo "------------------------------------------------"

# Check source images
if [ ! -d "$TEST_DATA_DIR" ]; then
    echo "Error: Test data directory '$TEST_DATA_DIR' does not exist."
    exit 1
fi

# Load images from mvtec dataset recursively if available
IMAGES=()
while IFS=  read -r -d $'\0'; do
    IMAGES+=("$REPLY")
done < <(find "$TEST_DATA_DIR/mvtec_anomaly_detection" -type f \( -name "*.png" -o -name "*.jpg" -o -name "*.jpeg" \) -print0 2>/dev/null)

# Fallback to simple images folder if mvtec not found
if [ ${#IMAGES[@]} -eq 0 ]; then
    IMAGE_DIR="$TEST_DATA_DIR/images"
    if [ -d "$IMAGE_DIR" ]; then
        shopt -s nullglob
        IMAGES=("$IMAGE_DIR"/*.png "$IMAGE_DIR"/*.jpg "$IMAGE_DIR"/*.jpeg)
        shopt -u nullglob
    fi
fi

NUM_IMAGES=${#IMAGES[@]}

if [ "$NUM_IMAGES" -eq 0 ]; then
    echo "Error: No image files found in '$TEST_DATA_DIR' (checked mvtec_anomaly_detection and images/)."
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
        # Unique filename (keep extension from source)
        
        # Pick random image
        RAND_INDEX=$(( RANDOM % NUM_IMAGES ))
        SOURCE_FILE="${IMAGES[$RAND_INDEX]}"
        EXTENSION="${SOURCE_FILE##*.}"
        
        FILENAME="img_$(date +%s%N)_$j.$EXTENSION"
        FILE_PATH="$CAM_DIR/$FILENAME"
        
        # Copy file
        cp "$SOURCE_FILE" "$FILE_PATH"

        # Create Context File (Only in strict mode)
        if [ "$MODE" == "strict" ]; then
            # Randomly select context_1.json or context_2.json
            RAND_CTX=$(( RANDOM % 2 + 1 ))
            CTX_SOURCE="$TEST_DATA_DIR/context_${RAND_CTX}.json"
            
            # Copy context file if it exists, appending .json (double extension)
            if [ -f "$CTX_SOURCE" ]; then
                JSON_PATH="${FILE_PATH}.json"
                mkdir -p "$(dirname "$JSON_PATH")" # Ensure directory exists
                cp "$CTX_SOURCE" "$JSON_PATH"
            fi
        fi

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