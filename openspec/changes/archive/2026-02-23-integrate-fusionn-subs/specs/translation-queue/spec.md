# translation-queue Specification Delta

## MODIFIED Requirements

### Requirement: Translation Job Publishing

The system SHALL publish translation job messages to Redis queue when Chinese subtitles are missing.

#### Scenario: Enqueue translation job

- **WHEN** subtitle analyzer finds no Chinese subtitle in video
- **THEN** the system SHALL create a translation job message with:
  - `job_id` (string, UUID v4)
  - `video_path` (string, absolute path to video file)
  - `subtitle_path` (string, absolute path to extracted English subtitle)
  - `media_title` (string, formatted media name)
  - `media_type` (string, "movie" or "episode")
- **AND** the system SHALL serialize the message to JSON
- **AND** the system SHALL LPUSH the message to Redis list key configured in `redis.queue_key`
- **AND** the system SHALL log the job ID and enqueue timestamp

#### Scenario: Translation job enqueue failure

- **WHEN** Redis LPUSH operation fails (connection error, out of memory)
- **THEN** the system SHALL log an error with job details
- **AND** the system SHALL NOT retry automatically (avoid infinite loops)
- **AND** the system SHALL continue processing (non-blocking failure)

#### Scenario: Translation job already exists

- **WHEN** a translation job for the same video file might already be in the queue
- **THEN** the system SHALL enqueue the job without duplicate checking
- **AND** fusionn-subs SHALL handle duplicate processing idempotently

### Requirement: Translation Job Message Schema

The system SHALL use a standardized JSON schema for translation job messages compatible with fusionn-subs.

#### Scenario: Job message schema validation

- **WHEN** creating a translation job message
- **THEN** the message SHALL contain exactly these fields:
  - `job_id` (string, UUID format)
  - `video_path` (string, absolute path)
  - `subtitle_path` (string, absolute path to English subtitle)
  - `media_title` (string, human-readable media name)
  - `media_type` (string, enum: "movie" or "episode")
- **AND** the message SHALL be valid JSON
- **AND** the message SHALL NOT include callback_url (configured in fusionn-subs)

#### Scenario: Schema evolution compatibility

- **WHEN** a new optional field is added to the job schema in the future
- **THEN** existing fusionn-subs consumers SHALL ignore unknown fields (forward compatibility)
- **AND** required fields SHALL NOT be removed (backward compatibility)

## REMOVED Requirements

### Requirement: Translation Callback Reception

**Reason**: Callback handling moved to dedicated `translation-callback` capability

**Migration**: Use `/api/v1/callback/translation` endpoint defined in translation-callback spec

### Requirement: Redis Queue Key Naming

**Reason**: Queue key naming simplified - only main queue used, no job metadata storage

**Migration**: Use `redis.queue_key` config value (default: `fusionn:translation_queue`)
