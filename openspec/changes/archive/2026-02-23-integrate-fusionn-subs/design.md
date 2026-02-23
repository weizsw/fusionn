## Context

fusionn receives webhooks from Sonarr/Radarr when new media is downloaded. It analyzes embedded subtitles and extracts them. When Chinese subtitles are missing, it currently queues a translation job to Redis but has no worker to process it.

fusionn-subs is a separate Go service designed to poll Redis for subtitle translation jobs, translate English subtitles to Chinese using AI providers (OpenRouter/Gemini), and callback when complete. It was originally built as a standalone tool but needs integration with fusionn.

**Current State:**
- fusionn extracts subtitles to `/tmp` and queues minimal job data (video path only)
- fusionn-subs expects subtitle file path in job message and writes output to configurable location
- Job message formats are incompatible
- No callback handler exists in fusionn

**Constraints:**
- Both services run as separate containers (deployment independence)
- Translation is slow (30s - 5+ minutes per subtitle)
- Need reliability - jobs must survive service restarts
- fusionn-subs should remain reusable for other projects

## Goals / Non-Goals

**Goals:**
- Enable automatic Chinese subtitle translation when missing from media
- Maintain service independence (separate deployments, restarts don't lose jobs)
- Ensure reliable job processing with retries
- Use shared filesystem for subtitle files (avoid network transfer)
- Preserve fusionn-subs reusability

**Non-Goals:**
- Merging fusionn-subs into fusionn as a library (maintain separation)
- Supporting multiple translation services (fusionn-subs only)
- Real-time translation progress tracking
- Translation quality validation or feedback loops

## Decisions

### Decision 1: Shared Filesystem via Docker Volumes

**Chosen:** Both services mount the same media library volume. Subtitles are extracted to and translated in the media directory alongside video files.

**Rationale:**
- Eliminates need to transfer subtitle content over network in callback
- Simplifies path management - both services work with same file paths
- Standard deployment pattern for media automation services
- Callback only needs to pass paths, not file content

**Alternatives Considered:**
- Network storage (NFS/S3): Added complexity, latency, and failure modes
- Callback with file content: Large payloads (subtitles can be 100KB+), timeout risks
- fusionn-subs writes to different location: Path mapping complexity, mounting issues

**Trade-offs:**
- ✓ Simple, fast, reliable file access
- ✓ No additional infrastructure beyond Docker volumes
- ✗ Requires both services on same host or shared network storage in distributed setup

### Decision 2: Subtitle Extraction to Media Directory with Standard Naming

**Chosen:** fusionn extracts subtitles to `<video-dir>/<video-name>.eng.srt` instead of `/tmp/fusionn-<uuid>.srt`

**Rationale:**
- Both services can access files via same paths
- Predictable naming makes debugging easier
- Files stay with media for potential re-processing
- fusionn cleanup processor can remove intermediate files after merge

**Alternatives Considered:**
- Keep temp directory: Would require fusionn-subs to mount `/tmp` or copy files, fragile
- Random naming: Harder to debug, no benefit for temporary files
- Keep intermediate files: Clutters media library, no clear use case

**Trade-offs:**
- ✓ Simple path management
- ✓ Easier debugging and manual inspection
- ✗ Intermediate files visible in media directory (mitigated by cleanup)

### Decision 3: Remove Callback URL from Job Message

**Chosen:** fusionn-subs reads callback URL from its own config, not from job message

**Rationale:**
- Callback URL is deployment-specific, not job-specific
- fusionn-subs already has `callback.url` config field
- Reduces job message size and complexity
- Allows changing callback endpoint without re-queuing jobs

**Alternatives Considered:**
- Include callback URL in job: More flexible but unnecessary - all jobs callback to same endpoint
- Environment variable per job: Over-engineering for single-endpoint scenario

**Trade-offs:**
- ✓ Simpler job message structure
- ✓ Deployment flexibility
- ✗ Less flexible if multiple callback endpoints needed (not a current requirement)

### Decision 4: Retry Logic in fusionn-subs

**Chosen:** 
- Translation retries: Max 3 attempts, no backoff (idempotent operation)
- Callback retries: Max 5 attempts, exponential backoff (1s, 2s, 4s, 8s, 16s)

**Rationale:**
- Translation failures are usually model/rate-limit issues - retry immediately
- Callback failures are often transient (fusionn restart) - backoff gives time to recover
- Conservative retry counts prevent infinite loops
- Exponential backoff for callbacks prevents hammering down service

**Alternatives Considered:**
- No retries: Unacceptable - would lose jobs on transient failures
- Dead letter queue: Over-engineering for current scale, adds Redis complexity
- Retry in fusionn: Wrong layer - worker should own retry logic

**Trade-offs:**
- ✓ Handles transient failures gracefully
- ✓ No additional infrastructure (DLQ)
- ✗ Jobs lost after max retries (acceptable for current use case)
- ✗ No visibility into retry counts (can add metrics later)

### Decision 5: Unified Job Message Format

**Chosen:** Single message structure with fields from both services:
```json
{
  "job_id": "uuid",
  "video_path": "/media/Show/S01E01.mkv",
  "subtitle_path": "/media/Show/S01E01.eng.srt",
  "media_title": "Show Name S01E01",
  "media_type": "episode"
}
```

**Rationale:**
- `job_id`: Required for tracking and correlating callback with original request
- `video_path`: Useful for context/logging, required by fusionn callback
- `subtitle_path`: The actual file to translate (replaces fusionn-subs `path` field)
- `media_title`: Better than `overview` for logging/debugging
- `media_type`: Distinguishes episodes from movies (useful for logging)

**Removed fields:**
- `callback_url`: Moved to fusionn-subs config
- `file_name`: Redundant with `subtitle_path`
- `provider`: fusionn-subs config decides provider
- `overview`: Renamed to `media_title` for clarity

**Trade-offs:**
- ✓ Clear, minimal field set
- ✓ Both services have needed information
- ✗ Breaking change for fusionn-subs (requires update)

## Risks / Trade-offs

### Risk: fusionn-subs down when jobs queued
- **Impact**: Jobs accumulate in Redis, media gets dual subtitles delayed
- **Mitigation**: Redis persists queue, jobs processed when service returns. Add monitoring for queue depth.

### Risk: Callback failure after max retries
- **Impact**: Translated subtitle exists but never merged, manual intervention needed
- **Mitigation**: Log loudly on final failure. Consider adding retry endpoint for manual trigger. Acceptable for v1.

### Risk: Disk space exhaustion from queued subtitles
- **Impact**: Subtitle extraction fills disk if many jobs queued
- **Mitigation**: fusionn already has cleanup processor. Monitor disk usage. Not unique to this integration.

### Risk: Path mismatch between services
- **Impact**: fusionn-subs can't find file, translation fails
- **Mitigation**: Both services must mount media volume with same path. Document in deployment guide. Add validation logs.

### Trade-off: Service coupling via filesystem
- **Issue**: Both services must run on same host or shared network storage
- **Acceptance**: Standard pattern for media services (Sonarr/Radarr/Plex all use shared volumes). Benefits outweigh distributed complexity.

### Trade-off: No translation progress visibility
- **Issue**: User doesn't know if job is queued, processing, or stuck
- **Acceptance**: Acceptable for v1. Can add status endpoint later if needed. Notifications happen on completion.

## Migration Plan

**Prerequisites:**
- Redis server running and accessible to both services
- Media library volume mountable by both containers

**Deployment Steps:**

1. **Update fusionn-subs first:**
   - Deploy new version with updated job message format
   - Old jobs in queue will fail validation (acceptable - queue should be empty)
   - Update config to point callback URL to fusionn

2. **Update fusionn:**
   - Deploy new version with callback endpoint and updated queue format
   - Redis config must match fusionn-subs queue name

3. **Verify docker-compose/k8s config:**
   - Both services mount media volume to same path
   - Both services can reach Redis
   - fusionn-subs can reach fusionn callback endpoint

4. **Test flow:**
   - Trigger webhook with media lacking Chinese subtitle
   - Verify job appears in Redis
   - Verify fusionn-subs picks up job
   - Verify callback received and merge completes

**Rollback:**
- fusionn: Revert to previous version, existing queued jobs safe
- fusionn-subs: Revert to previous version, may need to drain queue

**Data Migration:**
None required - fresh start for job queue

## Open Questions

None - design is ready for implementation.
