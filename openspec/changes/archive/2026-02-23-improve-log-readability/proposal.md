## Why

Current processor logs lack visual hierarchy - all processors use the same format without distinguishing markers, and sub-logs (detailed steps within processors) aren't indented. This makes it difficult to quickly identify which pipeline stage is executing or troubleshoot specific processor issues in production logs.

## What Changes

- Add processor-specific emoji prefixes to make each processor visually distinct at a glance
- Indent sub-logs (detailed logs within processors) to create visual hierarchy
- Maintain job IDs and timestamps for traceability while improving scanability
- Apply consistent formatting across all 11 processors (Analyzer, Extractor, Conversion, Merger, Style, FontEmbedding, Output, Notification, Cleanup, TranslationQueue, and any future processors)

## Capabilities

### New Capabilities
- `structured-logging`: Log formatting with visual hierarchy, processor-specific emojis, and indented sub-logs

### Modified Capabilities

## Impact

- `pkg/logger`: Add indent functionality to logger helpers
- `internal/service/subtitle/processor_*.go`: Update all 11+ processor implementations to use emoji prefixes and indented sub-logs
- `internal/service/subtitle/pipeline.go`: Coordinate processor-level vs sub-log formatting
- Log output format changes (non-breaking - log parsing should remain compatible)
