## MODIFIED Requirements

### Requirement: DuoSubs CLI Integration

The system SHALL execute DuoSubs either as a local binary or via HTTP service to merge primary (Chinese) and secondary (English) subtitle files and extract the combined ASS file from the output.

#### Scenario: Successful subtitle merge via local binary
- **WHEN** configuration specifies `subtitle.duosubs.mode: "local"`
- **WHEN** DuoSubs CLI is invoked with primary Chinese SRT and secondary English SRT
- **THEN** the system SHALL execute `duosubs merge -p primary.srt -s secondary.srt --output-dir {output_dir} --output-name {basename} --model {configured_model} --device {configured_device}`
- **AND** the system SHALL wait for DuoSubs to complete
- **AND** the system SHALL locate the output ZIP file named `{basename}.zip` in the output directory
- **AND** the system SHALL extract the ZIP file contents
- **AND** the system SHALL locate the `{basename}_combined.ass` file from the extracted contents
- **AND** the system SHALL remove the ZIP file after successful extraction
- **AND** the system SHALL return the path to the combined ASS file

#### Scenario: Successful subtitle merge via HTTP service
- **WHEN** configuration specifies `subtitle.duosubs.mode: "http"`
- **WHEN** merge is requested with primary Chinese SRT and secondary English SRT
- **THEN** the system SHALL send HTTP POST request to configured service URL with container paths
- **AND** the system SHALL translate container paths to host paths using configured prefixes
- **AND** the system SHALL wait for HTTP response
- **AND** the system SHALL parse response JSON for output path
- **AND** the system SHALL return the output path from response

#### Scenario: HTTP service initialization with health check
- **WHEN** fusionn service starts with `subtitle.duosubs.mode: "http"`
- **THEN** the system SHALL perform health check to configured HTTP service
- **AND** if health check fails, the system SHALL log warning but continue startup
- **AND** subsequent merge requests SHALL fail with clear error if service unreachable

#### Scenario: HTTP service unreachable during merge
- **WHEN** HTTP merge is requested but service is unreachable
- **THEN** the system SHALL return error indicating service connection failure
- **AND** error message SHALL include configured service URL
- **AND** the system SHALL log connection error with details

#### Scenario: DuoSubs execution timeout
- **WHEN** DuoSubs execution exceeds the configured timeout (default: 10 minutes)
- **THEN** the system SHALL terminate the DuoSubs process (local mode) or cancel HTTP request (http mode)
- **AND** the system SHALL return a timeout error
- **AND** the system SHALL log the timeout with file paths and model name

#### Scenario: DuoSubs execution failure
- **WHEN** DuoSubs CLI exits with non-zero status (local mode) or HTTP service returns error (http mode)
- **THEN** the system SHALL capture stderr output or HTTP error message
- **AND** the system SHALL return an error with the error message
- **AND** the system SHALL log the failure with file paths and exit code or HTTP status

#### Scenario: Missing DuoSubs installation (local mode only)
- **WHEN** the system attempts to execute `duosubs` command but it is not found
- **THEN** the system SHALL return an error indicating DuoSubs is not installed
- **AND** the system SHALL log a warning that DuoSubs Python package is missing

#### Scenario: ZIP extraction failure (local mode only)
- **WHEN** the DuoSubs output ZIP file is corrupt or cannot be extracted
- **THEN** the system SHALL return an error indicating ZIP extraction failed
- **AND** the system SHALL include the ZIP path in the error message
- **AND** the system SHALL log the extraction error with details

#### Scenario: Combined file missing from ZIP (local mode only)
- **WHEN** the extracted ZIP does not contain the expected `{basename}_combined.ass` file
- **THEN** the system SHALL return an error indicating the combined file was not found
- **AND** the system SHALL list the files that were found in the ZIP
- **AND** the system SHALL log the unexpected ZIP contents

#### Scenario: ZIP file not created by DuoSubs (local mode only)
- **WHEN** DuoSubs completes successfully but does not create the expected ZIP file
- **THEN** the system SHALL return an error indicating the ZIP file was not created
- **AND** the system SHALL include the expected ZIP path in the error message
- **AND** the system SHALL log DuoSubs output for debugging

## ADDED Requirements

### Requirement: Mode Selection Configuration

The system SHALL support configuration to select between local binary execution and HTTP service for DuoSubs operations.

#### Scenario: Default to local mode
- **WHEN** no mode is specified in configuration
- **THEN** the system SHALL default to `mode: "local"` for backwards compatibility

#### Scenario: Explicit local mode configuration
- **WHEN** configuration specifies `subtitle.duosubs.mode: "local"`
- **THEN** the system SHALL execute DuoSubs as local binary
- **AND** HTTP configuration fields SHALL be ignored

#### Scenario: HTTP mode configuration
- **WHEN** configuration specifies `subtitle.duosubs.mode: "http"`
- **THEN** the system SHALL validate HTTP configuration fields are present
- **AND** if `http_url`, `http_container_prefix`, or `http_host_prefix` are missing, service initialization SHALL fail
- **AND** the system SHALL use HTTP client for all DuoSubs operations

#### Scenario: Invalid mode configuration
- **WHEN** configuration specifies unsupported mode value (not "local" or "http")
- **THEN** service initialization SHALL fail with error
- **AND** error message SHALL list valid mode values

### Requirement: Path Translation for HTTP Mode

The system SHALL translate filesystem paths between container and host coordinate systems when using HTTP mode.

#### Scenario: Container path translation
- **WHEN** using HTTP mode with `http_container_prefix: "/data"` and `http_host_prefix: "/Users/me/media"`
- **WHEN** subtitle paths are `/data/show/file.eng.srt` and `/data/show/file.chs.srt`
- **THEN** the system SHALL include these container paths in HTTP request payload
- **AND** the system SHALL include `container_prefix` and `host_prefix` in request for service-side translation

#### Scenario: Output path translation from response
- **WHEN** HTTP service returns `output_path: "/data/show/output_combined.ass"`
- **THEN** the system SHALL use this path as-is (already in container coordinate system)
- **AND** subsequent operations SHALL access file at this container path
