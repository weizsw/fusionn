# SDH Subtitle Filter

## Problem

When only SDH (Subtitles for the Deaf and Hard of Hearing) subtitle tracks are available, the extracted English SRT contains descriptive annotations that clutter the viewing experience:

- `[music playing]`, `[door slams]` — sound effect descriptions in square brackets
- `(sighs)`, `(phone ringing)` — action/sound cues in parentheses
- `JOHN:`, `MAN ON PHONE:` — speaker identification labels
- Standalone ALL CAPS lines like `SCREAMING` — non-dialogue descriptions

These annotations are useful for hearing-impaired viewers but distracting when the subtitle is being used as a base for bilingual (EN+ZH) subtitle generation.

## Decision

Use `cleanit` (Python CLI tool) with the `no-sdh` rule to filter SDH content from extracted English SRT files. This runs as a new processor in the analyze pipeline, only when the selected English track is detected as SDH.

Lyrics are preserved — `cleanit`'s `no-lyrics` tag is a separate rule that we do not invoke.

### Why `cleanit` over native Go

- Battle-tested SDH rules with active maintenance
- Handles edge cases (mixed dialogue+annotation lines, multi-line entries, empty entry cleanup)
- YAML-configurable if rules need tuning later
- Follows the project's existing pattern of shelling out to CLI tools (ffmpeg, opencc, duosubs, fusionn-font)

## Architecture

### Pipeline Placement

The SDH filter slots into the **analyze pipeline** between extraction and translation queue:

```
AnalyzerProcessor → ExtractorProcessor → SDHFilterProcessor → TranslationQueueProcessor
```

The cleaned English SRT is what gets sent downstream for translation and/or merging.

### Components

#### 1. `Track.IsSDH` field (`analyzer.go`)

Add a boolean `IsSDH` field to the `Track` struct. Set it in `detectEnglishSubtitle` on the **selected** (winning) track using the existing helpers:

```go
IsSDH: isHearingImpaired(candidates[bestIdx].stream) || isSDHByTitle(candidates[bestIdx].title)
```

This reuses the same disposition/title logic already in `analyzer.go` without changing scoring behavior.

#### 2. `internal/executor/cleanit.go`

New executor following the same pattern as `ffmpeg.go` and `opencc.go`:

- `RunCleanit(ctx, srtPath)` — executes `cleanit -t no-sdh <path>`
- `IsCleanitAvailable()` — checks if `cleanit` binary is in PATH

`cleanit` modifies the SRT file in-place.

#### 3. `processor_sdh_filter.go`

New pipeline processor:

- **Name**: `"SDHFilter"`
- **ShouldRun**: `pctx.Analysis.EnglishTrack != nil && pctx.Analysis.EnglishTrack.IsSDH`
- **Process**: Copies the SRT to a temp file, runs `executor.RunCleanit(ctx, tempPath)`, then renames the result over the original (atomic replace). On failure, the original SRT is untouched.
- Non-fatal: on any `cleanit` error, logs a warning and **returns `nil`** so downstream processors still run with the unfiltered SRT. This is critical because `Pipeline.Execute` aborts on non-nil errors.

#### 4. `service.go`

Insert `NewSDHFilterProcessor()` into the analyze pipeline after `NewExtractorProcessor()`.

Log a warning at startup if `cleanit` is not available (mirrors the `fusionn-font` availability check pattern).

### Data Flow

```
Video file
  ↓ ffprobe (detect tracks, identify SDH via disposition/title)
  ↓ ffmpeg (extract English SRT)
  ↓ cleanit -t no-sdh (in-place filter, only if track is SDH)
  ↓ translation queue or merge pipeline
```

### Error Handling

| Scenario | Behavior |
|----------|----------|
| `cleanit` not installed | Log warning at startup; processor skips at runtime |
| `cleanit` fails on a file | Log warning, return `nil` — continue with unfiltered SRT (original file untouched due to temp+rename) |
| Track is not SDH | Processor skips entirely |

### What Gets Removed (by `cleanit no-sdh`)

- Sound descriptions in `[brackets]` and `(parentheses)`
- Speaker labels (e.g., `NARRATOR:`, `MAN:`)
- ALL CAPS non-dialogue descriptors
- Entries that become empty after removal (with re-numbering)

### What Gets Preserved

- All dialogue text
- Lyrics (♪/♫ markers and associated text)
- Timing information
- Mixed lines: only the SDH portion is stripped, dialogue is kept (e.g., `(sighs) I'm tired` → `I'm tired`)

### Callback Path Contract

`ProcessWithSubtitles` (translation callback) enqueues merge jobs without running the analyze pipeline. The contract: only analyze-pipeline outputs are SDH-cleaned. The callback receives the same on-disk English SRT that was already cleaned during the analyze phase, so no additional filtering is needed on that path.

### Deployment

`cleanit` requires **Python 3.11+**. Install via `pip install cleanit` or `pipx install cleanit`. The `cleanit` binary must be on PATH. For Docker deployments, add `pip install cleanit` to the image build.

### Limitations

- `isSDHByTitle` uses substring matching for `"cc"` which could false-positive on unrelated titles. This is an existing analyzer behavior, not introduced by this change.
- Some SDH streams lack both `hearing_impaired` disposition and SDH title keywords — these won't be detected as SDH, so the filter won't run. This is acceptable: false negatives (leaving SDH content) are preferable to false positives (filtering non-SDH subtitles).
