# subtitle-merger Specification

## Purpose
TBD - created by archiving change add-subtitle-merge-automation. Update Purpose after archive.
## Requirements
### Requirement: DuoSubs CLI Integration

The system SHALL execute DuoSubs Python CLI to merge primary (Chinese) and secondary (English) subtitle files and extract the combined ASS file from the output ZIP.

#### Scenario: Successful subtitle merge

- **WHEN** DuoSubs CLI is invoked with primary Chinese SRT and secondary English SRT
- **THEN** the system SHALL execute `duosubs merge -p primary.srt -s secondary.srt --output-dir {output_dir} --output-name {basename} --model {configured_model} --device {configured_device}`
- **AND** the system SHALL wait for DuoSubs to complete
- **AND** the system SHALL locate the output ZIP file named `{basename}.zip` in the output directory
- **AND** the system SHALL extract the ZIP file contents
- **AND** the system SHALL locate the `{basename}_combined.ass` file from the extracted contents
- **AND** the system SHALL remove the ZIP file after successful extraction
- **AND** the system SHALL return the path to the combined ASS file

#### Scenario: DuoSubs execution timeout

- **WHEN** DuoSubs execution exceeds the configured timeout (default: 10 minutes)
- **THEN** the system SHALL terminate the DuoSubs process
- **AND** the system SHALL return a timeout error
- **AND** the system SHALL log the timeout with file paths and model name

#### Scenario: DuoSubs execution failure

- **WHEN** DuoSubs CLI exits with non-zero status
- **THEN** the system SHALL capture stderr output
- **AND** the system SHALL return an error with the stderr message
- **AND** the system SHALL log the failure with file paths and exit code

#### Scenario: Missing DuoSubs installation

- **WHEN** the system attempts to execute `duosubs` command but it is not found
- **THEN** the system SHALL return an error indicating DuoSubs is not installed
- **AND** the system SHALL log a warning that DuoSubs Python package is missing

#### Scenario: ZIP extraction failure

- **WHEN** the DuoSubs output ZIP file is corrupt or cannot be extracted
- **THEN** the system SHALL return an error indicating ZIP extraction failed
- **AND** the system SHALL include the ZIP path in the error message
- **AND** the system SHALL log the extraction error with details

#### Scenario: Combined file missing from ZIP

- **WHEN** the extracted ZIP does not contain the expected `{basename}_combined.ass` file
- **THEN** the system SHALL return an error indicating the combined file was not found
- **AND** the system SHALL list the files that were found in the ZIP
- **AND** the system SHALL log the unexpected ZIP contents

#### Scenario: ZIP file not created by DuoSubs

- **WHEN** DuoSubs completes successfully but does not create the expected ZIP file
- **THEN** the system SHALL return an error indicating the ZIP file was not created
- **AND** the system SHALL include the expected ZIP path in the error message
- **AND** the system SHALL log DuoSubs output for debugging

### Requirement: Traditional to Simplified Chinese Conversion

The system SHALL convert Traditional Chinese subtitles to Simplified Chinese using OpenCC when Simplified Chinese subtitles are unavailable.

#### Scenario: Successful Traditional to Simplified conversion

- **WHEN** a Traditional Chinese SRT file is provided for conversion
- **THEN** the system SHALL execute OpenCC with `t2s.json` config (Traditional to Simplified)
- **AND** the system SHALL create a new Simplified Chinese SRT file
- **AND** the system SHALL preserve all subtitle timing and formatting
- **AND** the system SHALL return the path to the converted SRT file

#### Scenario: OpenCC conversion failure

- **WHEN** OpenCC execution fails (corrupted file, encoding issues)
- **THEN** the system SHALL return an error with OpenCC stderr output
- **AND** the system SHALL log the failure with input file path

#### Scenario: Skip conversion when Simplified Chinese exists

- **WHEN** both Traditional and Simplified Chinese subtitles are available
- **THEN** the system SHALL prioritize Simplified Chinese
- **AND** the system SHALL NOT invoke OpenCC conversion

### Requirement: Subtitle Merging Configuration

The system SHALL support configurable DuoSubs model selection and merging behavior.

#### Scenario: Custom model configuration

- **WHEN** the configuration specifies `subtitle.duosubs.model: "Qwen/Qwen3-Embedding-0.6B"`
- **THEN** the system SHALL pass `--model Qwen/Qwen3-Embedding-0.6B` to DuoSubs CLI

#### Scenario: Default model fallback

- **WHEN** no model is specified in configuration
- **THEN** the system SHALL use `sentence-transformers/LaBSE` as the default model (commonly used, multilingual support)

#### Scenario: Device configuration (CPU/GPU)

- **WHEN** the configuration specifies `subtitle.duosubs.device: "cuda"`
- **THEN** the system SHALL set `CUDA_VISIBLE_DEVICES` environment variable before executing DuoSubs
- **AND** DuoSubs SHALL use GPU acceleration

### Requirement: Merged Subtitle Output

The system SHALL save merged subtitle files to the configured output directory with appropriate naming.

#### Scenario: Save merged subtitle to video directory

- **WHEN** DuoSubs produces a merged ASS file
- **THEN** the system SHALL copy the file to the same directory as the video file
- **AND** the system SHALL name the file `{video_basename}_bilingual.ass`
- **AND** the system SHALL set file permissions to 0644

#### Scenario: Video directory write permission

- **WHEN** the video directory is not writable
- **THEN** the system SHALL return an error indicating insufficient permissions
- **AND** the system SHALL log the directory path and permission error

#### Scenario: Overwrite existing subtitle

- **WHEN** a merged subtitle file already exists at the output path
- **THEN** the system SHALL overwrite the existing file
- **AND** the system SHALL log a warning that the file was overwritten

### Requirement: Fallback Logic for Missing Subtitles

The system SHALL implement a prioritized fallback strategy for selecting subtitle tracks.

#### Scenario: English subtitle fallback order

- **WHEN** analyzing English subtitles
- **THEN** the system SHALL prefer standard English (priority 1)
- **AND** if unavailable, SHALL use English SDH (priority 2)
- **AND** if unavailable, SHALL use title-based detection (priority 3)

#### Scenario: Chinese subtitle fallback order

- **WHEN** analyzing Chinese subtitles
- **THEN** the system SHALL prefer Simplified Chinese (priority 1)
- **AND** if unavailable, SHALL use Traditional Chinese + OpenCC conversion (priority 2)
- **AND** if unavailable, SHALL trigger Redis translation queue (priority 3)

#### Scenario: No fallback available

- **WHEN** no English subtitle is found in any fallback tier
- **THEN** the system SHALL abort subtitle merging
- **AND** the system SHALL log an error indicating English subtitle is required for merging
- **AND** the system SHALL NOT enqueue a translation job (English assumed mandatory)

