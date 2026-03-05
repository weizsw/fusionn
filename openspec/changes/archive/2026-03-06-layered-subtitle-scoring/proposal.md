## Why

The English subtitle selector picks the first non-SDH English track by stream order. When a video contains a "Forced" track (foreign-dialogue-only translations, ~23 entries) before the full English track (~567 entries), the forced track wins. This produces a nearly empty subtitle merge that misses all regular dialogue.

## What Changes

- Replace the binary priority system (non-SDH=1, SDH=2) with a layered penalty scoring system that evaluates four independent signals: disposition flags, title keywords, frame count heuristic, and byte size heuristic
- Penalize forced/signs tracks heavily so they are never selected when a full English track exists
- Add graceful degradation: if disposition flags are missing, title keywords catch it; if both are missing, frame/byte count acts as a safety net
- Final priority order: Regular English > SDH English > Forced/Signs English

## Capabilities

### New Capabilities

_(none)_

### Modified Capabilities

- `subtitle-analyzer`: English subtitle identification requirements change from a 2-tier priority (non-SDH / SDH) to a 4-layer penalty scoring system that accounts for forced tracks, disposition flags, title keywords, and frame/byte count heuristics

## Impact

- `internal/service/subtitle/analyzer.go` — `matchEnglishTrack` and `detectEnglishSubtitle` rewritten with scoring logic
- `internal/executor/ffmpeg.go` — `StreamInfo` already parses `Disposition` and `Tags` (including `NUMBER_OF_FRAMES`, `NUMBER_OF_BYTES`); no schema change needed, just consumption
- Existing tests for English subtitle selection will need updating to cover forced-track scenarios
