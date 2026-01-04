# Subtitle Analyzer Capability

## ADDED Requirements

### Requirement: Subtitle Track Detection

The system SHALL use ffprobe to detect all subtitle tracks embedded in video files.

#### Scenario: Multiple subtitle tracks detected

- **WHEN** ffprobe analyzes a video file with 3 subtitle tracks
- **THEN** the system SHALL return a list of 3 subtitle track objects
- **AND** each object SHALL contain track index, language code, codec, and title

#### Scenario: No subtitle tracks

- **WHEN** ffprobe analyzes a video file with no subtitle tracks
- **THEN** the system SHALL return an empty list
- **AND** the system SHALL NOT report an error

#### Scenario: ffprobe execution failure

- **WHEN** ffprobe command fails (file not found, corrupted video, permission denied)
- **THEN** the system SHALL return an error with the ffprobe stderr output
- **AND** the system SHALL log the failure with the video file path

### Requirement: English Subtitle Identification

The system SHALL identify English subtitle tracks using language codes and title heuristics with fallback priority.

#### Scenario: Standard English subtitle

- **WHEN** a subtitle track has language code "eng" or "en"
- **THEN** the system SHALL classify it as an English subtitle with priority 1 (highest)

#### Scenario: English SDH subtitle

- **WHEN** a subtitle track has language code "eng-sdh" or title contains "SDH"
- **THEN** the system SHALL classify it as an English subtitle with priority 2

#### Scenario: Title-based detection

- **WHEN** a subtitle track has no language code but title contains "English" (case-insensitive)
- **THEN** the system SHALL classify it as an English subtitle with priority 3

#### Scenario: No English subtitle found

- **WHEN** no subtitle tracks match English detection criteria
- **THEN** the system SHALL return nil for English subtitle
- **AND** the system SHALL log a warning indicating English subtitle is missing

### Requirement: Simplified Chinese Subtitle Identification

The system SHALL identify Simplified Chinese subtitle tracks using language codes and title heuristics with fallback priority.

#### Scenario: Standard Simplified Chinese subtitle

- **WHEN** a subtitle track has language code "zh-Hans", "zh-CN", "chi", or "zho"
- **THEN** the system SHALL classify it as a Simplified Chinese subtitle with priority 1 (highest)

#### Scenario: Title-based detection for Simplified Chinese

- **WHEN** a subtitle track has title containing "简体", "简中", or "CHS" (case-insensitive)
- **THEN** the system SHALL classify it as a Simplified Chinese subtitle with priority 2

#### Scenario: Traditional Chinese fallback

- **WHEN** no Simplified Chinese subtitle found but Traditional Chinese exists (language "zh-Hant" or "zh-TW")
- **THEN** the system SHALL classify it as a Traditional Chinese subtitle for conversion with priority 3
- **AND** the system SHALL mark it as requiring OpenCC conversion

#### Scenario: No Chinese subtitle found

- **WHEN** no subtitle tracks match any Chinese detection criteria
- **THEN** the system SHALL return nil for Chinese subtitle
- **AND** the system SHALL mark the job for Redis queue translation

### Requirement: Subtitle Track Extraction

The system SHALL extract selected subtitle tracks from video files to temporary SRT files using ffmpeg.

#### Scenario: Successful subtitle extraction

- **WHEN** ffmpeg extracts subtitle track 2 from a video file
- **THEN** the system SHALL create a temporary SRT file with unique name (e.g., `/tmp/subtitle-{uuid}.srt`)
- **AND** the system SHALL return the file path
- **AND** the system SHALL set appropriate file permissions (0644)

#### Scenario: Subtitle extraction failure

- **WHEN** ffmpeg fails to extract a subtitle track (unsupported codec, corrupted data)
- **THEN** the system SHALL return an error with ffmpeg stderr output
- **AND** the system SHALL log the failure with video file path and track index
- **AND** the system SHALL NOT create a partial/empty SRT file

#### Scenario: Cleanup of temporary files

- **WHEN** subtitle processing completes (success or failure)
- **THEN** the system SHALL delete all temporary extracted SRT files
- **AND** the system SHALL log if cleanup fails but SHALL NOT block processing

