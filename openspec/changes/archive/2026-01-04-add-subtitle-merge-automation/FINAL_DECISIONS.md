# Final Design Decisions ✅

All design questions have been resolved. The proposal is **READY FOR IMPLEMENTATION**.

## Decision Summary

| # | Question | Decision | Rationale |
|---|----------|----------|-----------|
| 1 | DuoSubs Model | **LaBSE** (`sentence-transformers/LaBSE`) | Standard, widely tested, multilingual support |
| 2 | Subtitle Output | **Same directory as video** | Keeps media + subtitles together, follows media server conventions |
| 3 | Notifications | **Yes, integrate Apprise** | Like fusionn-muse, provides success/failure alerts |
| 4 | fusionn-subs | **Already implemented + Mock for testing** | Production-ready, mock callback during dev/test |
| 5 | Subtitle Format | **Keep ASS** | DuoSubs default, better styling support than SRT |
| 6 | Chinese Conversion | **Use OpenCC library** | Fast, accurate Traditional → Simplified conversion |
| 7 | Webhook Fields | **Use absolute paths** | `episodeFile.path` (Sonarr), `movieFile.path` (Radarr) |

## Configuration Schema (Final)

```yaml
subtitle:
  enabled: true
  
  duosubs:
    model: "sentence-transformers/LaBSE"  # ✅ DECISION 1
    device: "auto"
    timeout_minutes: 10
  
  output_same_dir: true  # ✅ DECISION 2
  
  english_variants: ["eng", "eng-sdh", "en"]
  chinese_variants: ["zh-Hans", "zh-CN", "chi", "zho"]
  
  opencc:  # ✅ DECISION 6
    enabled: true
    config: "t2s.json"

redis:
  host: "redis"
  port: 6379
  password: ""
  database: 0
  queue_key: "fusionn:translation:queue"

apprise:  # ✅ DECISION 3
  enabled: true
  base_url: "http://apprise:8000"
  key: "apprise"
  tag: "fusionn-subtitle"
```

## Testing Strategy

### Development/Testing Mode
- **Mock fusionn-subs**: Use mock HTTP server or test helper to simulate translation callbacks
- **Avoid hard dependency**: Don't require fusionn-subs to be running during unit/integration tests
- **Test callback endpoint**: Verify callback parsing and subtitle merge trigger independently

### Production Mode
- **Real fusionn-subs**: Connect to actual fusionn-subs service via Redis queue
- **End-to-end testing**: Test full pipeline in staging environment with real fusionn-subs

### Mock Implementation
```go
// Example test mock
func mockTranslationCallback(jobID, translatedSubPath string) {
    payload := map[string]string{
        "job_id": jobID,
        "status": "completed",
        "translated_subtitle_path": translatedSubPath,
    }
    // POST to /api/v1/callback/translation
}
```

## Integration Points

### 1. Sonarr/Radarr → fusionn
- **Sonarr**: `POST /api/v1/webhook/sonarr`
  - Extract: `episodeFile.path` (absolute path)
  - Event types: "Download", "Upgrade"
- **Radarr**: `POST /api/v1/webhook/radarr`
  - Extract: `movieFile.path` (absolute path)
  - Event types: "Download", "Upgrade"

### 2. fusionn → fusionn-subs
- **Translation Queue**: Redis RPUSH to `fusionn:translation:queue`
- **Job Payload**:
  ```json
  {
    "job_id": "uuid",
    "video_path": "/path/to/video.mkv",
    "english_subtitle_path": "/tmp/eng.srt",
    "callback_url": "http://fusionn:8080/api/v1/callback/translation",
    "media_type": "movie|episode",
    "metadata": {...}
  }
  ```

### 3. fusionn-subs → fusionn
- **Callback**: `POST /api/v1/callback/translation`
- **Payload**:
  ```json
  {
    "job_id": "uuid",
    "status": "completed",
    "translated_subtitle_path": "/tmp/zh.srt"
  }
  ```

### 4. fusionn → Apprise
- **Success Notification**: "✅ Subtitle merged: {video_name}"
- **Failure Notification**: "❌ Subtitle merge failed: {video_name} - {error}"

## Technology Stack (Final)

| Component | Technology | Version/Details |
|-----------|-----------|-----------------|
| **Language** | Go | 1.25.5 |
| **HTTP Framework** | Gin | github.com/gin-gonic/gin v1.10.0 |
| **Redis Client** | go-redis | github.com/redis/go-redis/v9 |
| **Subtitle Detection** | ffprobe + ffmpeg | System binaries |
| **Subtitle Merging** | DuoSubs | Python CLI (`duosubs merge`) |
| **Model** | LaBSE | sentence-transformers/LaBSE (~2GB) |
| **Chinese Conversion** | OpenCC | Python package (`opencc`) |
| **Notifications** | Apprise | HTTP client (similar to fusionn-muse) |

## File Output Example

**Input**: `/movies/Movie Title (2024)/Movie.Title.2024.1080p.mkv`

**Output**: `/movies/Movie Title (2024)/Movie.Title.2024.1080p_bilingual.ass`

- **Format**: ASS (Advanced SubStation Alpha)
- **Primary**: Simplified Chinese
- **Secondary**: English
- **Merged by**: DuoSubs semantic alignment

## Workflow Summary

```
1. Sonarr/Radarr Import → Webhook
   ↓
2. fusionn receives webhook → Extract file path
   ↓
3. ffprobe detects subtitle tracks
   ↓
4. ffmpeg extracts English + Chinese subtitles
   ↓
5a. If Chinese exists → DuoSubs merge → Save to video dir → Apprise ✅
   ↓
5b. If Traditional Chinese → OpenCC convert → DuoSubs merge → Apprise ✅
   ↓
5c. If no Chinese → Redis queue → fusionn-subs translates → Callback → Merge → Apprise ✅
```

## Implementation Plan

**Phase 1: Configuration + Webhooks** (Week 1)
- Configuration schema with Apprise
- Webhook handlers (Sonarr + Radarr)
- Basic job queue

**Phase 2: Subtitle Analysis + Merging** (Week 2)
- ffprobe/ffmpeg integration
- DuoSubs executor
- OpenCC conversion
- Apprise notifications

**Phase 3: Translation Queue** (Week 3)
- Redis client
- Translation callback API
- fusionn-subs integration

**Phase 4: Testing + Deployment** (Week 4)
- End-to-end testing
- Docker setup (ffmpeg + Python deps)
- Documentation

## Architecture: Extensible Pipeline

The system is built on a **Pipeline Architecture** that supports easy addition of new processing steps:

### Core Design
- **Processor Interface**: Each step is a self-contained processor
- **ProcessingContext**: State flows through pipeline
- **Conditional Execution**: Processors decide when to run
- **Configuration-Driven**: Enable/disable processors via config

### Current Processors (Phase 1)
1. AnalyzerProcessor - Detect subtitle tracks
2. ExtractorProcessor - Extract to SRT files
3. ConversionProcessor - Traditional → Simplified (OpenCC)
4. MergerProcessor - Merge with DuoSubs
5. OutputProcessor - Copy to video directory
6. NotificationProcessor - Apprise alerts
7. CleanupProcessor - Remove temp files
8. TranslationQueueProcessor - Queue if Chinese missing

### Future Processors (Extensible)
- **StyleProcessor** - Modify ASS fonts/colors/positioning
- **TimingProcessor** - Adjust timing offsets/speed
- **FilterProcessor** - Remove ads/credits
- **QualityCheckProcessor** - Validate timing/encoding
- **BackupProcessor** - Backup original subtitles

See `ARCHITECTURE.md` for detailed design and examples.

## Next Steps

1. ✅ **Proposal validated**: `openspec validate --strict` passed
2. ✅ **All design decisions finalized**
3. ✅ **Architecture designed**: Extensible pipeline pattern
4. 📝 **Ready for approval**
5. 🚀 **Begin implementation**: Follow `tasks.md` (35 tasks total)

## References

- **DuoSubs**: https://github.com/CK-Explorer/DuoSubs
- **OpenCC**: https://github.com/BYVoid/OpenCC
- **Sonarr Webhooks**: https://wiki.servarr.com/sonarr/settings#connect
- **Radarr Webhooks**: https://wiki.servarr.com/radarr/settings#connect
- **fusionn-muse Apprise**: Reference existing Apprise client implementation

---

**Status**: ✅ **COMPLETE - READY FOR IMPLEMENTATION**

**Validation**: ✅ `openspec validate add-subtitle-merge-automation --strict` passed

**Tasks**: 0/35 completed (8 tasks added for Apprise integration)

**Delta Count**: 19 requirements across 4 capabilities

