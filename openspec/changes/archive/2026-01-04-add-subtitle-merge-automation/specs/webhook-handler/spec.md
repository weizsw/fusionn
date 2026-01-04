# Webhook Handler Capability

## ADDED Requirements

### Requirement: Sonarr Webhook Reception

The system SHALL accept webhook POST requests from Sonarr at `/api/v1/webhook/sonarr` containing media import event data.

#### Scenario: Sonarr episode import

- **WHEN** Sonarr sends a POST request with episode import payload containing `episodeFile.path`
- **THEN** the system SHALL extract the absolute file path from `episodeFile.path`
- **AND** the system SHALL parse series title from `series.title` and episode metadata from `episodes` array
- **AND** the system SHALL enqueue a subtitle processing job with the file path
- **AND** the system SHALL respond with HTTP 202 Accepted

#### Scenario: Invalid Sonarr payload

- **WHEN** Sonarr sends a POST request with malformed JSON
- **THEN** the system SHALL respond with HTTP 400 Bad Request
- **AND** the system SHALL log the validation error

#### Scenario: Missing required fields

- **WHEN** Sonarr sends a payload without the required `eventType` or `series` fields
- **THEN** the system SHALL respond with HTTP 400 Bad Request
- **AND** the system SHALL return a JSON error message indicating the missing fields

### Requirement: Radarr Webhook Reception

The system SHALL accept webhook POST requests from Radarr at `/api/v1/webhook/radarr` containing media import event data.

#### Scenario: Radarr movie import

- **WHEN** Radarr sends a POST request with movie import payload containing `movieFile.path`
- **THEN** the system SHALL extract the absolute file path from `movieFile.path`
- **AND** the system SHALL parse movie title from `movie.title`, year from `movie.year`, and IMDB ID from `movie.imdbId`
- **AND** the system SHALL enqueue a subtitle processing job with the file path
- **AND** the system SHALL respond with HTTP 202 Accepted

#### Scenario: Invalid Radarr payload

- **WHEN** Radarr sends a POST request with malformed JSON
- **THEN** the system SHALL respond with HTTP 400 Bad Request
- **AND** the system SHALL log the validation error

#### Scenario: Missing required fields

- **WHEN** Radarr sends a payload without the required `eventType` or `movie` fields
- **THEN** the system SHALL respond with HTTP 400 Bad Request
- **AND** the system SHALL return a JSON error message indicating the missing fields

### Requirement: Webhook Payload Validation

The system SHALL validate webhook payloads before processing to ensure data integrity.

#### Scenario: Valid event type

- **WHEN** a webhook is received with `eventType: "Download"` or `eventType: "Grab"`
- **THEN** the system SHALL accept the webhook for processing

#### Scenario: Ignored event type

- **WHEN** a webhook is received with an unsupported `eventType` (e.g., "Test", "Rename")
- **THEN** the system SHALL respond with HTTP 200 OK without processing
- **AND** the system SHALL log that the event type is ignored

#### Scenario: File path validation

- **WHEN** a webhook contains a video file path
- **THEN** the system SHALL verify the file exists on the filesystem
- **AND** if the file does not exist, the system SHALL respond with HTTP 404 Not Found

### Requirement: Asynchronous Job Enqueueing

The system SHALL enqueue subtitle processing jobs asynchronously to prevent webhook timeouts.

#### Scenario: Job enqueue success

- **WHEN** a valid webhook is processed
- **THEN** the system SHALL create a job with a unique UUID
- **AND** the system SHALL add the job to the processing queue
- **AND** the system SHALL respond with HTTP 202 Accepted and the job ID

#### Scenario: Job enqueue failure

- **WHEN** the job queue is full or unavailable
- **THEN** the system SHALL respond with HTTP 503 Service Unavailable
- **AND** the system SHALL log the queue failure error

