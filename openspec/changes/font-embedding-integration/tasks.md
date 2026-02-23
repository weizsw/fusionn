## 1. Configuration

- [x] 1.1 Add `FontEmbeddingConfig` struct to `internal/config/config.go` with `Enabled`, `FontsDir`, and `TimeoutSeconds` fields
- [x] 1.2 Add `FontEmbedding` field to `SubtitleConfig` struct
- [x] 1.3 Add default values for FontEmbeddingConfig (disabled by default, 300s timeout)
- [x] 1.4 Add validation: if enabled=true, fonts_dir must be set and exist
- [x] 1.5 Update `config/config.example.yaml` with font_embedding section and comments
- [x] 1.6 Add config tests for FontEmbeddingConfig parsing and validation

## 2. Font Embedding Processor

- [x] 2.1 Create `internal/service/subtitle/processor_font_embedding.go` with FontEmbeddingProcessor struct
- [x] 2.2 Implement `NewFontEmbeddingProcessor(cfg config.FontEmbeddingConfig)` constructor
- [x] 2.3 Implement `Name()` method returning "FontEmbedding"
- [x] 2.4 Implement `ShouldRun(pctx *ProcessingContext)` - check enabled, MergedSubPath exists, binary available
- [x] 2.5 Implement `Process(ctx context.Context, pctx *ProcessingContext)` with subprocess execution
- [x] 2.6 Add helper function to check if fusionn-font binary exists in PATH
- [x] 2.7 Add helper function to build fusionn-font command with proper arguments
- [x] 2.8 Add helper function to execute command with timeout and capture output
- [x] 2.9 Add helper function to verify and replace embedded file
- [x] 2.10 Add comprehensive error handling with graceful fallback for all failure modes

## 3. Pipeline Integration

- [x] 3.1 Update `internal/service/subtitle/service.go` to create FontEmbeddingProcessor
- [x] 3.2 Add FontEmbeddingProcessor to mergePipeline after StyleProcessor and before OutputProcessor
- [x] 3.3 Pass FontEmbeddingConfig from main config to processor constructor
- [x] 3.4 Add startup check to log warning if fusionn-font not found and feature is enabled

## 4. Unit Tests

- [x] 4.1 Create `internal/service/subtitle/processor_font_embedding_test.go`
- [x] 4.2 Test `ShouldRun()` returns false when disabled
- [x] 4.3 Test `ShouldRun()` returns false when MergedSubPath is empty
- [x] 4.4 Test `ShouldRun()` returns false when binary not found
- [x] 4.5 Test `ShouldRun()` returns true when all conditions met
- [x] 4.6 Test successful font embedding with mocked command execution
- [x] 4.7 Test graceful fallback when binary not found
- [x] 4.8 Test graceful fallback when fonts directory missing
- [x] 4.9 Test graceful fallback when command times out
- [x] 4.10 Test graceful fallback when command returns non-zero exit
- [x] 4.11 Test graceful fallback when embedded file has zero size
- [x] 4.12 Test file replacement logic when embedding succeeds

## 5. Docker Integration

- [x] 5.1 Update `Dockerfile` to add fusionn-font binary download step
- [x] 5.2 Add `ARG FUSIONN_FONT_VERSION=v1.0.2` to Dockerfile
- [x] 5.3 Add architecture detection logic using `$TARGETARCH`
- [x] 5.4 Add wget/curl command to download appropriate binary from GitHub releases
- [x] 5.5 Set executable permissions on downloaded binary
- [x] 5.6 Add verification step: `RUN fusionn-font --version`
- [x] 5.7 Handle unsupported architectures with clear error message
- [ ] 5.8 Test Docker build on amd64 and arm64 platforms

## 6. Docker Compose

- [x] 6.1 Update `docker-compose.yml` to add example fonts volume mount (commented out)
- [x] 6.2 Add comment explaining fonts directory setup
- [ ] 6.3 Test docker-compose up with and without fonts volume

## 7. Documentation

- [x] 7.1 Update README.md to add "Font Embedding" to Features list
- [x] 7.2 Add "Font Embedding" section to README after Redis Integration
- [x] 7.3 Document configuration options and volume mount setup
- [x] 7.4 Add example workflow showing automatic font embedding
- [x] 7.5 Document graceful fallback behavior
- [x] 7.6 Add troubleshooting section for common issues (missing fonts, timeout)

## 8. Logging

- [x] 8.1 Add log message when font embedding starts: "🔤 Embedding fonts into subtitle..."
- [x] 8.2 Add success log with size comparison when embedding completes
- [x] 8.3 Add warning logs for all graceful fallback scenarios
- [x] 8.4 Add startup warning if binary not found: "⚠️ fusionn-font binary not found - font embedding disabled"
- [x] 8.5 Include fusionn-font command output in error logs for debugging

## 9. Verification

- [ ] 9.1 Build Docker image and verify fusionn-font binary is included
- [ ] 9.2 Test with feature disabled - verify no behavior change
- [ ] 9.3 Test with feature enabled and fonts mounted - verify fonts are embedded
- [ ] 9.4 Test with feature enabled but no fonts directory - verify graceful fallback
- [ ] 9.5 Test with feature enabled but missing fonts - verify graceful fallback with warnings
- [ ] 9.6 Test end-to-end: webhook → subtitle processing → font-embedded output
- [ ] 9.7 Verify embedded ASS files play correctly with embedded fonts
- [ ] 9.8 Run all tests and verify CI passes
