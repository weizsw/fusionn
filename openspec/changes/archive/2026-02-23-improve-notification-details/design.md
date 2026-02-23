## Context

Fusionn processes video subtitles by extracting/translating English and Chinese tracks, optionally converting Traditional to Simplified Chinese, and merging them into a styled output. Currently, Apprise notifications sent at the end of processing provide minimal information:
- Success: Shows media title, type, and output path
- Info (queued): Shows media title, type, and reason
- Warning: Shows media title, type, and reason

The ProcessingContext contains rich metadata about what happened during processing (source tracks, conversion flags, file paths), but this information isn't surfaced in notifications. Users need to check logs to understand which files were processed and how.

**Current State:**
- `NotificationProcessor` constructs messages using only `pctx.MediaTitle`, `pctx.MediaType`, and `pctx.MergedSubPath`
- `ProcessingContext` tracks `NeedsConversion` (Traditional→Simplified flag) but it's not shown
- `AnalysisResult` contains track info (extracted paths, codec) but not exposed in notifications
- No indication of whether Chinese subtitle was extracted from video or will be translated

## Goals / Non-Goals

**Goals:**
- Show Chinese subtitle source: "extracted" (found in video) or "will be translated" (missing)
- Show Traditional→Simplified conversion status when applicable
- Use clear, structured formatting (emoji, labels, multi-line) for readability
- Maintain compatibility with existing notification types and Apprise integration

**Non-Goals:**
- Adding new notification channels (Apprise only)
- Changing notification trigger conditions (still fires at end of pipeline)
- Adding configuration options for message format (use sensible defaults)
- Detailed error reporting (keep warnings/errors brief)

## Decisions

### D1: Determine Chinese subtitle source from ProcessingContext flags
**Decision:** Use `pctx.NeedsTranslation` flag to distinguish:
- `false` + `pctx.ChineseSubPath != ""` → extracted from video
- `true` → will be translated (queued)  
**Rationale:** This flag already exists and accurately reflects whether Chinese subtitle was found during analysis.  
**Alternative Considered:** Check AnalysisResult.ChineseTrack - rejected because ProcessingContext is the authoritative state after all processors run.

### D2: Show conversion status using NeedsConversion flag
**Decision:** When `pctx.NeedsConversion == true`, add line to message: "Conversion: Traditional → Simplified Chinese"  
**Rationale:** This is important for users with Traditional Chinese content to know conversion occurred.  
**Alternative Considered:** Only show if conversion actually happened - rejected because NeedsConversion is set during analysis and indicates user's Chinese track was Traditional.

### D3: Use multi-line message format with labels
**Decision:** Structure body as:
```
Media: <title>
Type: <movie/episode>
Chinese Sub: <extracted/translated>
[Conversion: Traditional → Simplified]
```
**Rationale:** Labels make information scannable. Multi-line format is clearer than comma-separated. Emoji in title already provides visual status indicator.  
**Alternative Considered:** JSON format - rejected as less human-readable in notification popup.

## Risks / Trade-offs

**Risk:** Longer messages may be truncated in some notification channels (SMS, push notifications)  
→ **Mitigation:** Keep total message under 500 chars. Use concise labels. Most Apprise backends support multi-line.

**Trade-off:** Adding more fields increases message length but improves actionability  
→ **Accept:** Information density is worth the extra lines. Users requested these specific fields.

## Migration Plan

No migration needed - this is a backward-compatible enhancement to notification messages. Changes are internal to `NotificationProcessor.Process()` method.

**Rollout:**
1. Update message construction logic in `processor_notification.go`
2. Test with various scenarios (success, queued, warning, traditional conversion)
3. Deploy - existing Apprise configuration remains unchanged

**Rollback:** Revert code changes to restore previous message format.

## Open Questions

None - requirements are clear and design is straightforward.
