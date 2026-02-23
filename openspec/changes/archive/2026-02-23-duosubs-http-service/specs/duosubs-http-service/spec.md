## ADDED Requirements

### Requirement: Service SHALL expose HTTP API for subtitle merging
The DuoSubs HTTP service SHALL provide a REST API endpoint for merging subtitle files with automatic path translation.

#### Scenario: Successful merge via API
- **WHEN** client sends POST request to `/merge` with valid `primary_path`, `secondary_path`, `output_dir`
- **THEN** service translates container paths to host paths
- **THEN** service executes duosubs merge command
- **THEN** service returns 200 OK with `success: true` and `output_path`

#### Scenario: Input file not found
- **WHEN** client sends merge request but primary or secondary file does not exist on host
- **THEN** service returns 200 OK with `success: false` and descriptive error message
- **THEN** error message includes which file was not found and the path checked

#### Scenario: DuoSubs execution fails
- **WHEN** duosubs command fails with non-zero exit code
- **THEN** service returns 200 OK with `success: false` and error from duosubs stderr
- **THEN** error message helps diagnose duosubs failure

### Requirement: Service SHALL translate paths from container to host
The service SHALL convert filesystem paths from container coordinate system to host coordinate system using configured prefix mappings.

#### Scenario: Path translation with matching prefix
- **WHEN** service receives request with `container_prefix: "/data"` and `host_prefix: "/Users/me/media"`
- **WHEN** request includes `primary_path: "/data/show/file.srt"`
- **THEN** service translates to `/Users/me/media/show/file.srt` before accessing filesystem

#### Scenario: Path translation with no prefix match
- **WHEN** service receives path `/tmp/file.srt` with `container_prefix: "/data"`
- **THEN** service uses path as-is without translation

#### Scenario: Path translation configured via environment
- **WHEN** service starts with `HOST_MEDIA_PATH` environment variable set
- **WHEN** request omits `host_prefix` field
- **THEN** service uses environment variable for host prefix

### Requirement: Service SHALL provide health check endpoint
The service SHALL expose a health check endpoint to verify service availability.

#### Scenario: Health check returns healthy status
- **WHEN** client sends GET request to `/health`
- **THEN** service returns 200 OK with JSON `{"status": "healthy", "service": "duosubs"}`

### Requirement: Service SHALL execute duosubs with proper environment
The service SHALL run duosubs with configuration optimized for host GPU access.

#### Scenario: DuoSubs execution uses host GPU
- **WHEN** service runs on macOS with Metal GPU
- **THEN** duosubs process can access Metal acceleration (MPS device)
- **THEN** subtitle merging completes 10-50x faster than CPU-only

#### Scenario: DuoSubs execution respects timeout
- **WHEN** duosubs execution exceeds 10 minutes (default timeout)
- **THEN** service terminates duosubs process
- **THEN** service returns error indicating timeout

### Requirement: Service SHALL validate request payload
The service SHALL validate incoming merge requests have required fields before processing.

#### Scenario: Valid request payload
- **WHEN** client sends request with all required fields (`primary_path`, `secondary_path`, `output_dir`, `container_prefix`)
- **THEN** service accepts request and begins processing

#### Scenario: Missing required field
- **WHEN** client sends request missing `primary_path` or `secondary_path`
- **THEN** service returns 422 Unprocessable Entity with validation error

#### Scenario: Invalid JSON payload
- **WHEN** client sends malformed JSON in request body
- **THEN** service returns 422 Unprocessable Entity with JSON parse error

### Requirement: Service SHALL create output directories
The service SHALL ensure output directories exist before executing duosubs.

#### Scenario: Output directory creation
- **WHEN** merge request specifies `output_dir` that does not exist
- **THEN** service creates directory (including parents) with appropriate permissions
- **THEN** duosubs execution proceeds successfully

### Requirement: Service SHALL run on configurable host and port
The service SHALL allow configuration of bind host and port via environment variables.

#### Scenario: Custom port configuration
- **WHEN** service starts with `DUOSUBS_PORT=9000` environment variable
- **THEN** service listens on port 9000 instead of default 8765

#### Scenario: Custom host configuration
- **WHEN** service starts with `DUOSUBS_HOST=127.0.0.1`
- **THEN** service binds to localhost only (not all interfaces)

#### Scenario: Default configuration
- **WHEN** service starts without environment variables
- **THEN** service listens on `0.0.0.0:8765` by default
