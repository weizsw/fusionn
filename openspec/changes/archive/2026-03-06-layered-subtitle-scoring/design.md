## Context

The English subtitle selector in `analyzer.go` uses a 2-tier priority: non-SDH (priority 1) vs SDH (priority 2). Among same-priority tracks, first-in-stream-order wins. This fails when a "Forced" subtitle (foreign-dialogue-only translations, ~23 entries) precedes the full English track (~567 entries) — both are non-SDH, so the forced track wins by position.

The `StreamInfo` struct already parses `Disposition` (map of `forced`, `hearing_impaired`, `default`, etc.) and `Tags` (including `NUMBER_OF_FRAMES`, `NUMBER_OF_BYTES` from MKV statistics) from ffprobe. Neither is used in selection today.

## Goals / Non-Goals

**Goals:**

- Never select a forced/signs track when a full English subtitle exists
- Prefer regular English over SDH when both are available
- Degrade gracefully when metadata is missing (no disposition, no title, no frame stats)
- Keep the change scoped to English track selection; Chinese selection is unaffected

**Non-Goals:**

- Changing the Chinese subtitle selection logic
- Supporting non-SRT subtitle codecs differently
- Adding user-configurable priority weights

## Decisions

### Decision 1: Penalty scoring over exclusion filters

**Choice:** Accumulate penalty points per track across four independent signal layers. Lowest total penalty wins.

**Alternative considered:** Strict exclusion filters (disposition `forced=1` → exclude entirely). Rejected because if all tracks get incorrectly excluded (bad metadata), you get no English subtitle at all. Scoring always produces a winner.

**Penalty table:**

| Signal | Condition | Penalty | Rationale |
|--------|-----------|---------|-----------|
| Disposition: forced | `disposition["forced"] == 1` | +100 | Strongest signal when present |
| Disposition: hearing_impaired | `disposition["hearing_impaired"] == 1` | +10 | SDH is acceptable but not preferred |
| Title: forced/signs | title contains "forced", "signs", "signs & songs" | +100 | Catches forced tracks when disposition is missing |
| Title: SDH/CC | title contains "sdh", "cc", "hearing impaired" | +10 | Catches SDH when disposition is missing |
| Frame count: outlier low | track frames < 25% of max English track frames | +50 | Safety net — forced tracks have 10-50 frames vs 400-800+ |
| Byte count: outlier low | track bytes < 25% of max English track bytes | +50 | Redundant safety net for frame count |

Penalties from different layers stack (a forced track with `forced=1` disposition AND "Forced" title AND low frame count scores 100+100+50+50 = 300). This is intentional — stacking makes the scoring more decisive, not less correct.

**Tie-breaking:** If two tracks have the same penalty, prefer the one with more frames (more complete subtitle). If still tied, first in stream order.

### Decision 2: 25% threshold for frame/byte heuristic

**Choice:** A track is considered suspiciously small if it has less than 25% of the maximum frame (or byte) count among all English tracks of the same language.

**Rationale:** In the real example, the forced track has 23 frames vs 567 (4%). Even a generous 25% threshold catches this with wide margin. The threshold only applies when multiple English tracks exist (nothing to compare against with a single track).

### Decision 3: Frame/byte stats from MKV tags, not post-extraction

**Choice:** Read `NUMBER_OF_FRAMES` and `NUMBER_OF_BYTES` from `StreamInfo.Tags` (populated by ffprobe from MKV statistics). Do not extract-then-count.

**Alternative considered:** Extract each English subtitle, count lines, compare. Rejected — extraction is expensive (disk I/O, ffmpeg invocation per track) and defeats the purpose of selecting *before* extracting.

**Fallback:** If tags are missing (non-MKV, or MKV without statistics), frame/byte layer contributes 0 penalty. The other three layers still function.

### Decision 4: Two-pass approach within `detectEnglishSubtitle`

**Choice:** First pass collects all English candidate tracks with their metadata. Second pass computes penalties (needs the full candidate list to determine max frames/bytes for the heuristic threshold). Return the candidate with the lowest penalty.

This replaces the current single-pass "keep best so far" approach, because the frame/byte heuristic requires knowing the max across all candidates before scoring any individual one.

## Risks / Trade-offs

- **[Risk] Frame/byte tags missing for non-MKV containers** → Mitigation: Layer contributes 0 penalty; disposition and title layers still work. Three layers is sufficient.
- **[Risk] A legitimate short subtitle (e.g., a movie with very little dialogue) gets penalized by frame heuristic** → Mitigation: The 25% threshold is relative to other English tracks of the same file, not an absolute number. A single English track has nothing to compare against, so no penalty applies. Only when a dramatically smaller track exists alongside a normal one does the penalty fire.
- **[Risk] Some release groups use non-standard title tags** → Mitigation: Title keyword matching is case-insensitive and covers common variants ("forced", "signs", "signs & songs", "sdh", "cc", "hearing impaired"). The keyword list can be extended without changing the scoring architecture.
