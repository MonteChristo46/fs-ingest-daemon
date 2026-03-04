#!/bin/bash

# Configuration
SOURCE_DIR="./test-data/mvtec_anomaly_detection"
TARGET_DIR="$HOME/fsd/data"
RATE="1s"
DB_PATH="$HOME/fsd/fsd.db"
LOG_FILE="./simulation.log"
CATEGORIES=${1:-""} # Optional categories to filter

echo "--- CLEANUP ---"
echo "Cleaning up $TARGET_DIR..."
# Note: We only delete the contents, NOT the directory itself, so the daemon's fsnotify watcher doesn't break.
# We also do NOT delete DB_PATH as the background daemon holds an active connection to it.
rm -rf "$TARGET_DIR"/*
> "$LOG_FILE"
mkdir -p "$TARGET_DIR"

echo "--- BUILD ---"
go build -o fsd ./cmd/fsd/main.go || { echo "Build failed"; exit 1; }

# Function to kill background processes on exit
cleanup() {
    echo ""
    echo "--- STOPPING ---"
    if [ -n "$SIM_PID" ]; then
        echo "Killing Simulator (PID $SIM_PID)..."
        kill $SIM_PID 2>/dev/null
    fi
    exit
}
trap cleanup INT TERM

echo "--- STARTING SIMULATOR ---"
echo "Source:     $SOURCE_DIR"
echo "Target:     $TARGET_DIR"
echo "Rate:       $RATE"
if [ -n "$CATEGORIES" ]; then
    echo "Categories: $CATEGORIES"
fi
echo "------------------------------------------------"

# Build simulate command arguments
SIM_ARGS=(simulate --source "$SOURCE_DIR" --target "$TARGET_DIR" --rate "$RATE")
if [ -n "$CATEGORIES" ]; then
    SIM_ARGS+=(--categories "$CATEGORIES")
fi

# Redirect simulator output to log file
./fsd "${SIM_ARGS[@]}" >> "$LOG_FILE" 2>&1 &
SIM_PID=$!
echo "Simulator running with PID $SIM_PID (logs -> $LOG_FILE)"

# Tail the logs so we can see what's happening
# We tail both the simulation log and the daemon log (if it exists)
DAEMON_LOG="$HOME/fsd/fsd.log"
tail -f "$LOG_FILE" "$DAEMON_LOG" 2>/dev/null &
TAIL_PID=$!

# Wait for simulator to finish or until Ctrl+C
wait $SIM_PID

# Kill tail on exit
kill $TAIL_PID 2>/dev/null
