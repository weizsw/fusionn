# font-embedding Specification

## Purpose
Font embedding allows ASS subtitle files to include subsetted font data, ensuring consistent rendering across systems that may lack the required fonts.

## Requirements

### Requirement: Font embedding SHALL be configurable

The system SHALL provide configuration options to enable/disable font embedding and specify the fonts directory.

#### Scenario: Font embedding is disabled by default
- **WHEN** no font embedding configuration is provided
- **THEN** font embedding SHALL NOT be performed

#### Scenario: Font embedding can be enabled
- **WHEN** `subtitle.font_embedding.enabled` is set to `true`
- **THEN** the font embedding processor SHALL be active in the pipeline

#### Scenario: Fonts directory must be configured when enabled
- **WHEN** `subtitle.font_embedding.enabled` is `true` AND `fonts_dir` is not set
- **THEN** the system SHALL log an error and disable font embedding

#### Scenario: Timeout is configurable
- **WHEN** `subtitle.font_embedding.timeout_seconds` is set
- **THEN** the font embedding operation SHALL timeout after the specified duration

### Requirement: Fonts SHALL be embedded after subtitle merge

The system SHALL embed fonts into ASS files after the merge operation and before final output.

#### Scenario: Processor runs after style application
- **WHEN** a merged ASS file is ready and styled
- **THEN** the font embedding processor SHALL attempt to embed fonts before output

#### Scenario: Processor updates the merged subtitle path
- **WHEN** font embedding succeeds
- **THEN** the processing context SHALL reference the font-embedded ASS file

#### Scenario: Original file is preserved on failure
- **WHEN** font embedding fails
- **THEN** the original non-embedded ASS file SHALL remain in the processing context

### Requirement: Font embedding SHALL use fusionn-font CLI

The system SHALL execute the fusionn-font binary to perform font subsetting and embedding.

#### Scenario: Correct command is executed
- **WHEN** font embedding is triggered
- **THEN** the system SHALL execute `fusionn-font subset {ass_file} -d {fonts_dir} --embed --output-ass {output_path}`

#### Scenario: Command includes timeout
- **WHEN** font embedding is executed
- **THEN** the operation SHALL be subject to the configured timeout

#### Scenario: Command output is logged
- **WHEN** fusionn-font executes
- **THEN** stdout and stderr SHALL be captured and logged for debugging

### Requirement: Font embedding failures SHALL be handled gracefully

The system SHALL NOT fail subtitle processing when font embedding encounters errors.

#### Scenario: Binary not found
- **WHEN** fusionn-font binary is not available in PATH
- **THEN** the system SHALL log a warning and skip font embedding

#### Scenario: Fonts directory does not exist
- **WHEN** the configured fonts_dir does not exist
- **THEN** the system SHALL log a warning and skip font embedding

#### Scenario: Font files are missing
- **WHEN** fusionn-font cannot find required font files
- **THEN** the system SHALL log a warning and continue with the original ASS file

#### Scenario: Command timeout
- **WHEN** fusionn-font execution exceeds the timeout
- **THEN** the system SHALL terminate the process, log a warning, and continue with the original ASS file

#### Scenario: Command returns non-zero exit code
- **WHEN** fusionn-font fails with an error
- **THEN** the system SHALL log the error with command output and continue with the original ASS file

### Requirement: Embedded files SHALL replace original files

The system SHALL replace the original ASS file with the embedded version when embedding succeeds.

#### Scenario: Successful embedding replaces original
- **WHEN** fusionn-font successfully creates an embedded ASS file
- **THEN** the system SHALL verify the embedded file exists and has non-zero size
- **THEN** the system SHALL replace the original ASS file with the embedded version

#### Scenario: Embedded file is validated before replacement
- **WHEN** fusionn-font creates an output file
- **THEN** the system SHALL check that the file size is greater than zero before replacement

#### Scenario: Failed validation falls back to original
- **WHEN** the embedded file does not exist or has zero size
- **THEN** the system SHALL log a warning and keep the original ASS file

### Requirement: Font embedding SHALL be included in Docker image

The fusionn-font binary SHALL be bundled in the Docker image for zero-setup deployment.

#### Scenario: Binary is downloaded during build
- **WHEN** the Docker image is built
- **THEN** fusionn-font binary SHALL be downloaded from GitHub releases

#### Scenario: Correct architecture is selected
- **WHEN** building for amd64 or arm64
- **THEN** the appropriate architecture-specific binary SHALL be downloaded

#### Scenario: Binary is executable
- **WHEN** the Docker image is built
- **THEN** fusionn-font SHALL be placed in PATH with executable permissions

#### Scenario: Build fails on unsupported architecture
- **WHEN** building for an unsupported architecture
- **THEN** the Docker build SHALL fail with a clear error message

### Requirement: Font embedding status SHALL be logged

The system SHALL provide clear logging for font embedding operations.

#### Scenario: Starting font embedding
- **WHEN** font embedding begins
- **THEN** the system SHALL log "🔤 Embedding fonts into subtitle..."

#### Scenario: Successful embedding with size reduction
- **WHEN** font embedding succeeds
- **THEN** the system SHALL log success with original and embedded file sizes

#### Scenario: Graceful failure
- **WHEN** font embedding fails for any reason
- **THEN** the system SHALL log a warning with the failure reason

#### Scenario: Binary not available at startup
- **WHEN** the application starts and fusionn-font is not found
- **THEN** the system SHALL log "⚠️ fusionn-font binary not found - font embedding disabled"

### Requirement: Font embedding SHALL support Docker volume mounts

Users SHALL be able to provide fonts via Docker volume mounts.

#### Scenario: Fonts directory is mounted
- **WHEN** users configure `-v ./fonts:/app/fonts` in docker-compose
- **THEN** the system SHALL access font files from the mounted directory

#### Scenario: Example configuration is documented
- **WHEN** users review docker-compose.yml.example
- **THEN** they SHALL see the fonts volume mount configuration
