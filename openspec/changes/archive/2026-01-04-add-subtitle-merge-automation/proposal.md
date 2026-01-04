# Proposal: Subtitle Merge Automation

## Why

### Problem Statement

Users want bilingual (Chinese + English) subtitles automatically created for imported media from Sonarr and Radarr. Current workflow requires manual subtitle extraction and merging, which is time-consuming and error-prone.

### User Impact

- **Time Savings**: Automated subtitle merging eliminates manual work (5-10 minutes per video)
- **Consistency**: Standardized bilingual subtitle output for all media
- **Better Viewing Experience**: High-quality semantic alignment via DuoSubs
- **Extensibility**: Pipeline architecture allows adding future features (ASS styling, timing, filters)

### Business Value

- Improves media automation workflow completeness
- Demonstrates advanced subtitle processing capabilities
- Enables integration with translation services (fusionn-subs)
- Provides foundation for future subtitle enhancement features

## What Changes

### New Capabilities

1. **webhook-handler** (5 requirements, 13 scenarios)
   - Sonarr webhook endpoint (`POST /api/v1/webhook/sonarr`)
   - Radarr webhook endpoint (`POST /api/v1/webhook/radarr`)
   - Parse payload and extract file paths (`episodeFile.path`, `movieFile.path`)
   - Validate event types (filter "Download", "Upgrade" only)
   - Enqueue subtitle processing jobs

2. **subtitle-analyzer** (4 requirements, 13 scenarios)
   - Detect subtitle tracks using ffprobe
   - Identify English subtitles with fallback (`eng` → `eng-sdh`)
   - Identify Chinese subtitles with fallback (`zh-Hans` → `zh-Hant`)
   - Extract subtitle tracks to SRT files using ffmpeg

3. **subtitle-merger** (5 requirements, 16 scenarios)
   - Convert Traditional → Simplified Chinese using OpenCC
   - Merge English + Chinese subtitles using DuoSubs CLI
   - Handle DuoSubs model selection (LaBSE default)
   - Save merged subtitle to video directory
   - Cleanup temporary files

4. **translation-queue** (5 requirements, 15 scenarios)
   - Publish translation jobs to Redis queue if Chinese missing
   - Callback API endpoint (`POST /api/v1/callback/translation`)
   - Retry subtitle merge after translation completion
   - Handle translation queue errors gracefully
   - Mock fusionn-subs for testing

### Configuration Changes

```yaml
# New configuration sections
subtitle:
  enabled: true
  duosubs:
    model: "sentence-transformers/LaBSE"
    device: "auto"
    timeout_minutes: 10
  output_same_dir: true
  english_variants: ["eng", "eng-sdh", "en"]
  chinese_variants: ["zh-Hans", "zh-CN", "chi", "zho"]
  opencc:
    enabled: true
    config: "t2s.json"

redis:
  host: "redis"
  port: 6379
  password: ""
  database: 0
  queue_key: "fusionn:translation:queue"

apprise:
  enabled: true
  base_url: "http://apprise:8000"
  key: "apprise"
  tag: "fusionn-subtitle"
```

### Architecture: Extensible Pipeline

The system is built on a **Pipeline Architecture** that supports easy addition of new processing steps:

**Core Design:**
- **Processor Interface**: Each step is a self-contained processor
- **ProcessingContext**: State flows through pipeline
- **Conditional Execution**: Processors decide when to run
- **Configuration-Driven**: Enable/disable processors via config

**Current Processors (Phase 1):**
1. AnalyzerProcessor - Detect subtitle tracks
2. ExtractorProcessor - Extract to SRT files
3. ConversionProcessor - Traditional → Simplified (OpenCC)
4. MergerProcessor - Merge with DuoSubs
5. OutputProcessor - Copy to video directory
6. NotificationProcessor - Apprise alerts
7. CleanupProcessor - Remove temp files
8. TranslationQueueProcessor - Queue if Chinese missing

**Future Processors (Extensible):**
- **StyleProcessor** - Modify ASS fonts/colors/positioning
- **TimingProcessor** - Adjust timing offsets/speed
- **FilterProcessor** - Remove ads/credits
- **QualityCheckProcessor** - Validate timing/encoding
- **BackupProcessor** - Backup original subtitles

See `ARCHITECTURE.md` for detailed design.

### Dependencies

- **External Tools**: ffmpeg, ffprobe (for subtitle extraction)
- **Python Packages**: DuoSubs, OpenCC (for subtitle processing)
- **Go Packages**: `github.com/google/uuid` (for job ID generation)
- **Services**: Redis (for translation queue), Apprise (for notifications)

### Testing Strategy

- **Unit Tests**: Test each processor independently
- **Integration Tests**: Test full pipeline with sample video files
- **Mock fusionn-subs**: Use mock server for testing translation callback
- **Table-Driven Tests**: Cover all webhook payload variations
- **Race Detection**: Run tests with `-race` flag

## Impact

### User-Facing Changes

- ✅ **New Webhook Endpoints**: `/api/v1/webhook/sonarr`, `/api/v1/webhook/radarr`
- ✅ **New Callback Endpoint**: `/api/v1/callback/translation` (for fusionn-subs)
- ✅ **Automatic Subtitle Creation**: Bilingual subtitles saved to video directory
- ✅ **Apprise Notifications**: Success/failure alerts for subtitle merge

### Migration Required

- **Configuration**: Add new `subtitle`, `redis`, `apprise` sections to config.yaml
- **Docker Image**: Install ffmpeg, Python, DuoSubs, OpenCC in container
- **Redis Setup**: Configure Redis connection for translation queue
- **Apprise Setup**: Configure Apprise for notifications (optional)

### Backward Compatibility

- ✅ **No Breaking Changes**: New feature is opt-in via configuration
- ✅ **Graceful Degradation**: If DuoSubs fails, log error and send notification
- ✅ **Existing Endpoints Unchanged**: No modifications to existing API routes

### Rollout Plan

**Phase 1: Core Implementation** (Current)
- Configuration schema
- Webhook handlers
- Subtitle analyzer
- Subtitle merger with pipeline
- Translation queue
- Apprise integration

**Phase 2: Testing & Validation**
- Unit tests for all processors
- Integration tests with sample files
- Mock fusionn-subs testing
- Performance benchmarking

**Phase 3: Future Enhancements**
- StyleProcessor (ASS styling)
- TimingProcessor (timing adjustments)
- FilterProcessor (ad removal)
- QualityCheckProcessor (subtitle validation)
- Custom user-defined processors

### Monitoring & Observability

- Log all processing steps (analyzer, merger, queue)
- Track job IDs through the pipeline
- Apprise notifications for success/failure
- Metrics for processing duration, error rates

### Documentation Required

- API documentation for webhook endpoints
- Configuration guide for `subtitle`, `redis`, `apprise` sections
- DuoSubs model selection guide
- OpenCC conversion configuration
- fusionn-subs integration guide
- Troubleshooting guide for common errors

---

**Status**: ✅ Proposal scaffolded and validated with `openspec validate --strict`

**Tasks**: 38 tasks across 8 groups (see `tasks.md`)

**Validation**: All design decisions resolved (see `FINAL_DECISIONS.md`)

**Architecture**: Extensible pipeline pattern (see `ARCHITECTURE.md`)
