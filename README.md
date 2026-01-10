# fs-ingest-daemon

**fs-ingest-daemon** is a zero-dependency, resilient data bridge designed to transform local file-system events into structured cloud data. It solves the "Last Mile" problem of edge computing by ensuring images and data are captured, contextually tagged, and safely uploaded to a cloud pipeline, even in environments with intermittent connectivity or limited storage.

## Features

*   **Platform Portability:** Runs natively on Windows (x64) and Linux (x64/ARM) as a system service.
*   **Resource Efficiency:** Minimal CPU and RAM footprint, suitable for industrial PCs and edge devices (e.g., Raspberry Pi).
*   **Data Integrity:** Guarantees no data loss. Files are only eligible for deletion after a confirmed successful upload to the cloud.
*   **Smart Pruning:** Lifecycle-based eviction strategy ("Genius Pruning") that manages local disk space by removing the oldest uploaded files when limits are reached.
*   **Resilient Connectivity:** Buffers data locally during network outages and retries uploads automatically.
*   **Contextual Intelligence:** Automatically extracts metadata and context tags from the directory hierarchy (e.g., `cam_1/2026/01/06/...`).

## Architecture

The daemon operates with four main concurrent components:

1.  **Watcher:** Recursively watches a target directory for new files. When a file is detected, it is recorded in the local SQLite database with a `PENDING` status.
2.  **Store (SQLite):** A local persistent state store (`fsd.db`) that tracks every file's lifecycle (`PENDING` -> `UPLOADED`) and metadata.
3.  **Ingester:**
    *   Polls for `PENDING` files.
    *   Calculates SHA256 checksums.
    *   Extracts metadata from file paths.
    *   Initiates a handshake with the Cloud API to get a Presigned Upload URL.
    *   Streams the file directly to object storage (S3).
    *   Confirms the upload with the API and marks the file as `UPLOADED`.
4.  **Pruner:** Monitors local disk usage. If the watched directory size exceeds the configured `max_data_size_gb`, it deletes the Least Recently Modified (LRM) files that have been successfully `UPLOADED`, freeing up space for new data.

## Getting Started

### Prerequisites

*   Go 1.21 or higher (for building from source)

### Building the Binary

```bash
go build -o fsd cmd/fsd/main.go
```

### Configuration

On the first run, the daemon generates a `config.json` file in the same directory as the binary. You can also create it manually:

**config.json**
```json
{
  "device_id": "dev-001",
  "endpoint": "http://localhost:8080",
  "max_data_size_gb": 1.0,
  "watch_path": "./data"
}
```

*   **device_id**: Unique identifier for this edge device.
*   **endpoint**: The base URL of the Glitch Hunt Ingestion API.
*   **max_data_size_gb**: Maximum allowed size for the watched directory before pruning kicks in.
*   **watch_path**: The local directory to watch for incoming files.

### CLI Usage

The daemon includes a built-in CLI for easy management.

**Run in foreground (for testing):**
```bash
./fsd run
```

**Install as a system service:**
```bash
sudo ./fsd install
```

**Start the service:**
```bash
sudo ./fsd start
```

**Check service status:**
```bash
sudo ./fsd status
```

**View logs:**
```bash
./fsd logs
```

**Stop the service:**
```bash
sudo ./fsd stop
```

**Uninstall the service:**
```bash
sudo ./fsd uninstall
```

## Project Structure

*   `cmd/fsd`: Main entry point and CLI implementation.
*   `internal/api`: HTTP client for the Ingestion API.
*   `internal/config`: Configuration loading and management.
*   `internal/ingest`: Core ingestion logic (Handshake -> Upload -> Confirm).
*   `internal/pruner`: Disk space management and file eviction logic.
*   `internal/store`: SQLite database interactions.
*   `internal/watcher`: Recursive file system watcher using `fsnotify`.
*   `internal/util`: Helper functions for metadata extraction.

## License

[MIT](LICENSE)
