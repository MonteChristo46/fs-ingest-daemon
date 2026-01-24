# Project: fs-ingest-daemon

The primary goal of fs-ingest-daemon is to provide a zero-dependency, resilient data bridge that transforms local file-system events into structured cloud data. It aims to solve the "Last Mile" problem of edge computing: ensuring that images are captured, metadata is extracted from context, and data is safely uploaded to a cloud pipeline even in environments with intermittent connectivity or limited storage.

## General instructions

- You will use Golang as primary language and follow best code practices. 
- Use Kardianos for creating the service
- As CLI Engine use Cobra
- As State Store you can use sqlite
- Since we want to transfer a couple of images per second you try to keep the http connection open. HTTPS:// Handshakes are expensive. 
- When interacting with the API you must adhere to the @openapi.json
- When dealing with API addresses, URI you must use env files. 
- The app must be highly configurable means - config values must be managed in @internal/config/config.go. 

## In-Scope (The "Must-Haves")
Platform Portability: Native execution on Windows (x64) and Linux (x64/ARM) without requiring pre-installed runtimes (Python/Node). We will use kardianos

Resource Efficiency: Maintaining a minimal CPU and RAM footprint so it can run on industrial PCs or Raspberry Pi devices alongside other processes.

Data Integrity: Guaranteeing that no image is deleted from the edge until a "Success" handshake is confirmed by the Cloud API.

Self-Management: Enabling end-users to install, monitor, and troubleshoot the service via a simple CLI interface.

Contextual Intelligence: Automatically mapping directory hierarchies to metadata tags to avoid manual configuration for every new camera or sensor.

## High Level Architecture Overview: 

Detection: Local FS change.

Handshake: API provides the S3 "Key" (Presigned URL).

Transfer: Edge streams binary to S3.

Finalize: API updates Postgres/Redis; Edge marks file as UPLOADED. (NOT PART of the fs-ingest daemon)

Lifecycle: Pruner eventually removes UPLOADED files when disk space is needed.

### The Pruning ALgorithm
The "Genius" Pruning Algorithm
We implement a Lifecycle-Based Eviction strategy to prevent data loss during network outages:

Constraint: Folder size < MAX_GB.

Selection: Only files with status == 'UPLOADED' in the local SQLite DB are candidates for deletion.

Order: Least Recently Modified (LRM) files are deleted first.

Safety Valve (Backpressure): If CurrentSize > MAX_GB AND DeletableFiles == 0, the service enters Pause Mode and triggers a system alert.

### The Ingestion Handshake

We dont have the API yet. But it will follow in later deployments but currently out of scope. Instead of the S3 Upload and the presigned logic we implement a logging statement. BUT after success Daemon marks local file as UPLOADED in SQLite.

## User Experience
The end-user interaction must be "no-nonsense":

Download: Grab the binary for the specific OS.

Config: Fill in endpoint and max_size in the config file.

Install: Run ./fsd install.

Monitor: Run ./fsd status.