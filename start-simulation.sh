#!/bin/bash

# Configuration
DATASET="${DATASET:-mvtec}"

# Source directories by dataset
MVTEC_SOURCE="./test-data/mvtec_anomaly_detection"
VISA_SOURCE="./test-data/visa_converted"

# VisA auto-transformation
if [ ! -d "$VISA_SOURCE" ] && { [ "$DATASET" == "visa" ] || [ "$DATASET" == "both" ]; }; then
    echo "VisA converted directory not found. Running transformation..."
    ./scripts/transform-visa.sh || { echo "Transformation failed"; exit 1; }
fi

# Category configs by dataset
MVTEC_CONFIGS=(
    "wood:1s"
    #"metal_nut:1s"
    "bottle:5s"
    "cable:5s"
    "capsule:5s"
    #"hazelnut:1s"
    "zipper:20s"
    #"leather:10s"
    "screw:1s"
    "tile:3s"
    "toothbrush:30s"
    "transistor:60s"
    #"grid:60s"
)
VISA_CONFIGS=(
    "candle:12s"
    "capsules:120s"
    #"cashew:0.5s"
    #"chewinggum:1s"
    "fryum:120s"
    "macaroni1:60s"
    #"macaroni2:1s"
    "pcb1:30s"
    "pcb2:30s"
    "pcb3:20s"
    "pcb4:30s"
    #"pipe_fryum:0.5s"
)

# Build source/category pairs based on DATASET
declare -a CATEGORY_CONFIGS

if [ "$DATASET" == "mvtec" ]; then
    SOURCE_DIR="$MVTEC_SOURCE"
    CATEGORY_CONFIGS=("${MVTEC_CONFIGS[@]}")
elif [ "$DATASET" == "visa" ]; then
    SOURCE_DIR="$VISA_SOURCE"
    CATEGORY_CONFIGS=("${VISA_CONFIGS[@]}")
elif [ "$DATASET" == "both" ]; then
    # Prepend source directory to each config for disambiguation
    for cfg in "${MVTEC_CONFIGS[@]}"; do
        CATEGORY_CONFIGS+=("$MVTEC_SOURCE|$cfg")
    done
    for cfg in "${VISA_CONFIGS[@]}"; do
        CATEGORY_CONFIGS+=("$VISA_SOURCE|$cfg")
    done
else
    echo "Unknown dataset: $DATASET. Use 'mvtec', 'visa', or 'both'."
    exit 1
fi

TARGET_DIR="$HOME/glitch-hunt/input"
DB_PATH="$HOME/glitch-hunt/hunt.db"
LOG_FILE="./simulation.log"

DEFECT_RATE="0.02"
JITTER="0.2"
NESTED="false"

# Example: Run multiple categories with varying rates
# If argument is provided, just run that one category with a default rate
# Support format: "category:rate" or "source|category:rate"
if [ -n "$1" ]; then
    SINGLE_CONFIG="$1"
    # If it has a colon, use the user's rate; otherwise append ":1s"
    case "$SINGLE_CONFIG" in
        *\|*) ;;              # already has source prefix
        *:*)  ;;              # already has rate
        *)    SINGLE_CONFIG="$SINGLE_CONFIG:1s" ;;
    esac
    CATEGORY_CONFIGS=("$SINGLE_CONFIG")
fi

echo "--- CLEANUP ---"
echo "Cleaning up $TARGET_DIR..."
# Note: We only delete the contents, NOT the directory itself, so the daemon's fsnotify watcher doesn't break.
# We also do NOT delete DB_PATH as the background daemon holds an active connection to it.
rm -rf "$TARGET_DIR"/*
> "$LOG_FILE"
mkdir -p "$TARGET_DIR"

echo "--- BUILD ---"
go build -o hunt ./cmd/hunt/main.go || { echo "Build failed"; exit 1; }

# Array to keep track of simulator process PIDs
SIM_PIDS=()

# Function to kill background processes on exit
cleanup() {
    echo ""
    echo "--- STOPPING ---"
    for pid in "${SIM_PIDS[@]}"; do
        if kill -0 "$pid" 2>/dev/null; then
            echo "Killing Simulator (PID $pid)..."
            kill "$pid" 2>/dev/null
        fi
    done
    if [ -n "$TAIL_PID" ]; then
        kill "$TAIL_PID" 2>/dev/null
    fi
    exit
}
trap cleanup INT TERM

echo "--- STARTING SIMULATORS ---"
echo "Dataset:     $DATASET"
echo "Target:      $TARGET_DIR"
echo "Defect Rate: $DEFECT_RATE"
echo "Jitter:      $JITTER"
echo "------------------------------------------------"

for config in "${CATEGORY_CONFIGS[@]}"; do
    # Combined entries (both mode): "source|category:rate"
    # Simple entries: "category:rate"
    if [[ "$config" == *"|"* ]]; then
        src_dir="${config%%|*}"
        rest="${config#*|}"
    else
        src_dir="$SOURCE_DIR"
        rest="$config"
    fi
    category="${rest%%:*}"
    rate="${rest#*:}"
    
    echo "Starting simulation for category '$category' at rate '$rate' [src: $src_dir]"
    ./hunt simulate \
        --source "$src_dir" \
        --target "$TARGET_DIR" \
        --rate "$rate" \
        --categories "$category" \
        --defect-rate "$DEFECT_RATE" \
        --jitter "$JITTER" \
        --nested="$NESTED" \
        >> "$LOG_FILE" 2>&1 &
        
    SIM_PIDS+=($!)
done

echo "Simulators running with PIDs: ${SIM_PIDS[*]} (logs -> $LOG_FILE)"

# Tail the logs so we can see what's happening
# We tail both the simulation log and the daemon log (if it exists)
DAEMON_LOG="$HOME/glitch-hunt/hunt.log"
tail -f "$LOG_FILE" "$DAEMON_LOG" 2>/dev/null &
TAIL_PID=$!

# Wait for all simulators to finish (or until Ctrl+C)
for pid in "${SIM_PIDS[@]}"; do
    wait "$pid"
done

# Kill tail on exit
kill "$TAIL_PID" 2>/dev/null



