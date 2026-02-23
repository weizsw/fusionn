## Context

fusionn currently generates dual-language ASS subtitle files by merging English and Chinese subtitles through a pipeline of processors. These ASS files reference fonts by name (e.g., "WenQuanYi Micro Hei"), requiring users to manually install matching fonts on playback devices for correct rendering.

The fusionn-font tool (separate Python project) provides CLI functionality to analyze ASS files, subset fonts to only used characters, and embed them directly into ASS files using the UUEncode format. This integration will make fusionn-font available within fusionn's Docker deployment and automate font embedding as part of the subtitle processing pipeline.

**Current Pipeline:**
```
Merge Pipeline:
1. ConversionProcessor (OpenCC traditional→simplified)
2. MergerProcessor (DuoSubs merge → ASS)
3. StyleProcessor (Apply ASS styling)
4. OutputProcessor (Move to final location)
5. NotificationProcessor (Send notifications)
6. CleanupProcessor (Remove temp files)
```

**Constraints:**
- Must maintain backward compatibility (disabled by default)
- Must not fail subtitle processing if font embedding fails
- Must work seamlessly in Docker without Python installation
- Must follow existing processor pattern and pipeline architecture

## Goals / Non-Goals

**Goals:**
- Automatically embed fonts into merged ASS files when enabled
- Provide simple configuration (just enable + specify fonts directory)
- Gracefully handle failures (missing fonts, tool errors) without breaking pipeline
- Bundle fusionn-font binary in Docker image for zero-setup deployment
- Reduce font file sizes through automatic subsetting (typically 90%+ reduction)

**Non-Goals:**
- Not implementing font subsetting/embedding logic in Go (reuse fusionn-font)
- Not supporting system font auto-discovery (users provide fonts directory)
- Not embedding fonts in non-ASS subtitle formats (SRT, VTT)
- Not validating font coverage before embedding (fusionn-font handles this)
- Not creating HTTP service for fusionn-font (direct CLI execution is simpler)

## Decisions

### 1. Integration Method: Direct CLI Execution

**Decision:** Execute fusionn-font as a subprocess using Go's `os/exec` package.

**Alternatives considered:**
- HTTP service (like duosubs-service): Adds complexity, network overhead, and container management for minimal benefit
- Embed Python in Go: Requires cgo and Python runtime, increases image size significantly
- Rewrite in Go: Large effort to port fonttools-based subsetting logic

**Rationale:** CLI execution is simplest and most reliable. The font embedding operation is fast (typically <30 seconds) and doesn't need request/response patterns or concurrency control that would justify an HTTP service.

### 2. Binary Distribution: GitHub Releases

**Decision:** Download pre-built fusionn-font binary from GitHub releases during Docker build.

**Alternatives considered:**
- Install Python + pip install fusionn-font: Adds 60-95MB to image vs 10-15MB for binary
- Build from source in Docker: Slower builds, requires Python toolchain

**Rationale:** Pre-built binaries (created with PyInstaller) are standalone with zero runtime dependencies. This minimizes image size and build time.

### 3. Pipeline Position: After Style, Before Output

**Decision:** Insert FontEmbeddingProcessor between StyleProcessor and OutputProcessor.

```
Updated Pipeline:
3. StyleProcessor (Apply ASS styling)
4. FontEmbeddingProcessor (NEW - embed fonts)
5. OutputProcessor (Move to final location)
```

**Rationale:** 
- Must run after MergerProcessor (needs final ASS file)
- Must run after StyleProcessor (fonts may be referenced in style overrides)
- Must run before OutputProcessor (final file should already have fonts embedded)
- Running before cleanup ensures temp directory is still available

### 4. Error Handling: Graceful Fallback

**Decision:** If font embedding fails (missing binary, missing fonts, timeout, etc.), log warning and continue with original non-embedded ASS file. Never fail the entire pipeline.

**Alternatives considered:**
- Fail pipeline on error: Breaks subtitle processing for users without fonts
- Strict mode configuration: Adds complexity for minimal benefit

**Rationale:** Font embedding is an enhancement, not a requirement. Subtitles without embedded fonts still work (users just need fonts installed). Graceful fallback maximizes availability while still providing the feature when possible.

### 5. Configuration: Simple Enable + Directory

**Decision:** Minimal configuration with just `enabled`, `fonts_dir`, and `timeout_seconds`.

```yaml
subtitle:
  font_embedding:
    enabled: false
    fonts_dir: "/app/fonts"
    timeout_seconds: 300
```

**Rationale:** 
- Binary path is implementation detail (hardcoded lookup in PATH)
- No need for per-font configuration (fusionn-font auto-matches by family name)
- Timeout provides safety against hung processes
- Disabled by default maintains backward compatibility

### 6. File Handling: In-Place Replacement

**Decision:** Create embedded version as temp file, then replace original if successful.

**Process:**
```
1. Input: /path/.fusionn-merge-{uuid}/output.ass
2. Run: fusionn-font subset output.ass -d /fonts --embed --output-ass output.embedded.ass
3. Verify: Check output.embedded.ass exists and size > 0
4. Replace: mv output.embedded.ass output.ass
5. Context: pctx.MergedSubPath still points to same location (now embedded)
```

**Rationale:** Simple atomic operation. If embedding fails at any step, original file is untouched. CleanupProcessor handles final cleanup of temp directory.

## Risks / Trade-offs

**[Risk] fusionn-font binary architecture mismatch**
→ **Mitigation:** Dockerfile uses `$TARGETARCH` to download correct binary (amd64/arm64). Build fails explicitly if unsupported architecture.

**[Risk] Fonts directory not mounted or empty**
→ **Mitigation:** Processor checks directory exists before running. Logs clear error if missing. ShouldRun() returns false to skip gracefully.

**[Risk] Font files don't match fonts used in ASS**
→ **Mitigation:** fusionn-font logs which fonts were found/missing. Processor logs this output. Embedding still succeeds (just skips unavailable fonts).

**[Risk] Large font files cause timeout**
→ **Mitigation:** Configurable timeout (default 5 minutes). Subsetting typically reduces size by 90%+ so execution is fast. If timeout occurs, fall back to non-embedded file.

**[Risk] fusionn-font has breaking changes in future releases**
→ **Mitigation:** Pin specific version in Dockerfile (`v1.0.3`). Update explicitly when tested. CLI interface is stable.

**[Trade-off] Binary vs source installation**
Chose binary for smaller image size, but means we can't easily patch fusionn-font code if issues arise. Acceptable trade-off given tool stability.

**[Trade-off] Direct CLI vs HTTP service**
Chose CLI for simplicity, but means each subtitle processing job spawns a subprocess. Acceptable overhead given fast execution and infrequent processing.

**[Trade-off] Disabled by default**
Users must explicitly enable and configure fonts directory. This is safer for rollout but means users won't get font embedding "for free". Acceptable since fonts directory requires user setup anyway.

## Migration Plan

**Phase 1: Development**
1. Implement FontEmbeddingProcessor with full test coverage
2. Update config structs and validation
3. Update Dockerfile to download fusionn-font binary
4. Test with real ASS files and various font scenarios

**Phase 2: Documentation**
1. Update README with font embedding section
2. Add config.example.yaml entries
3. Update docker-compose.yml example with fonts volume mount
4. Document troubleshooting (missing fonts, timeout issues)

**Phase 3: Deployment**
1. Build new Docker image with fusionn-font bundled
2. Deploy with feature disabled by default (backward compatible)
3. Users opt-in by setting `subtitle.font_embedding.enabled: true` and mounting fonts directory

**Rollback Strategy:**
- Feature is disabled by default, so rollback is no-op
- If issues arise, users can disable with `enabled: false`
- Previous Docker images remain available and functional

**Validation:**
- Verify fusionn-font binary included in image: `docker run fusionn fusionn-font --version`
- Test with fonts mounted: Check logs for "✅ Fonts embedded successfully"
- Test without fonts: Verify graceful fallback with warning logs
- Test with feature disabled: Verify no change in behavior

## Open Questions

None - design is complete and ready for implementation.
