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

*   Go 1.24 or higher (for building from source)

### Building the Binary

```bash
go build -o fsd cmd/fsd/main.go
```

### Configuration

On the first run, the daemon generates a `config.json` file in the same directory as the binary. You can also create it manually.

**Default `config.json`:**
```json
{
  "device_id": "dev-001",
  "endpoint": "http://localhost:8000",
  "max_data_size_gb": 1,
  "watch_path": "./data",
  "log_path": "./fsd.log",
  "db_path": "./fsd.db",
  "ingest_check_interval": "2s",
  "ingest_batch_size": 10,
  "prune_check_interval": "1m",
  "prune_batch_size": 50,
  "api_timeout": "30s"
}
```

**Parameters:**

| Parameter | Description | Default |
| :--- | :--- | :--- |
| `device_id` | Unique identifier for this edge device used in API requests. | `"dev-001"` |
| `endpoint` | Base URL of the Ingestion API. | `"http://localhost:8080"` |
| `watch_path` | Local directory to recursively watch for new files. | `"./data"` |
| `max_data_size_gb` | Disk usage threshold (GB) for the `watch_path`. Pruning triggers if exceeded. | `1.0` |
| `log_path` | Path to the application log file. | `"./fsd.log"` |
| `db_path` | Path to the SQLite state database. | `"./fsd.db"` |
| `ingest_check_interval` | How frequently the daemon checks for `PENDING` files to upload (e.g., "2s", "500ms"). | `"2s"` |
| `ingest_batch_size` | Maximum number of files to process in a single upload cycle. | `10` |
| `prune_check_interval` | How frequently the daemon checks disk usage (e.g., "1m", "30s"). | `"1m"` |
| `prune_batch_size` | Maximum number of files to delete in a single pruning cycle. | `50` |
| `api_timeout` | HTTP timeout for API requests (e.g., "30s"). | `"30s"` |

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
*   `internal/api`: HTTP client and data models for the Ingestion API.
*   `internal/config`: Configuration loading and management.
*   `internal/ingest`: Core ingestion logic (Handshake -> Upload -> Confirm).
*   `internal/pruner`: Disk space management and file eviction logic.
*   `internal/store`: SQLite database interactions.
*   `internal/watcher`: Recursive file system watcher using `fsnotify`.
*   `internal/util`: Helper functions for metadata extraction.

## License

[MIT](LICENSE)
