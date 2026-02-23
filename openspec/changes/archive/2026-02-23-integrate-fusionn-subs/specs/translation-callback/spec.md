# translation-callback Specification

## Purpose

Handle HTTP callbacks from fusionn-subs when subtitle translation completes, triggering the merge pipeline.

## ADDED Requirements

### Requirement: Callback Endpoint

The system SHALL expose an HTTP endpoint at `/api/v1/callback/translation` to receive translation completion notifications from fusionn-subs.

#### Scenario: Successful callback reception

- **WHEN** fusionn-subs POSTs a callback with valid JSON payload
- **THEN** the system SHALL parse the payload containing job_id, video_path, eng_subtitle_path, and chs_subtitle_path
- **AND** the system SHALL validate all required fields are present
- **AND** the system SHALL respond with HTTP 202 Accepted
- **AND** the system SHALL enqueue a merge job with the provided subtitle paths

#### Scenario: Invalid callback payload

- **WHEN** callback payload is malformed JSON or missing required fields
- **THEN** the system SHALL respond with HTTP 400 Bad Request
- **AND** the system SHALL return a JSON error message indicating which fields are invalid
- **AND** the system SHALL log the validation error

#### Scenario: Callback for nonexistent files

- **WHEN** callback payload contains subtitle paths that do not exist on filesystem
- **THEN** the system SHALL respond with HTTP 400 Bad Request
- **AND** the system SHALL return an error indicating which files are missing
- **AND** the system SHALL log the file validation error

### Requirement: Merge Job Enqueueing

The system SHALL enqueue merge jobs when valid translation callbacks are received.

#### Scenario: Enqueue merge job from callback

- **WHEN** a valid callback is received with translated subtitle paths
- **THEN** the system SHALL create a merge job with:
  - Job ID from callback
  - Video path from callback
  - English subtitle path from callback
  - Chinese subtitle path from callback
  - Media title set to video path (for logging)
  - Media type set to "callback"
- **AND** the system SHALL enqueue the job to the merge queue
- **AND** the system SHALL log the job ID and paths

#### Scenario: Merge queue full

- **WHEN** merge queue is at capacity and cannot accept new jobs
- **THEN** the system SHALL respond with HTTP 503 Service Unavailable
- **AND** the system SHALL return a JSON error indicating queue is full
- **AND** fusionn-subs SHALL retry the callback with exponential backoff

### Requirement: Callback Authentication

The system SHALL accept callbacks without authentication in initial implementation.

#### Scenario: Unauthenticated callback

- **WHEN** a callback request arrives without authentication headers
- **THEN** the system SHALL process the callback normally
- **AND** the system SHALL NOT require API keys or tokens

#### Scenario: Future authentication consideration

- **WHEN** authentication is added in future versions
- **THEN** the system SHALL support API key or shared secret authentication
- **AND** the system SHALL reject callbacks without valid credentials

### Requirement: Callback Idempotency

The system SHALL handle duplicate callbacks gracefully.

#### Scenario: Duplicate callback for same job

- **WHEN** fusionn-subs retries a callback for a job already completed
- **THEN** the system SHALL check if job_id already exists in merge queue or completed jobs
- **AND** if job is already processed, the system SHALL respond with HTTP 200 OK
- **AND** the system SHALL NOT enqueue a duplicate merge job
- **AND** the system SHALL log that duplicate callback was ignored

#### Scenario: Callback for in-progress merge

- **WHEN** a callback arrives for a job currently being merged
- **THEN** the system SHALL respond with HTTP 202 Accepted
- **AND** the system SHALL NOT enqueue a duplicate merge job
- **AND** the system SHALL log that merge is already in progress
