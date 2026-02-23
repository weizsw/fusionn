## 1. Logger Package Updates

- [x] 1.1 Add `Indent()` method to logger package that returns a prefixed logger
- [x] 1.2 Add indent prefix constant (e.g., `  ├─ `) to logger package
- [x] 1.3 Add convenience methods for indented logging (InfoIndent, ErrorIndent, etc.)
- [x] 1.4 Write tests for indent functionality

## 2. Pipeline Coordination

- [x] 2.1 Add processor emoji mapping in pipeline.go
- [x] 2.2 Update pipeline processor start log to include emoji prefix
- [x] 2.3 Update pipeline processor completion log to include emoji prefix
- [x] 2.4 Update pipeline processor skip log to include emoji prefix (if applicable)

## 3. Processor Updates - Core

- [x] 3.1 Update AnalyzerProcessor to use indented logger for sub-logs
- [x] 3.2 Update ExtractorProcessor to use indented logger for sub-logs
- [x] 3.3 Update ConversionProcessor to use indented logger for sub-logs
- [x] 3.4 Update MergerProcessor to use indented logger for sub-logs

## 4. Processor Updates - Styling & Output

- [x] 4.1 Update StyleProcessor to use indented logger for sub-logs
- [x] 4.2 Update FontEmbeddingProcessor to use indented logger for sub-logs
- [x] 4.3 Update OutputProcessor to use indented logger for sub-logs

## 5. Processor Updates - Notifications & Cleanup

- [x] 5.1 Update NotificationProcessor to use indented logger for sub-logs
- [x] 5.2 Update CleanupProcessor to use indented logger for sub-logs
- [x] 5.3 Update TranslationQueueProcessor to use indented logger for sub-logs

## 6. Validation & Testing

- [x] 6.1 Run full pipeline with test media to verify log formatting
- [x] 6.2 Verify emoji rendering in Docker container logs
- [x] 6.3 Check that timestamps and job IDs remain parseable
- [x] 6.4 Update any log parsing scripts/tools if necessary
