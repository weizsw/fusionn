# Design: Subtitle Merge Automation

## Context

Sonarr and Radarr are popular media management tools that automate TV show and movie downloads. They support webhooks that trigger when new media is imported. Users want bilingual subtitles (Chinese + English) automatically created for imported media to improve viewing experience.

**Current Limitations:**
- No automated subtitle merging solution
- Manual extraction and merging is time-consuming
- Fallback logic for missing subtitles not standardized
- No integration with translation services

**Stakeholders:**
- End users who consume bilingual media
- fusionn service (this project) - orchestrator
- fusionn-subs service - translation worker
- Sonarr/Radarr - media management tools

## Goals / Non-Goals

### Goals
- Automatically merge Chinese and English subtitles when both exist
- Implement intelligent fallback for subtitle language variants
- Queue translation jobs when Chinese subtitles missing
- Provide callback API for translation completion
- Use DuoSubs for high-quality semantic subtitle alignment
- Handle errors gracefully with logging and notifications

### Non-Goals
- Video transcoding or re-encoding (out of scope)
- Real-time subtitle display or player integration
- Manual subtitle upload interface (webhook-driven only)
- Subtitle quality assessment or correction (DuoSubs handles this)
- Support for subtitle formats other than SRT/ASS (DuoSubs limitation)

## Decisions

### 1. Webhook Architecture

**Decision:** Use separate endpoints for Sonarr and Radarr webhooks (`/api/v1/webhook/sonarr` and `/api/v1/webhook/radarr`).

**Rationale:**
- Different payload formats may require different parsing logic
- Easier to add service-specific features later
- Clear separation of concerns

**Alternatives Considered:**
- Single unified `/api/v1/webhook/media` endpoint with auto-detection → Rejected: Harder to debug and maintain

### 2. Subtitle Extraction Tool

**Decision:** Use ffprobe for detection and ffmpeg for extraction.

**Rationale:**
- Industry-standard tools for media file analysis
- Already widely used in media automation pipelines
- Reliable subtitle track extraction
- Easy to integrate via shell execution

**Alternatives Considered:**
- MediaInfo library → Rejected: Less comprehensive subtitle extraction capabilities
- Python-based libraries (pymediainfo, ffmpeg-python) → Rejected: Adds unnecessary Python dependency for simple task

### 3. DuoSubs Integration Method

**Decision:** Shell out to DuoSubs Python CLI (`duosubs merge`).

**Rationale:**
- Follows existing pattern from fusionn-muse (shell execution for Python tools)
- DuoSubs is CLI-first with well-documented command interface
- No need to maintain Python HTTP service
- Simpler deployment (just install Python package)

**Alternatives Considered:**
- Create DuoSubs HTTP microservice → Rejected: Unnecessary complexity for single-purpose tool
- Reimplement DuoSubs in Go → Rejected: Massive effort, semantic models require PyTorch/HuggingFace

### 4. Chinese Translation Fallback

**Decision:** Traditional → Simplified conversion using OpenCC library when Traditional Chinese subtitles exist but Simplified Chinese missing.

**Rationale:**
- OpenCC is battle-tested for Chinese script conversion
- Faster than LLM translation
- Preserves timing and formatting perfectly
- High accuracy for script conversion

**Alternatives Considered:**
- Always use LLM translation → Rejected: Overkill for script conversion, slower and more expensive
- Skip Traditional Chinese fallback → Rejected: Wastes existing subtitle resources

### 5. Redis Queue Schema

**Decision:** Use Redis LIST (LPUSH/RPUSH) with JSON-encoded job messages.

**Schema:**
```json
{
  "job_id": "uuid-v4",
  "video_path": "/path/to/video.mkv",
  "english_subtitle_path": "/path/to/en.srt",
  "callback_url": "http://fusionn:8080/api/v1/callback/translation",
  "media_type": "movie|episode",
  "metadata": {
    "title": "Movie Title",
    "year": 2024,
    "imdb_id": "tt1234567"
  },
  "timestamp": "2024-01-01T00:00:00Z"
}
```

**Rationale:**
- Simple FIFO queue semantics
- JSON is human-readable for debugging
- Callback URL allows decoupled architecture
- Metadata helps translation service prioritize and contextualize

**Alternatives Considered:**
- Redis STREAM → Rejected: Overkill for simple queue needs
- Message broker (RabbitMQ, Kafka) → Rejected: Too heavy for single queue use case
- Database-backed queue → Rejected: Slower, requires schema management

### 6. Subtitle Language Detection Strategy

**Decision:** Use ffprobe's language tag + heuristic fallback based on track title/filename.

**Fallback Order:**
```
English:
  1. language=eng OR eng-us OR en
  2. language=eng-sdh (SDH = Subtitles for Deaf and Hard of Hearing)
  3. title contains "English" or "ENG" (case-insensitive)

Simplified Chinese:
  1. language=zh-Hans OR zh-CN OR chi OR zho
  2. title contains "简体" or "简中" or "CHS"
  3. Traditional Chinese (zh-Hant, zh-TW) + OpenCC conversion
  4. title contains "繁体" or "繁中" or "CHT" + OpenCC conversion
```

**Rationale:**
- ffprobe language tags are most reliable but not always present
- Fallback to title/filename covers poorly-tagged files
- OpenCC conversion preserves timing and is fast

### 7. Sonarr/Radarr Webhook Payload Parsing

**Decision:** Parse standard Sonarr/Radarr webhook payloads to extract file paths.

**Sonarr Payload Fields:**
```json
{
  "eventType": "Download" | "Upgrade" | "Rename",
  "series": {
    "title": "Series Name",
    "path": "/path/to/series"
  },
  "episodes": [
    {
      "title": "Episode Title",
      "episodeNumber": 1,
      "seasonNumber": 1
    }
  ],
  "episodeFile": {
    "path": "/absolute/path/to/episode.mkv",
    "relativePath": "Season 1/Episode.mkv",
    "sceneName": "Release.Name"
  }
}
```

**Radarr Payload Fields:**
```json
{
  "eventType": "Download" | "Upgrade" | "Rename",
  "movie": {
    "title": "Movie Title",
    "year": 2024,
    "imdbId": "tt1234567",
    "tmdbId": 12345
  },
  "movieFile": {
    "path": "/absolute/path/to/movie.mkv",
    "relativePath": "Movie (2024)/Movie.mkv",
    "sceneName": "Release.Name"
  }
}
```

**Key Fields Used:**
- Sonarr: `episodeFile.path` (absolute path to video file)
- Radarr: `movieFile.path` (absolute path to video file)
- Both: `eventType` (filter to "Download" and "Upgrade" only)

**Rationale:**
- `path` provides absolute filesystem path needed for ffprobe/ffmpeg
- `eventType` allows filtering relevant events (ignore "Test", "Rename")
- Additional metadata (title, year, IMDB ID) useful for logging and future features

## System Architecture

### Design Philosophy: Extensible Pipeline

The subtitle processing system is built on a **Pipeline Architecture** to support easy addition of future features like ASS style modifications, timing adjustments, custom filters, and more.

**Key Principles:**
- **Extensibility**: New processing steps can be added without modifying core code
- **Composability**: Processors can be chained in any order
- **Conditional Execution**: Each processor decides if it should run based on context
- **Configuration-Driven**: Processors can be enabled/disabled via configuration

### Pipeline Flow

```
Webhook → ProcessingContext → Pipeline → [Processors] → Result → Notification

Processors (sequential execution):
  1. AnalyzerProcessor      - Detect subtitle tracks (ffprobe)
  2. ExtractorProcessor     - Extract subtitles to SRT files (ffmpeg)
  3. ConversionProcessor    - Traditional → Simplified Chinese (OpenCC)
  4. MergerProcessor        - Merge English + Chinese (DuoSubs)
  5. StyleProcessor         - FUTURE: Modify ASS styles (fonts, colors)
  6. TimingProcessor        - FUTURE: Adjust subtitle timing
  7. FilterProcessor        - FUTURE: Remove ads/credits
  8. OutputProcessor        - Copy to video directory
  9. NotificationProcessor  - Send Apprise alert
  10. CleanupProcessor      - Remove temporary files
  11. TranslationQueueProcessor - Queue translation if Chinese missing
```

### ProcessingContext

Central state object that flows through the pipeline:

```go
type ProcessingContext struct {
    // Input
    VideoPath   string
    MediaType   string  // "movie" or "episode"
    JobID       string
    
    // Analysis results
    Analysis *AnalysisResult
    
    // Processing results
    EnglishSubPath  string
    ChineseSubPath  string
    MergedSubPath   string
    
    // Flags
    NeedsConversion  bool
    NeedsTranslation bool
    
    // Extensible metadata
    Metadata map[string]interface{}
}
```

### Processor Interface

```go
type Processor interface {
    Name() string
    Process(ctx context.Context, pctx *ProcessingContext) error
    ShouldRun(pctx *ProcessingContext) bool
}
```

### Benefits

1. **Easy Extension**: Add new processors without modifying existing code
   ```go
   pipeline.AddProcessor(NewStyleProcessor(config))
   ```

2. **Isolated Testing**: Test each processor independently
   ```go
   func TestMergerProcessor(t *testing.T) {
       proc := NewMergerProcessor(cfg)
       pctx := &ProcessingContext{...}
       err := proc.Process(ctx, pctx)
       // Assert...
   }
   ```

3. **Clear Logging**: Track execution of each processor
   ```
   ▶️  Running processor: AnalyzerProcessor
   ✅ Processor completed: AnalyzerProcessor
   ⏭️  Skipping processor: ConversionProcessor (condition not met)
   ```

4. **Future-Proof**: Ready for advanced features
   - **StyleProcessor**: Modify ASS fonts, colors, positioning
   - **TimingProcessor**: Adjust timing offsets, speed
   - **FilterProcessor**: Remove unwanted subtitle lines
   - **BackupProcessor**: Backup original subtitles
   - **QualityCheckProcessor**: Validate subtitle quality

See `ARCHITECTURE.md` for detailed pipeline design and examples.

## Risks / Trade-offs

### Risk 1: DuoSubs Model Download Size
**Impact:** First run downloads 1-3GB model (LaBSE or Qwen embedding).
**Mitigation:** 
- Pre-download model during Docker image build
- Document model storage requirements
- Allow configurable model selection for resource-constrained environments

### Risk 2: Processing Time
**Impact:** Full pipeline (extraction + DuoSubs + merge) can take 1-5 minutes per video.
**Mitigation:**
- Async job queue prevents webhook timeout
- Log progress at each stage
- Add timeout configuration for long-running jobs

### Risk 3: ffmpeg/ffprobe Dependency
**Impact:** Requires system packages not in minimal Go container.
**Mitigation:**
- Use ffmpeg-enabled Docker base image (e.g., `linuxserver/ffmpeg`)
- Document installation requirements for non-Docker deployments
- Add health check to verify ffmpeg availability

### Risk 4: Redis Single Point of Failure
**Impact:** If Redis goes down, translation jobs are lost.
**Mitigation:**
- Redis persistence (AOF or RDB)
- Retry logic with exponential backoff
- Log all enqueued jobs for manual recovery

### Risk 5: OpenCC Conversion Accuracy
**Impact:** Traditional → Simplified conversion may have contextual errors (e.g., 臺灣 vs 台湾).
**Mitigation:**
- Use OpenCC's `t2s.json` config (optimized for Traditional to Simplified)
- Document that conversion is character-based, not semantic
- Prefer LLM translation for better quality when possible

## Migration Plan

### Phase 1: Core Infrastructure (Week 1)
1. Add configuration schema and Redis client
2. Implement webhook handlers (Sonarr + Radarr)
3. Implement subtitle analyzer (ffprobe integration)
4. Write unit tests for each component

### Phase 2: Subtitle Merging (Week 2)
1. Implement DuoSubs executor
2. Implement subtitle merger with fallback logic
3. Implement OpenCC Traditional → Simplified conversion
4. Integration testing with sample videos

### Phase 3: Translation Queue (Week 3)
1. Implement Redis queue publisher
2. Implement translation callback endpoint
3. Test full pipeline: webhook → queue → callback → merge
4. Add error handling and retry logic

### Phase 4: Deployment and Documentation (Week 4)
1. Update Dockerfile with ffmpeg and Python dependencies
2. Update docker-compose.yaml with Redis service
3. Write comprehensive README documentation
4. Test with real Sonarr/Radarr instance
5. Deploy to production

### Rollback Strategy
- Keep existing media files untouched (only create new subtitle files)
- Disable subtitle merging via configuration flag if issues arise
- Redis queue is separate service (can be rolled back independently)
- No database schema changes (stateless service)

## Design Questions - ALL RESOLVED ✅

1. **DuoSubs Model Selection:** Should we default to `LaBSE` (multilingual, 2GB) or `Qwen3-Embedding-0.6B` (smaller, 600MB)?
   - **✅ DECISION:** Use `sentence-transformers/LaBSE` (standard, widely tested, multilingual)

2. **Subtitle Output Location:** Where should merged subtitles be saved? Same directory as video, or separate output folder?
   - **✅ DECISION:** Save merged subtitle to the same directory as video file

3. **Notification Integration:** Should we integrate Apprise (like fusionn-muse) for success/failure notifications?
   - **✅ DECISION:** Yes, integrate Apprise for subtitle merge success/failure notifications

4. **fusionn-subs Integration:** Is fusionn-subs already implemented, or should we mock the callback for testing?
   - **✅ DECISION:** fusionn-subs is already implemented (production-ready), but **mock the callback for testing** to avoid hard dependency during development

5. **Subtitle Format Preference:** Should merged output be ASS (DuoSubs default) or convert back to SRT?
   - **✅ DECISION:** Keep ASS format (DuoSubs default, better styling support)

6. **OpenCC Library:** Use library for Traditional → Simplified Chinese conversion?
   - **✅ DECISION:** Yes, use OpenCC library (Python package)

7. **Sonarr/Radarr Webhook Fields:** What fields are available in webhook payloads?
   - **✅ DECISION:** Use `episodeFile.path` (Sonarr) and `movieFile.path` (Radarr) for absolute file paths

