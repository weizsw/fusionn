# OpenSpec Proposal: Subtitle Merge Automation

## ✅ Status: Ready for Review

This proposal has been scaffolded and validated successfully with `openspec validate --strict`.

## 📋 Overview

**Change ID:** `add-subtitle-merge-automation`

**Purpose:** Automate bilingual subtitle creation for Sonarr/Radarr imported media by merging Chinese and English subtitles using semantic alignment (DuoSubs).

**Trigger:** Sonarr/Radarr webhooks on media import completion.

## 🎯 Key Features

1. **Webhook Handlers** - Accept import notifications from Sonarr (TV shows) and Radarr (movies)
2. **Extensible Pipeline** - Processor-based architecture for easy feature addition (ASS styling, timing, filters)
3. **Subtitle Analyzer** - Detect and extract English + Chinese subtitles from video files using ffprobe/ffmpeg
4. **Smart Fallback Logic**:
   - English: `eng` → `eng-sdh` → title-based detection
   - Chinese: `zh-Hans` → `zh-Hant` (with OpenCC conversion) → Redis translation queue
5. **Semantic Subtitle Merging** - Use [DuoSubs](https://github.com/CK-Explorer/DuoSubs) for high-quality bilingual subtitle alignment
6. **Translation Queue** - Redis-based job queue for missing Chinese subtitles (handled by fusionn-subs)
7. **Callback API** - Receive translation completion notifications and trigger post-translation merge
8. **Apprise Integration** - Success/failure notifications
9. **Mock Testing** - Mock fusionn-subs for development

## 📊 Capabilities

| Capability | Requirements | Scenarios |
|------------|--------------|-----------|
| **webhook-handler** | 5 | 13 |
| **subtitle-analyzer** | 4 | 13 |
| **subtitle-merger** | 5 | 16 |
| **translation-queue** | 5 | 15 |
| **Total** | **19** | **57** |

## 🔧 Technical Stack

- **Subtitle Detection**: ffprobe (read metadata) + ffmpeg (extract tracks)
- **Subtitle Merging**: DuoSubs Python CLI with Sentence Transformers (LaBSE/Qwen models)
- **Chinese Conversion**: OpenCC (Traditional → Simplified)
- **Queue**: Redis (go-redis/redis client)
- **Architecture**: Async job queue, webhook-triggered pipeline

## 📁 File Structure

```
openspec/changes/add-subtitle-merge-automation/
├── proposal.md                          # Why, What, Impact
├── tasks.md                             # 32 implementation tasks
├── design.md                            # Architecture decisions & trade-offs
└── specs/
    ├── webhook-handler/spec.md          # Sonarr/Radarr webhook endpoints
    ├── subtitle-analyzer/spec.md        # Subtitle detection & extraction
    ├── subtitle-merger/spec.md          # DuoSubs integration & fallback
    └── translation-queue/spec.md        # Redis queue & callback API
```

## 🔄 Workflow

```
┌─────────────────────┐
│ Sonarr/Radarr       │
│ Import Complete     │
└──────┬──────────────┘
       │ POST webhook
       ▼
┌─────────────────────┐
│ Webhook Handler     │ Enqueue job (UUID)
│ (fusionn)           │
└──────┬──────────────┘
       │
       ▼
┌─────────────────────┐
│ Subtitle Analyzer   │ ffprobe → detect tracks
│                     │ ffmpeg → extract SRT files
└──────┬──────────────┘
       │
       ├─── English: Found ──┐
       │                     │
       ├─── Chinese: Found ──┤
       │                     ▼
       │              ┌─────────────────────┐
       │              │ DuoSubs Merger      │
       │              │ Primary: Chinese    │
       │              │ Secondary: English  │
       │              └──────┬──────────────┘
       │                     │
       │                     ▼ bilingual.ass
       │              ┌─────────────────────┐
       │              │ Output Directory    │
       │              └─────────────────────┘
       │
       └─── Chinese: Missing ───┐
                                ▼
                         ┌─────────────────────┐
                         │ Redis Queue         │
                         │ (translation job)   │
                         └──────┬──────────────┘
                                │
                                ▼
                         ┌─────────────────────┐
                         │ fusionn-subs        │ (external)
                         │ Translate EN → ZH   │
                         └──────┬──────────────┘
                                │ POST callback
                                ▼
                         ┌─────────────────────┐
                         │ Callback Handler    │
                         │ (fusionn)           │
                         └──────┬──────────────┘
                                │
                                ▼
                         ┌─────────────────────┐
                         │ DuoSubs Merger      │ (retry with translated)
                         └─────────────────────┘
```

## 📝 Configuration Example

```yaml
# config/config.yaml
subtitle:
  enabled: true
  
  # DuoSubs settings
  duosubs:
    model: "sentence-transformers/LaBSE"  # Default: standard, multilingual
    device: "auto"  # "auto", "cuda", "cpu"
    timeout_minutes: 10
  
  # Output: save merged subtitle to same directory as video file
  output_same_dir: true
  
  # Fallback preferences for subtitle detection
  english_variants: ["eng", "eng-sdh", "en"]
  chinese_variants: ["zh-Hans", "zh-CN", "chi", "zho"]
  
  # OpenCC conversion (Traditional → Simplified Chinese)
  opencc:
    enabled: true
    config: "t2s.json"  # Traditional to Simplified

# Redis connection (for translation queue to fusionn-subs)
redis:
  host: "redis"
  port: 6379
  password: ""
  database: 0
  queue_key: "fusionn:translation:queue"

# Apprise notifications (similar to fusionn-muse)
apprise:
  enabled: true
  base_url: "http://apprise:8000"
  key: "apprise"
  tag: "fusionn-subtitle"

# Sonarr webhook: POST /api/v1/webhook/sonarr
# Radarr webhook: POST /api/v1/webhook/radarr
```

## ✅ Design Decisions - All Resolved

All design questions have been answered and the proposal is ready for implementation:

1. **DuoSubs Model Selection**: ✅ Use `sentence-transformers/LaBSE` (standard, multilingual, widely tested)
2. **Subtitle Output Location**: ✅ Save to same directory as video file (`{video_basename}_bilingual.ass`)
3. **Notification Integration**: ✅ **Yes, integrate Apprise** for subtitle merge success/failure notifications
4. **fusionn-subs Status**: ✅ **Already implemented** (production-ready), mock callback for testing
5. **Subtitle Format**: ✅ Keep ASS format (DuoSubs default, better styling support)
6. **OpenCC Integration**: ✅ Use Python opencc library for Traditional → Simplified Chinese conversion
7. **Webhook Payload Fields**: ✅ Use `episodeFile.path` (Sonarr) and `movieFile.path` (Radarr) for file paths

## 🚀 Next Steps

1. **Review & Approve**: Review proposal.md, design.md, and spec deltas
2. **Answer Open Questions**: Clarify the 5 design questions above
3. **Begin Implementation**: Follow tasks.md sequentially (32 tasks across 8 groups)
4. **Validation**: `openspec validate add-subtitle-merge-automation --strict` (already passing ✅)

## 📚 References

- **DuoSubs**: https://github.com/CK-Explorer/DuoSubs
- **OpenCC**: https://github.com/BYVoid/OpenCC
- **Sonarr Webhooks**: https://wiki.servarr.com/sonarr/settings#connect
- **Radarr Webhooks**: https://wiki.servarr.com/radarr/settings#connect
- **go-redis**: https://github.com/redis/go-redis

---

**Validation Status:** ✅ `openspec validate add-subtitle-merge-automation --strict` passed

**Tasks:** 0/32 completed (ready to start)

**Delta Count:** 18 requirements across 4 capabilities

