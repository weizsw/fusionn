## Why

Current Apprise notifications lack essential details about subtitle processing outcomes. Users receive generic messages about merges/queues without knowing whether Chinese subtitles were extracted or translated, or if traditional conversion occurred. This makes it difficult to understand what processing actually happened.

## What Changes

- Add processing type indicators (extracted vs. translated Chinese subtitles)
- Add traditional conversion status when applicable
- Improve message formatting for better readability and structure
- Maintain backward compatibility with existing notification types (success, info, warning)

## Capabilities

### New Capabilities

None - this is an enhancement to existing notification behavior.

### Modified Capabilities

- `notification-formatting`: Update notification message structure to include filename, Chinese subtitle source (extracted/translated), and traditional conversion status with improved readability

## Impact

- `internal/service/subtitle/processor_notification.go`: Modify notification message construction to include additional context fields
- `internal/notification/apprise.go`: Potentially add message formatting helpers (if needed)
- No breaking changes to API or configuration
- No new dependencies required
