## Why

ASS subtitle files reference external fonts by name, requiring users to install matching fonts on playback devices. This creates portability issues - subtitles may render incorrectly or use fallback fonts if the required fonts aren't available. Embedding fonts directly into ASS files eliminates this dependency, ensuring consistent rendering across all devices without manual font installation.

## What Changes

- Add new `FontEmbeddingProcessor` to the subtitle processing pipeline, running after merge and before output
- Integrate fusionn-font CLI tool (downloaded as binary from GitHub releases) to subset and embed fonts
- Add configuration section `subtitle.font_embedding` to enable/disable the feature and specify fonts directory
- Update Dockerfile to download and install fusionn-font binary during image build
- Add graceful fallback behavior - if font embedding fails, continue with non-embedded ASS file and log warning
- Update docker-compose.yml example to include fonts directory volume mount
- Add documentation for font embedding setup and usage

## Capabilities

### New Capabilities

- `font-embedding`: Automatic subsetting and embedding of fonts into ASS subtitle files, with configurable fonts directory, timeout controls, and graceful failure handling

### Modified Capabilities

None - this is a new optional feature that doesn't change existing subtitle processing behavior when disabled.

## Impact

**Code:**
- New processor: `internal/service/subtitle/processor_font_embedding.go`
- New config struct: `FontEmbeddingConfig` in `internal/config/config.go`
- Updated pipeline registration in `internal/service/subtitle/service.go`
- New tests: `internal/service/subtitle/processor_font_embedding_test.go`

**Docker:**
- Dockerfile modified to download fusionn-font binary from GitHub releases
- Image size increase: ~10-15MB (single binary, no Python dependencies)

**Configuration:**
- New optional config section `subtitle.font_embedding` with `enabled`, `fonts_dir`, and `timeout_seconds` fields

**Dependencies:**
- External: fusionn-font binary (bundled in Docker image)
- No new Go dependencies required (uses standard library subprocess execution)

**User-facing:**
- Users must mount fonts directory as volume if enabling this feature
- Backward compatible - disabled by default, existing deployments unaffected
