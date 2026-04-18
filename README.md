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
2.  **Store (SQLite):** A local persistent state store (`hunt.db`) that tracks every file's lifecycle (`PENDING` -> `UPLOADED`) and metadata.
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
5.  **Pruner:** Monitors local disk usage. Implements a Hysteresis loop: eviction starts when usage exceeds `max_data_size_gb` * `prune_high_watermark_percent` (default 90%) and continues until usage drops below `prune_low_watermark_percent` (default 75%). This prevents rapid oscillation and reduces disk/DB fragmentation. Only `UPLOADED` files are eligible for deletion (LRM).

## Installation

**fs-ingest-daemon** handles its own setup. You can install it with a single command or manually.

### Quick Install

These commands automatically download the latest binary, install it, and start the interactive setup.

#### Linux / macOS

**Option A: System Service (Recommended)**
*Requires sudo. Installs to `/opt/hunt` and runs on boot.*
```bash
curl -sfL https://raw.githubusercontent.com/MonteChristo46/fs-ingest-daemon/main/scripts/install.sh | sudo sh
```

**Option B: User Service**
*No sudo required. Installs to `~/hunt` and runs on login.*
```bash
curl -sfL https://raw.githubusercontent.com/MonteChristo46/fs-ingest-daemon/main/scripts/install.sh | sh
```

#### Windows

**Option A: System Service (Recommended)**
*Run in PowerShell as Administrator. Installs to `C:\ProgramData\hunt`.*
```powershell
iwr -useb https://raw.githubusercontent.com/MonteChristo46/fs-ingest-daemon/main/scripts/install.ps1 | iex
```

**Option B: User Service**
*Run in standard PowerShell. Installs to `%LOCALAPPDATA%\hunt`.*
```powershell
iwr -useb https://raw.githubusercontent.com/MonteChristo46/fs-ingest-daemon/main/scripts/install.ps1 | iex
```

### Manual Installation

If you prefer to download the binary manually:

1.  **Download** the latest binary for your OS from the [Releases](https://github.com/MonteChristo46/fs-ingest-daemon/releases) page.
2.  **Run the installer**:

    **Linux / macOS**
    ```bash
    chmod +x hunt
    sudo ./hunt install
    ```

    **Windows (PowerShell Admin)**
    ```powershell
    .\hunt.exe install
    ```

### Interactive Setup
The installer will verify your environment and guide you through:
1.  **Location:** Confirms the install directory based on your permissions (System vs. User path).
2.  **Config:** Prompts for your `Device ID` and `API Endpoint`.
3.  **Loading Dock:** Asks where the daemon should look for files to upload. It will suggest a safe user-space directory (e.g., `~/glitch-hunt/input`) and ensure the regular user has proper read/write access to this "Drop Zone".
4.  **Pairing:** If the device is new, a QR code will appear. Scan it with the web app to claim the device.
5.  **Service:** The daemon registers itself with the OS and starts automatically.

### Uninstallation

To cleanly remove the service, data, and binary:

**Linux / macOS**
```bash
# System Install (if you installed with sudo)
curl -sfL https://raw.githubusercontent.com/MonteChristo46/fs-ingest-daemon/main/scripts/uninstall.sh | sudo sh

# User Install
curl -sfL https://raw.githubusercontent.com/MonteChristo46/fs-ingest-daemon/main/scripts/uninstall.sh | sh
```

**Windows**
```powershell
# Works for both System (Admin) and User installs
iwr -useb https://raw.githubusercontent.com/MonteChristo46/fs-ingest-daemon/main/scripts/uninstall.ps1 | iex
```

### Management
Once installed, use the CLI to manage the service:

```bash
# Check status
hunt status

# View live logs
hunt logs

# Stop/Start service
sudo hunt stop
sudo hunt start
```

## Configuration

The configuration file is generated at install time (e.g., `/opt/hunt/config.json`). You can edit this file manually to tune advanced settings.

**Configuration Parameters:**

| Parameter | Description | Default |
| :--- | :--- | :--- |
| `device_id` | Unique identifier used in API requests (e.g., "dev-001"). | `(User Input)` |
| `endpoint` | Base URL of the Ingestion API. | `(User Input)` |
| `sidecar_strategy` | Pairing strategy. `strict` waits for .json sidecar; `none` uploads standalone files. | `"none"` |
| `allowed_extensions` | List of allowed file extensions (case-insensitive). | `[".jpg", ".jpeg", ".png", ".json"]` |
| `watch_path` | Local directory path to watch for new files (The Loading Dock). | `~/glitch-hunt/input` |
| `max_data_size_gb` | Maximum allowed size for local storage (GB) before pruning kicks in. | `1.0` |
| `ingest_check_interval` | Polling frequency for checking new PENDING files. | `"20ms"` |
| `ingest_batch_size` | Number of files to process in a single ingest cycle. | `10` |
| `ingest_worker_count` | Number of concurrent upload workers. | `5` |
| `prune_check_interval` | Frequency of disk usage checks. | `"1m"` |
| `prune_batch_size` | Number of files to delete per prune cycle when full. | `50` |
| `prune_high_watermark_percent` | Percentage of Max Size to trigger eviction. | `90` |
| `prune_low_watermark_percent` | Percentage of Max Size to stop eviction. | `75` |
| `api_timeout` | Timeout duration for HTTP requests to the Cloud API. | `"30s"` |
| `debounce_duration` | Wait time after file write before processing (prevents partial reads). | `"500ms"` |
| `orphan_check_interval` | Time before a waiting file is marked as ORPHAN (uploaded without partner). | `"5m"` |
| `image_compression_enabled` | Whether to resize and compress images before upload (JPEG/PNG). | `true` |
| `image_max_dimension_px` | Maximum dimension (width or height) in pixels for resized images. | `800` |
| `image_compression_quality` | JPEG compression quality (1-100). | `80` |
| `metadata_update_interval` | Frequency of sending system info (OS, Uptime, IP) to the API. | `"24h"` |
| `web_client_url` | URL displayed in the QR code for device claiming. | `(Default Cloud URL)` |
| `log_max_size_mb` | Max size in MB before log rotation. | `10` |
| `log_max_backups` | Max number of old log files to retain. | `3` |
| `log_max_age_days` | Max number of days to retain old log files. | `28` |
| `log_compress` | Whether to compress old log files (gzip). | `true` |

### Changing Configuration

To modify the configuration after installation:

1.  **Locate the config file:**
    *   **Linux/macOS (System):** `/opt/hunt/config.json`
    *   **Linux/macOS (User):** `~/hunt/config.json`
    *   **Windows (System):** `C:\ProgramData\hunt\config.json`
    *   **Windows (User):** `%USERPROFILE%\hunt\config.json`

2.  **Edit the file:** Open `config.json` in any text editor (requires Admin/Root for system installs).

3.  **Restart the service:** Changes only take effect after a restart.
    ```bash
    # Linux / macOS
    sudo hunt restart

    # Windows (Powershell Admin)
    hunt restart
    ```

## Building from Source

If you are a developer contributing to the project:

```bash
# Generate the install scripts and build the binary for all platforms
./build.sh

# Or build the binary directly
go build -o hunt cmd/hunt/main.go

# Run locally (Foreground)
./hunt run
```

> **Note on Install Scripts:** The `scripts/install.sh` and `scripts/install.ps1` files are auto-generated from their `.tpl` counterparts during the build process to inject the version and CLI banner. If you need to modify the installers, edit the `.tpl` files and run `./build.sh`.

## Project Structure

*   `cmd/hunt`: Main entry point and CLI implementation.
*   `internal/api`: HTTP client and data models for the Ingestion API.
*   `internal/config`: Configuration loading and management.
*   `internal/ingest`: Core ingestion logic (Handshake -> Upload -> Confirm).
*   `internal/pruner`: Disk space management and file eviction logic.
*   `internal/store`: SQLite database interactions.
*   `internal/watcher`: Recursive file system watcher using `fsnotify`.
*   `internal/util`: Helper functions for metadata extraction.

## License

[MIT](LICENSE)
