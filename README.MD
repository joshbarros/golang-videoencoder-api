# Golang Video Encoder API

Video processing pipeline in Go that:
- downloads a source video from Google Cloud Storage (GCS),
- fragments and packages it to DASH with Bento4,
- uploads generated artifacts back to GCS.

## Current Project Status

What is working now:
- GCS-based download and upload flow (AWS removed).
- End-to-end service tests passing in Docker container.
- Upload completion signaling fixed to wait for all workers.
- Core packages and tests documented with GoDoc comments.

Important note:
- Current `cmd/main.go` is still a minimal placeholder (`Hello World`).
- This repository currently provides the processing core and tests, not a full production runtime service yet.

## Project Structure

- `internal/video/`
  - `service.go`: download, fragment, encode, cleanup
- `internal/upload/`
  - `manager.go`: concurrent upload manager for output artifacts
- `internal/job/`
  - `service.go`, `worker.go`, `manager.go`: job lifecycle and worker contracts
- `internal/repositories/video/`
  - `repository.go`: persistence adapter for `Video`
- `internal/repositories/job/`
  - `repository.go`: persistence adapter for `Job`
- `domain/`
  - core entities and validation (`Video`, `Job`)
- `internal/database/`
  - database connection setup and test DB helper
- `internal/queue/`
  - RabbitMQ client wrapper
- `cmd/main.go`
  - entrypoint placeholder

## Stack

- Go 1.25
- Google Cloud Storage (`cloud.google.com/go/storage`)
- Bento4 (`mp4fragment`, `mp4dash`)
- PostgreSQL (dev/runtime dependency via compose)
- RabbitMQ (provisioned in compose)
- GORM v2

## Prerequisites

- Docker + Docker Compose
- A GCS bucket for source/output artifacts
- Service account key JSON with bucket permissions

## Environment

Use `.env.example` as reference.

Main variables:
- `DB_TYPE`
- `DSN`
- `ENV`
- `DEBUG`
- `AUTO_MIGRATE_DB`
- `localStoragePath` (default `/tmp`)
- `GOOGLE_APPLICATION_CREDENTIALS`
- `VIDEO_SOURCE_BUCKET`
- `VIDEO_OUTPUT_BUCKET`

### Credentials

Local credential files are intentionally gitignored (`bucket-credentials.json`, `.env`).

## Running with Docker

### 1) Build containers

```bash
docker compose build app
```

### 2) Start services

```bash
docker compose up -d
```

### 3) Run service tests (inside container)

```bash
docker compose exec app /usr/local/go/bin/go test -v -count=1 ./internal/video/... ./internal/upload/... ./internal/job/... ./internal/repositories/job/... ./internal/repositories/video/... ./internal/database/... ./internal/queue/...
```

### 4) Run full test suite (inside container)

```bash
docker compose exec app /usr/local/go/bin/go test -count=1 ./...
```

## Manual Upload Smoke Check (No Unit Test)

You can manually validate upload behavior by creating a temporary file under `/tmp/<folder>` and invoking `VideoUpload` flow from a small Go script in the container, then confirming object existence in GCS.

This was validated during development and confirmed object creation + cleanup in the target bucket.

## Known Gaps Before Full Production Runtime

- `cmd/main.go` is still a placeholder; no HTTP/consumer runtime yet.
- No health/readiness endpoints yet.
- No structured retry/backoff policy for external operations.
- No versioned DB migration framework yet.
- Docker image is currently optimized for development/testing (entrypoint does not start the app runtime process).

## Quick Development Notes

- If Bento4 binaries are missing in host environment, run tests in container.
- `upload_manager.go` now waits for all workers before signaling completion.
- Integration tests require valid GCS credentials and bucket access.

## License

No license file has been added yet.
