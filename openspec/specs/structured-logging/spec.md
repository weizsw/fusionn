# structured-logging Specification

## Purpose
TBD - created by syncing delta from improve-log-readability change. Update Purpose after archive.

## Requirements

### Requirement: Processor emoji identification
Each processor in the subtitle pipeline SHALL be identified with a unique emoji prefix in log output. The emoji SHALL appear before the processor name in all processor-level log messages.

#### Scenario: Processor start log includes emoji
- **WHEN** the pipeline starts a processor
- **THEN** the log message SHALL display the processor's emoji followed by its name (e.g., "🔍 Running processor: AnalyzerProcessor")

#### Scenario: Processor completion log includes emoji
- **WHEN** the pipeline completes a processor successfully
- **THEN** the log message SHALL display the processor's emoji in the completion message

### Requirement: Processor emoji mapping
The system SHALL maintain a consistent emoji mapping for all processors.

#### Scenario: Standard processor emojis
- **WHEN** any processor runs
- **THEN** the system SHALL use the following emoji mappings:
  - AnalyzerProcessor: 🔍 (magnifying glass - analyzing/detecting)
  - Extractor: 📤 (outbox - extracting files)
  - Conversion: 🔄 (counterclockwise arrows - converting formats)
  - Merger: 🔀 (twisted arrows - merging subtitles)
  - Style: 🎨 (artist palette - styling)
  - FontEmbedding: 🔤 (letters - font handling)
  - Output: 💾 (floppy disk - saving files)
  - Notification: 📢 (loudspeaker - sending notifications)
  - Cleanup: 🧹 (broom - cleaning up)
  - TranslationQueue: 🌐 (globe - translation/international)

### Requirement: Indented sub-logs
Log messages emitted from within a processor (sub-logs) SHALL be visually indented to indicate they belong to the currently executing processor.

#### Scenario: Sub-log indentation
- **WHEN** a processor emits a log message during its execution
- **THEN** the log message SHALL be prefixed with an indent marker (e.g., "  ├─ ")

#### Scenario: Sub-log hierarchy visibility
- **WHEN** viewing consecutive log lines
- **THEN** indented sub-logs SHALL be visually grouped under their parent processor's start message

### Requirement: Indent helper method
The logger package SHALL provide a method for creating indented loggers that automatically prefix log messages.

#### Scenario: Creating indented logger
- **WHEN** a processor needs to emit sub-logs
- **THEN** the processor SHALL be able to obtain an indented logger instance that automatically adds indent prefixes to all log calls

#### Scenario: Indent prefix format
- **WHEN** using an indented logger
- **THEN** log messages SHALL be prefixed with "  ├─ " (2 spaces + tree character + dash + space)

### Requirement: Backward compatibility
Log format changes SHALL NOT break existing log parsing or monitoring integrations.

#### Scenario: Timestamp and level preservation
- **WHEN** emitting logs with new formatting
- **THEN** timestamp format and log level indicators SHALL remain unchanged

#### Scenario: Job ID preservation
- **WHEN** emitting logs with new formatting
- **THEN** job IDs SHALL continue to appear in the same format for correlation
