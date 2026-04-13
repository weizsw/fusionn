# Dual-Language Subtitle Handling

**Date:** 2026-04-13
**Status:** Approved

## Problem

When Bazarr finds and downloads a Chinese subtitle, it may be a **dual-language** subtitle containing both English and Chinese text in a single file. The current pipeline blindly treats it as a Chinese-only SRT and passes it to DuoSubs alongside the extracted English SRT. This produces garbled output with duplicated English lines.

There is no dual-language detection anywhere in the current codebase.

## Solution

Add a `DualLanguageProcessor` to the **merge pipeline** (Approach B — handle entirely in the merge pipeline). This processor detects dual-language subtitles, parses them, and converts them to a styled ASS file — skipping the DuoSubs merge entirely since the subtitle is already bilingual.

## Scope

- Dual-language detection and conversion for Bazarr-sourced subtitles only (`ChineseSubSource == "bazarr"`)
- SRT and ASS input format support
- OpenCC integration for Traditional → Simplified conversion within dual-language files
- No changes to the analyze pipeline or `MergeJob` struct

## Design

### New Processor: `DualLanguageProcessor`

**Pipeline position:** First processor in the merge pipeline, before `ConversionProcessor`.

```
Merge pipeline: DualLanguage → Conversion → Merger → Style → FontEmbedding → Output → Notification → Cleanup
```

**Construction:** Takes `config.OpenCCConfig` so it can run Traditional→Simplified conversion internally when enabled.

**`ShouldRun` conditions:**
- `ChineseSubSource == "bazarr"`
- `ChineseSubPath != ""`
- `MergedSubPath == ""` (not already processed)

**`Process` logic:**
1. Read the subtitle file at `ChineseSubPath`
2. Detect file format (SRT vs ASS by extension)
3. Run dual-language detection (see Detection Heuristics below)
4. If not dual-language: return nil (do nothing, normal pipeline continues)
5. If dual-language:
   a. Parse the full file
   b. Separate Chinese and English text per cue
   c. If OpenCC is enabled: write Chinese text to temp SRT, run `executor.ConvertOpenCC`, read back converted text. OpenCC `t2s` on already-Simplified text is a safe no-op, so we always convert when enabled rather than relying on `NeedsConversion` (which is `false` in the merge pipeline context).
   d. Generate ASS events with `Default` style (Chinese) and `Default_1` style (English)
   e. Write ASS to a temp file in a `.fusionn-dual-{uuid}` directory under the video's parent dir (same pattern as `MergerProcessor`). Set `pctx.TempMergeDir` for cleanup.
   f. Set `pctx.MergedSubPath` to the generated ASS path
   g. Log the detection and conversion

### Supported Input Formats

**Format 1: SRT with alternating lines** (most common)
```
1
00:00:01,000 --> 00:00:03,000
你好世界
Hello World
```
Each line categorized by Unicode: CJK-dominant → `Default` style, Latin-dominant → `Default_1` style, same timestamp.

**Format 2: SRT with `\N` separator**
```
1
00:00:01,000 --> 00:00:03,000
你好世界\NHello World
```
Split on `\N` first, then categorize each segment.

**Format 3: ASS with two styles**
```
Dialogue: 0,0:00:01.00,0:00:03.00,ChineseStyle,,0,0,0,,你好世界
Dialogue: 0,0:00:01.00,0:00:03.00,EnglishStyle,,0,0,0,,Hello World
```
Remap existing styles: CJK-dominant style → `Default`, Latin-dominant → `Default_1`.

**Format 4: Single line with both languages concatenated** (edge case)
```
1
00:00:01,000 --> 00:00:03,000
你好世界 Hello World
```
Split at CJK-to-Latin boundary, generate two events.

### Detection Heuristics

**Line classification:**
- **CJK dominant:** Contains characters in Unicode ranges `\u4E00-\u9FFF`, `\u3400-\u4DBF`, `\uF900-\uFAFF`
- **Latin dominant:** Primarily ASCII/Latin characters
- **Mixed:** Both CJK and Latin present in a single unseparated line

**File classification (SRT):**
- Sample first 30 cues
- Normalize: split each cue's text on `\N` and newlines
- A cue is "bilingual" if its normalized lines contain both CJK-dominant and Latin-dominant segments
- If >30% of sampled cues are bilingual → file is dual-language

**File classification (ASS):**
- Parse `[Events]` section
- Check if Dialogue events reference multiple styles
- Check if events across those styles contain different language content (one style CJK-dominant, another Latin-dominant)

**Aggressive stance:** The 30% threshold is intentionally low. It's better to restyle a dual-language subtitle than to garble it through DuoSubs.

### OpenCC Integration

For dual-language files containing Traditional Chinese, the processor handles OpenCC internally:
1. Extract Chinese lines from parsed cues into a temporary SRT file
2. Call `executor.ConvertOpenCC` on the temp SRT (reuses existing infrastructure)
3. Read back converted text
4. Use the converted Chinese text when generating the final ASS events

This approach avoids modifying the existing `ConversionProcessor`, which only operates on standalone Chinese SRT files.

### MergerProcessor Guard

Add a skip condition to `MergerProcessor.ShouldRun`:

```go
func (p *MergerProcessor) ShouldRun(pctx *ProcessingContext) bool {
    return pctx.MergedSubPath == "" && pctx.EnglishSubPath != "" && pctx.ChineseSubPath != ""
}
```

When `DualLanguageProcessor` sets `MergedSubPath`, the merger is skipped. The `StyleProcessor` still runs (it only requires `MergedSubPath != ""`), applying custom fonts, colors, and positioning as normal.

### ASS Output Format

The generated ASS follows the same structure as DuoSubs output, compatible with the existing `StyleProcessor`:

```
[Script Info]
ScriptType: v4.00+
...

[V4+ Styles]
Format: Name, Fontname, Fontsize, ...
Style: Default,...         (Chinese - primary)
Style: Default_1,...       (English - secondary)

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Dialogue: 0,0:00:01.00,0:00:03.00,Default,,0,0,0,,你好世界
Dialogue: 0,0:00:01.00,0:00:03.00,Default_1,,0,0,0,,Hello World
```

## Files Changed

| File | Change |
|------|--------|
| `internal/service/subtitle/processor_dual_language.go` | New — detection + conversion processor |
| `internal/service/subtitle/processor_dual_language_test.go` | New — tests for all format variants |
| `internal/service/subtitle/processor_merger.go` | Add `MergedSubPath == ""` guard to `ShouldRun` |
| `internal/service/subtitle/service.go` | Insert `DualLanguageProcessor` at start of merge pipeline, pass `OpenCCConfig` |

## Non-Goals

- Detecting dual-language subtitles from non-Bazarr sources (extracted or translated subs are already language-specific)
- Handling triple-language or other exotic subtitle formats
- Modifying the `MergeJob` struct or analyze pipeline
- Modifying `ConversionProcessor` — dual-language OpenCC is self-contained in the new processor
