# subtitle-analyzer Specification Delta

## MODIFIED Requirements

### Requirement: Subtitle Track Extraction

The system SHALL extract selected subtitle tracks from video files to SRT files in the media directory using ffmpeg.

#### Scenario: Successful subtitle extraction

- **WHEN** ffmpeg extracts subtitle track from a video file
- **THEN** the system SHALL determine the output path as:
  - Video directory: `filepath.Dir(videoPath)`
  - Video base name: `filepath.Base(videoPath)` without extension
  - Subtitle suffix: configured value (default: "eng")
  - Full path: `<video-dir>/<video-name>.<suffix>.srt`
- **AND** the system SHALL execute ffmpeg to extract to that path
- **AND** the system SHALL return the file path
- **AND** the system SHALL set appropriate file permissions (0644)

#### Scenario: Subtitle extraction failure

- **WHEN** ffmpeg fails to extract a subtitle track (unsupported codec, corrupted data)
- **THEN** the system SHALL return an error with ffmpeg stderr output
- **AND** the system SHALL log the failure with video file path and track index
- **AND** the system SHALL NOT create a partial/empty SRT file

#### Scenario: Extraction path collision

- **WHEN** a subtitle file already exists at the extraction path
- **THEN** the system SHALL overwrite the existing file
- **AND** the system SHALL log a warning about overwriting

#### Scenario: Cleanup of extracted files

- **WHEN** subtitle merge completes successfully
- **THEN** the cleanup processor SHALL delete extracted subtitle files (.eng.srt, .chs.srt)
- **AND** the system SHALL keep only the final merged .ass file
- **AND** the system SHALL log cleanup actions

### Requirement: Extracted Subtitle Naming

The system SHALL use standardized naming conventions for extracted subtitle files.

#### Scenario: English subtitle naming

- **WHEN** extracting an English subtitle track
- **THEN** the system SHALL use suffix from `subtitle.extracted_subtitle_suffix` config (default: "eng")
- **AND** the output file SHALL be named `<video-name>.eng.srt`

#### Scenario: Chinese subtitle naming

- **WHEN** extracting a Chinese subtitle track
- **THEN** the system SHALL use the same base name as the video
- **AND** the output file SHALL be named `<video-name>.chs.srt` (if extracted, though typically comes from translation)

#### Scenario: Configurable suffix

- **WHEN** config specifies a custom `extracted_subtitle_suffix` value
- **THEN** the system SHALL use that suffix instead of "eng"
- **AND** the system SHALL maintain the pattern `<video-name>.<suffix>.srt`
