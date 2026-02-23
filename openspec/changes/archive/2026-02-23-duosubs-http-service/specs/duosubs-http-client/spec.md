## ADDED Requirements

### Requirement: HTTP client SHALL call remote DuoSubs service
The HTTP client SHALL send merge requests to a remote DuoSubs HTTP service with automatic path translation from container paths to host paths.

#### Scenario: Successful merge request
- **WHEN** fusionn calls `Merge(ctx, primaryPath, secondaryPath, outputDir)` with container paths
- **THEN** HTTP client translates paths using configured prefixes and sends POST request to `/merge` endpoint
- **THEN** HTTP client returns the translated output path on success

#### Scenario: Service unavailable
- **WHEN** fusionn calls `Merge()` but HTTP service is unreachable
- **THEN** HTTP client returns error with connection details
- **THEN** error message indicates service URL and connection failure

#### Scenario: Service returns error
- **WHEN** HTTP service returns error response (non-2xx status)
- **THEN** HTTP client SHALL parse error from response body
- **THEN** HTTP client returns error with service error message

### Requirement: HTTP client SHALL validate service health
The HTTP client SHALL provide a health check method to verify service connectivity before processing requests.

#### Scenario: Healthy service
- **WHEN** fusionn calls `HealthCheck(ctx)` and service is running
- **THEN** HTTP client returns nil error
- **THEN** request completes within 5 seconds

#### Scenario: Unhealthy service
- **WHEN** fusionn calls `HealthCheck(ctx)` and service is down
- **THEN** HTTP client returns error indicating health check failed
- **THEN** error includes service URL for debugging

### Requirement: HTTP client SHALL translate paths between container and host
The HTTP client SHALL convert container filesystem paths to host filesystem paths using configured prefix mappings.

#### Scenario: Path within container prefix
- **WHEN** HTTP client receives path `/data/media/show/file.srt` with container prefix `/data` and host prefix `/Users/me/media`
- **THEN** HTTP client translates to `/Users/me/media/media/show/file.srt` in request payload

#### Scenario: Path outside container prefix
- **WHEN** HTTP client receives path `/tmp/file.srt` with container prefix `/data`
- **THEN** HTTP client passes path unchanged (no translation)

### Requirement: HTTP client SHALL handle timeouts
The HTTP client SHALL enforce configurable timeouts for merge operations to prevent indefinite blocking.

#### Scenario: Operation completes within timeout
- **WHEN** merge operation completes in 5 minutes with 10 minute timeout
- **THEN** HTTP client returns success with output path

#### Scenario: Operation exceeds timeout
- **WHEN** merge operation exceeds configured timeout (default 15 minutes)
- **THEN** HTTP client cancels request and returns timeout error
- **THEN** error message indicates timeout duration

### Requirement: HTTP client SHALL use JSON for request/response
The HTTP client SHALL serialize merge requests as JSON and deserialize JSON responses.

#### Scenario: Valid request serialization
- **WHEN** HTTP client sends merge request
- **THEN** request body is valid JSON with `primary_path`, `secondary_path`, `output_dir`, `container_prefix`, `host_prefix` fields
- **THEN** request Content-Type header is `application/json`

#### Scenario: Valid response deserialization
- **WHEN** HTTP service returns successful response
- **THEN** HTTP client parses JSON response with `success`, `output_path` fields
- **THEN** HTTP client extracts output path from response
