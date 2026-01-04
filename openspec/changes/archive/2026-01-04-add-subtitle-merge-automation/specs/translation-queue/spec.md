# Translation Queue Capability

## ADDED Requirements

### Requirement: Redis Queue Connection

The system SHALL establish a connection to Redis server for publishing translation job requests.

#### Scenario: Successful Redis connection

- **WHEN** the system starts and Redis configuration is provided
- **THEN** the system SHALL connect to Redis using host, port, password, and database from configuration
- **AND** the system SHALL verify the connection with a PING command
- **AND** the system SHALL log successful connection

#### Scenario: Redis connection failure

- **WHEN** Redis server is unreachable (connection timeout, wrong credentials)
- **THEN** the system SHALL retry connection with exponential backoff (1s, 2s, 4s, 8s, max 30s)
- **AND** after 5 failed attempts, the system SHALL log a critical error
- **AND** subtitle processing SHALL continue but translation jobs will fail

#### Scenario: Redis connection lost during operation

- **WHEN** Redis connection is lost after initial connection
- **THEN** the system SHALL attempt to reconnect automatically
- **AND** queued translation jobs SHALL be retried after reconnection

### Requirement: Translation Job Publishing

The system SHALL publish translation job messages to Redis queue when Chinese subtitles are missing.

#### Scenario: Enqueue translation job

- **WHEN** subtitle analyzer finds no Chinese subtitle in video
- **THEN** the system SHALL create a translation job message with:
  - Unique job ID (UUID v4)
  - Video file path
  - English subtitle file path
  - Callback URL (`http://{host}:{port}/api/v1/callback/translation`)
  - Media metadata (title, year, type)
  - Timestamp
- **AND** the system SHALL serialize the message to JSON
- **AND** the system SHALL RPUSH the message to Redis list key `fusionn:translation:queue`
- **AND** the system SHALL log the job ID and enqueue timestamp

#### Scenario: Translation job enqueue failure

- **WHEN** Redis RPUSH operation fails (connection error, out of memory)
- **THEN** the system SHALL log an error with job details
- **AND** the system SHALL NOT retry automatically (avoid infinite loops)
- **AND** the system SHALL return an error to the webhook handler

#### Scenario: Translation job already exists

- **WHEN** a translation job for the same video file is already in the queue
- **THEN** the system SHALL check if a job with matching video path exists
- **AND** if exists, the system SHALL NOT enqueue a duplicate job
- **AND** the system SHALL log that duplicate was skipped

### Requirement: Translation Callback Reception

The system SHALL accept translation completion callbacks from fusionn-subs service at `/api/v1/callback/translation`.

#### Scenario: Successful translation callback

- **WHEN** fusionn-subs sends a POST request with translated subtitle path
- **THEN** the system SHALL parse the JSON payload containing:
  - Job ID
  - Translated Chinese subtitle file path
  - Status ("completed")
- **AND** the system SHALL validate the translated file exists
- **AND** the system SHALL trigger subtitle merge using the translated Chinese subtitle
- **AND** the system SHALL respond with HTTP 200 OK

#### Scenario: Translation callback with failure status

- **WHEN** fusionn-subs sends a callback with status "failed"
- **THEN** the system SHALL log the failure reason from the payload
- **AND** the system SHALL NOT attempt subtitle merge
- **AND** the system SHALL respond with HTTP 200 OK (acknowledge receipt)

#### Scenario: Invalid callback payload

- **WHEN** callback payload is malformed JSON or missing required fields
- **THEN** the system SHALL respond with HTTP 400 Bad Request
- **AND** the system SHALL return a JSON error message indicating validation failure

#### Scenario: Callback for unknown job ID

- **WHEN** callback contains a job ID not found in system records
- **THEN** the system SHALL log a warning about unknown job ID
- **AND** the system SHALL respond with HTTP 404 Not Found

### Requirement: Translation Job Message Schema

The system SHALL use a standardized JSON schema for translation job messages.

#### Scenario: Job message schema validation

- **WHEN** creating a translation job message
- **THEN** the message SHALL contain all required fields:
  - `job_id` (string, UUID format)
  - `video_path` (string, absolute path)
  - `english_subtitle_path` (string, absolute path)
  - `callback_url` (string, valid HTTP URL)
  - `media_type` (string, enum: "movie" or "episode")
  - `metadata` (object with title, year, optional imdb_id)
  - `timestamp` (string, ISO 8601 format)
- **AND** the message SHALL be valid JSON

#### Scenario: Schema evolution compatibility

- **WHEN** a new field is added to the job schema in the future
- **THEN** existing fusionn-subs consumers SHALL ignore unknown fields (forward compatibility)
- **AND** required fields SHALL NOT be removed (backward compatibility)

### Requirement: Redis Queue Key Naming

The system SHALL use consistent Redis key naming conventions for translation queues.

#### Scenario: Queue key naming

- **WHEN** publishing translation jobs to Redis
- **THEN** the system SHALL use key `fusionn:translation:queue` for the main job queue
- **AND** the system SHALL use key pattern `fusionn:translation:job:{job_id}` for job metadata storage

#### Scenario: Job metadata storage

- **WHEN** enqueuing a translation job
- **THEN** the system SHALL store job metadata in Redis hash `fusionn:translation:job:{job_id}`
- **AND** the system SHALL set TTL of 7 days on the job metadata key
- **AND** after 7 days, the metadata SHALL be automatically deleted by Redis

