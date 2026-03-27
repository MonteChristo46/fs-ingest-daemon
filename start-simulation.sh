#!/bin/bash

# Configuration
SOURCE_DIR="./test-data/mvtec_anomaly_detection"
TARGET_DIR="$HOME/fsd/data"
DB_PATH="$HOME/fsd/fsd.db"
LOG_FILE="./simulation.log"

DEFECT_RATE="0.8"
JITTER="0.2"
NESTED="false"

# Example: Run multiple categories with varying rates
# If argument is provided, just run that one category with a default rate
if [ -n "$1" ]; then
    CATEGORY_CONFIGS=("$1:1s")
else
    # Default multi-category simulation
    CATEGORY_CONFIGS=(
        #"wood:1s"
        "metal_nut:3s"
        "bottle:2s"
        "cable:5s"
        "capsule:2s"
        "hazelnut:1s"
        "zipper: 3s"
    )
fi

echo "--- CLEANUP ---"
echo "Cleaning up $TARGET_DIR..."
# Note: We only delete the contents, NOT the directory itself, so the daemon's fsnotify watcher doesn't break.
# We also do NOT delete DB_PATH as the background daemon holds an active connection to it.
rm -rf "$TARGET_DIR"/*
> "$LOG_FILE"
mkdir -p "$TARGET_DIR"

echo "--- BUILD ---"
go build -o fsd ./cmd/fsd/main.go || { echo "Build failed"; exit 1; }

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
    ./fsd simulate \
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
DAEMON_LOG="$HOME/fsd/fsd.log"
tail -f "$LOG_FILE" "$DAEMON_LOG" 2>/dev/null &
TAIL_PID=$!

# Wait for all simulators to finish (or until Ctrl+C)
for pid in "${SIM_PIDS[@]}"; do
    wait "$pid"
done

# Kill tail on exit
kill "$TAIL_PID" 2>/dev/null
