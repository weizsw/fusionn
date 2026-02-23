## Why

Docker on macOS cannot access Metal GPU, causing DuoSubs subtitle merging to run 10-50x slower in containers (CPU-only). This creates a poor user experience with subtitle processing taking minutes instead of seconds. By offloading DuoSubs execution to a native host service, we can leverage Metal GPU acceleration while keeping fusionn containerized.

## What Changes

- Create a FastAPI-based DuoSubs HTTP service that runs natively on the host with GPU access
- Add HTTP mode to fusionn's DuoSubs executor (alongside existing local mode)
- Implement automatic path translation between container and host filesystems
- Add configuration for DuoSubs execution mode (local vs http)
- Maintain backward compatibility with existing local binary execution

## Capabilities

### New Capabilities
- `duosubs-http-client`: HTTP client in fusionn for calling remote DuoSubs service with path translation
- `duosubs-http-service`: FastAPI service wrapper for DuoSubs that runs on host with GPU access

### Modified Capabilities
- `subtitle-merging`: Add mode selection (local/http) and HTTP service integration for DuoSubs execution

## Impact

**Code:**
- `internal/executor/duosubs.go`: Add mode detection, HTTP client integration
- `internal/client/duosubs/`: New HTTP client package
- `internal/config/config.go`: Add HTTP mode configuration fields
- `internal/service/subtitle/processor_merger.go`: Initialize executor with mode-specific config
- `duosubs-service/`: New Python service directory

**Configuration:**
- Add `subtitle.duosubs.mode` field ("local" or "http")
- Add `subtitle.duosubs.http_*` fields for service URL and path mapping

**Deployment:**
- Optional: Run `duosubs-service` on host for GPU acceleration
- No impact if using default "local" mode
- Docker Compose example for integrated setup

**Dependencies:**
- New Python dependencies for HTTP service: `fastapi`, `uvicorn`, `pydantic`
- No new Go dependencies (use stdlib HTTP client)
