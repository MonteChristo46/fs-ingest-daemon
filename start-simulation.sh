#!/bin/bash

# Configuration
DATASET="${DATASET:-visa}"

if [ "$DATASET" == "mvtec" ]; then
    SOURCE_DIR="./test-data/mvtec_anomaly_detection"
    # Default multi-category simulation
    DEFAULT_CATEGORY_CONFIGS=(
        #"wood:0.5s"
        #"metal_nut:1s"
        #"bottle:1s"
        #"cable:1s"
        #"capsule:1s"
        #"hazelnut:1s"
        #"zipper:10s"
        #"leather:10s"
        #"screw:10s"
        #"tile:30s"
        #"toothbrush:30s"
        #"transistor:30s"
        #"grid:60s"
    )
elif [ "$DATASET" == "visa" ]; then
    SOURCE_DIR="./test-data/visa_converted"
    # Automatically transform VisA if not already done
    if [ ! -d "$SOURCE_DIR" ]; then
        echo "VisA converted directory not found. Running transformation..."
        ./scripts/transform-visa.sh || { echo "Transformation failed"; exit 1; }
    fi
    # Default multi-category simulation for VisA
    DEFAULT_CATEGORY_CONFIGS=(
        "candle:2s"
        "capsules:1s"
        #"cashew:0.5s"
        #"chewinggum:1s"
        #"fryum:1s"
        #"macaroni1:1s"
        "macaroni2:1s"
        #"pcb1:1s"
        #"pcb2:1s"
        #"pcb3:0.5s"
        #"pcb4:0.5s"
        "pipe_fryum:0.5s"
    )
else
    echo "Unknown dataset: $DATASET. Use 'mvtec' or 'visa'."
    exit 1
fi

TARGET_DIR="$HOME/glitch-hunt/input"
DB_PATH="$HOME/glitch-hunt/hunt.db"
LOG_FILE="./simulation.log"

DEFECT_RATE="0.01"
JITTER="0.2"
NESTED="true"

# Example: Run multiple categories with varying rates
# If argument is provided, just run that one category with a default rate
if [ -n "$1" ]; then
    CATEGORY_CONFIGS=("$1:1s")
else
    CATEGORY_CONFIGS=("${DEFAULT_CATEGORY_CONFIGS[@]}")
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
echo "Source:      $SOURCE_DIR"
echo "Target:      $TARGET_DIR"
echo "Defect Rate: $DEFECT_RATE"
echo "Jitter:      $JITTER"
echo "------------------------------------------------"

for config in "${CATEGORY_CONFIGS[@]}"; do
    category="${config%%:*}"
    rate="${config#*:}"
    
    echo "Starting simulation for category '$category' at rate '$rate'"
    ./hunt simulate \
        --source "$SOURCE_DIR" \
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



