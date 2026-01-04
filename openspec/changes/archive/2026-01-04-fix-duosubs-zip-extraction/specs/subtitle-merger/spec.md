# subtitle-merger Specification Delta

## MODIFIED Requirements

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

