# fs-ingest-daemon

**fs-ingest-daemon** is a zero-dependency, resilient data bridge designed to transform local file-system events into structured cloud data. It solves the "Last Mile" problem of edge computing by ensuring images and data are captured, contextually tagged, and safely uploaded to a cloud pipeline, even in environments with intermittent connectivity or limited storage.

## Features

*   **Platform Portability:** Runs natively on Windows (x64) and Linux (x64/ARM) as a system service.
*   **Resource Efficiency:** Minimal CPU and RAM footprint, suitable for industrial PCs and edge devices (e.g., Raspberry Pi).
*   **Data Integrity:** Guarantees no data loss. Files are only eligible for deletion after a confirmed successful upload to the cloud.
*   **Smart Pruning:** Lifecycle-based eviction strategy ("Genius Pruning") that manages local disk space by removing the oldest uploaded files when limits are reached.
*   **Resilient Connectivity:** Buffers data locally during network outages and retries uploads automatically.
*   **Contextual Intelligence:** Automatically extracts metadata and context tags from the directory hierarchy (e.g., `cam_1/2026/01/06/...`).
*   **Flexible Pairing:** Configurable sidecar strategy (`strict` vs `none`) to support both metadata-rich setups and simple image streams.

## Architecture

The daemon operates with four main concurrent components:

1.  **Watcher:** Recursively watches a target directory for new files. When a file is detected, it is recorded in the local SQLite database.
2.  **Store (SQLite):** A local persistent state store (`fsd.db`) that tracks every file's lifecycle (`PENDING` -> `UPLOADED`) and metadata.
3.  **Sidecar Logic:**
    *   **Strict Mode:** Waits for a companion `.json` file (e.g., `img.png` + `img.png.json`) to arrive.
    *   **None Mode:** Uploads files immediately as they are detected.
    *   ![Sidecar Logic](http://www.plantuml.com/plantuml/proxy?cache=no&src=https://raw.githubusercontent.com/user/repo/main/docs/sidecar_logic.plantuml)
    *   *(See `docs/sidecar_logic.plantuml` for the diagram source)*
4.  **Ingester:**
    *   Polls for `PENDING` files.
    *   Calculates SHA256 checksums.
    *   Extracts metadata from file paths.
    *   Initiates a handshake with the Cloud API to get a Presigned Upload URL.
    *   Streams the file directly to object storage (S3).
    *   Confirms the upload with the API and marks the file as `UPLOADED`.
5.  **Pruner:** Monitors local disk usage. If the watched directory size exceeds the configured `max_data_size_gb`, it deletes the Least Recently Modified (LRM) files that have been successfully `UPLOADED`, freeing up space for new data.

## Installation

**fs-ingest-daemon** is a single-binary application that handles its own setup. It supports two installation modes:

1.  **System Service (Recommended):** Runs as a background service on system boot. Requires Administrator/Root privileges. Default path: `/opt/fsd` or `C:\ProgramData\fsd`.
2.  **User Service:** Runs as a background agent only when the specific user logs in. No special privileges required. Default path: `~/fsd`.

### Linux / macOS

**Option A: System Service (Admin)**
*Best for headless servers and edge devices.*
```bash
chmod +x fsd
sudo ./fsd install
```

**Option B: User Service (Non-Admin)**
*Best for personal development machines or locked-down environments.*
```bash
chmod +x fsd
./fsd install
```

### Windows

**Option A: System Service (Admin)**
*Best for production deployments.*
1.  Open PowerShell as **Administrator**.
2.  Run:
    ```powershell
    .\fsd.exe install
    ```

**Option B: User Service (Non-Admin)**
*Best for testing without admin rights.*
1.  Open a standard PowerShell window.
2.  Run:
    ```powershell
    .\fsd.exe install
    ```

### Interactive Setup
The installer will verify your environment and guide you through:
1.  **Location:** Confirms the install directory based on your permissions (System vs. User path).
2.  **Config:** Prompts for your `Device ID` and `API Endpoint`.
3.  **Pairing:** If the device is new, a QR code will appear. Scan it with the web app to claim the device.
4.  **Service:** The daemon registers itself with the OS and starts automatically.

### 3. Management
Once installed, use the CLI to manage the service:

```bash
# Check status
fsd status

# View live logs
fsd logs

# Stop/Start service
sudo fsd stop
sudo fsd start

# Uninstall (Preserves data)
sudo fsd uninstall
```

## Configuration

The configuration file is generated at install time (e.g., `/opt/fsd/config.json`). You can edit this file manually to tune advanced settings.

**Configuration Parameters:**

| Parameter | Description | Default |
| :--- | :--- | :--- |
| `device_id` | Unique identifier used in API requests (e.g., "dev-001"). | `(User Input)` |
| `endpoint` | Base URL of the Ingestion API. | `(User Input)` |
| `sidecar_strategy` | Pairing strategy. `strict` waits for .json sidecar; `none` uploads standalone files. | `"strict"` |
| `watch_path` | Local directory path to watch for new files. | `[InstallDir]/data` |
| `max_data_size_gb` | Maximum allowed size for local storage (GB) before pruning kicks in. | `1.0` |
| `ingest_check_interval` | Polling frequency for checking new PENDING files. | `"20ms"` |
| `ingest_batch_size` | Number of files to process in a single ingest cycle. | `10` |
| `ingest_worker_count` | Number of concurrent upload workers. | `5` |
| `prune_check_interval` | Frequency of disk usage checks. | `"1m"` |
| `prune_batch_size` | Number of files to delete per prune cycle when full. | `50` |
| `api_timeout` | Timeout duration for HTTP requests to the Cloud API. | `"30s"` |
| `debounce_duration` | Wait time after file write before processing (prevents partial reads). | `"500ms"` |
| `orphan_check_interval` | Time before a waiting file is marked as ORPHAN (uploaded without partner). | `"5m"` |
| `metadata_update_interval` | Frequency of sending system info (OS, Uptime, IP) to the API. | `"24h"` |
| `web_client_url` | URL displayed in the QR code for device claiming. | `(Default Cloud URL)` |

## Building from Source

If you are a developer contributing to the project:

```bash
# Build the binary
go build -o fsd cmd/fsd/main.go

# Run locally (Foreground)
./fsd run
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
