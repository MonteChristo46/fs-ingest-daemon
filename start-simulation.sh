#!/bin/bash

# Configuration
SOURCE_DIR="./test-data/mvtec_anomaly_detection"
TARGET_DIR="./data"
RATE="1s" # Slowed down slightly to be readable
DB_PATH="./fsd.db"
LOG_FILE="./simulation.log"

# Cleanup & Build
echo "--- CLEANUP ---"
echo "Cleaning up $TARGET_DIR and $DB_PATH..."
rm -rf "$TARGET_DIR" "$DB_PATH" "$LOG_FILE"
mkdir -p "$TARGET_DIR"

echo "--- BUILD ---"
go build -o fsd ./cmd/fsd/main.go || { echo "Build failed"; exit 1; }

# Function to kill background processes on exit
cleanup() {
    echo ""
    echo "--- STOPPING ---"
    if [ -n "$FSD_PID" ]; then
        echo "Killing Daemon (PID $FSD_PID)..."
        kill $FSD_PID 2>/dev/null
    fi
    if [ -n "$SIM_PID" ]; then
        echo "Killing Simulator (PID $SIM_PID)..."
        kill $SIM_PID 2>/dev/null
    fi
    exit
}
trap cleanup INT TERM

# Start Daemon
echo "--- STARTING DAEMON ---"
# Redirect daemon output to log file
./fsd run >> "$LOG_FILE" 2>&1 &
FSD_PID=$!
echo "Daemon running with PID $FSD_PID (logs -> $LOG_FILE)"
sleep 2 # Give daemon time to init watcher and DB

# Start Simulator
echo "--- STARTING SIMULATOR ---"
echo "Source:  $SOURCE_DIR"
echo "Target:  $TARGET_DIR"
echo "Rate:    $RATE"
echo "------------------------------------------------"

# Redirect simulator output to log file
./fsd simulate \
    --source "$SOURCE_DIR" \
    --target "$TARGET_DIR" \
    --rate "$RATE" >> "$LOG_FILE" 2>&1 &
SIM_PID=$!
echo "Simulator running with PID $SIM_PID (logs -> $LOG_FILE)"

# Tail the logs so we can see what's happening
tail -f "$LOG_FILE" &
TAIL_PID=$!

# Wait forever (until Ctrl+C)
wait $FSD_PID $SIM_PID

# Kill tail on exit
kill $TAIL_PID 2>/dev/null