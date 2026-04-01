# Bazarr Subtitle Search Integration

**Date:** 2026-04-01
**Status:** Draft

## Problem

When Sonarr/Radarr imports media without embedded Chinese subtitles, fusionn immediately falls back to machine translation via fusionn-subs. Human-translated subtitles from online sources (OpenSubtitles, Subscene, etc.) are generally higher quality than machine translations, but fusionn has no way to check for them first.

Bazarr is already deployed and configured with Chinese subtitle providers. It successfully finds subtitles for some media, but there is no integration between fusionn and Bazarr — they operate independently.

## Solution

Insert a new `BazarrSearchProcessor` into the analyze pipeline between `SDHFilter` and `TranslationQueueProcessor`. When no Chinese subtitle is found in the media container, the processor calls Bazarr's API to search for one. If Bazarr finds and downloads a Chinese subtitle, the pipeline continues to merge. If not, the existing translation fallback proceeds as before.

## Pipeline Position

```
Analyzer → Extractor → SDHFilter → [BazarrSearchProcessor] → TranslationQueueProcessor
```

The new processor only activates when:
- Bazarr integration is enabled in config
- English subtitle was found (no point searching Chinese if there's no English to merge with)
- No Chinese subtitle was found by the analyzer (`Analysis.ChineseTrack == nil`)
- `ChineseSubPath` is empty (not already set by a prior step)
- The required Sonarr/Radarr IDs are present in the processing context

If any condition is not met, the processor is a no-op.

### TranslationQueueProcessor Interaction

`TranslationQueueProcessor.ShouldRun` currently checks `Analysis.ChineseTrack == nil`. When Bazarr finds a subtitle, `ChineseSubPath` is set but `Analysis.ChineseTrack` remains nil (the track was not in the media container). The `ShouldRun` condition must be extended to also require `pctx.ChineseSubPath == ""`, so translation is skipped when Bazarr already provided a Chinese subtitle.

## Bazarr API Interaction

Bazarr's REST API (flask-restx, auth via `X-API-KEY` header) provides synchronous subtitle search endpoints. The PATCH call blocks until all configured providers have been searched, eliminating the need for polling.

### TV Episodes

1. **Trigger search:** `PATCH /api/episodes/subtitles` with form/query params: `seriesid` (int), `episodeid` (int), `language` (string, e.g. `"zh"`), `hi` (string `"False"`), `forced` (string `"False"`). Note: Bazarr parses `hi` and `forced` as **strings**, not booleans.
2. **Check result:** `GET /api/episodes?episodeid[]=X` — response is a `{"data": [...]}` array. Each element has `subtitles` (list of existing subs with paths) and `missing_subtitles` (list of languages still needed).
3. The PATCH returns **204 with no body** — the subtitle state must be read from the subsequent GET.

### Movies

1. **Trigger search:** `PATCH /api/movies/subtitles` with form/query params: `radarrid` (int), `language` (string, e.g. `"zh"`), `hi` (string `"False"`), `forced` (string `"False"`).
2. **Check result:** `GET /api/movies?radarrid[]=X` — response is `{"data": [...], "total": N}` wrapper. Each element has `subtitles` and `missing_subtitles` fields.
3. Same as episodes: PATCH returns 204 with no body.

### Subtitle File Pickup

Bazarr places downloaded subtitles as sidecar files next to the video (e.g., `video.zh.srt`). After a successful search:
1. Scan the video's parent directory for Chinese `.srt` files relative to `VideoPath` (most reliable — avoids path mapping mismatches between Bazarr and fusionn container mounts)
2. Optionally cross-reference with the `subtitles` field from Bazarr's GET response for validation

**Path mapping caveat:** Bazarr stores and returns paths after applying its own path mappings. These strings may not match fusionn's mount layout. Prefer scanning relative to `VideoPath` (which fusionn already knows) rather than trusting Bazarr's stored paths directly.

## Data Flow Changes

### Webhook Payload Parsing

Sonarr and Radarr webhooks already include numeric IDs that Bazarr uses as its primary identifiers. Fusionn currently ignores them.

**Sonarr additions:**
- `series.id` → `SonarrSeriesID`
- `episodes[].id` → `SonarrEpisodeID` — Sonarr webhook payloads can include multiple episodes (e.g., multi-episode files). Use the first episode's ID since each webhook corresponds to a single `episodeFile.path`.

**Radarr additions:**
- `movie.id` → `RadarrID`

### ProcessingContext Additions

```go
SonarrSeriesID  int
SonarrEpisodeID int
RadarrID        int
```

### ProcessMedia Signature

Replace positional arguments with a params struct:

```go
type MediaParams struct {
    Path            string
    MediaType       string
    Title           string
    SonarrSeriesID  int
    SonarrEpisodeID int
    RadarrID        int
}
```

## Configuration

New `bazarr` section in `config.yaml`:

```yaml
bazarr:
  enabled: false
  url: "http://bazarr:6767"
  api_key: ""
  search_timeout: 180   # seconds — max time for the PATCH call to return
  language_code: "zh"   # Bazarr language code for Chinese subtitles
```

## New Components

### `internal/client/bazarr/client.go`

Thin HTTP client with four methods:

- `SearchEpisodeSubtitle(ctx, seriesID, episodeID, language) error`
- `SearchMovieSubtitle(ctx, radarrID, language) error`
- `GetEpisodeSubtitles(ctx, episodeID) (*EpisodeInfo, error)`
- `GetMovieSubtitles(ctx, radarrID) (*MovieInfo, error)`

The HTTP client's timeout is set from `bazarr.search_timeout`.

### `internal/service/subtitle/processor_bazarr_search.go`

Pipeline processor implementing the `Processor` interface:

- `Name()` → `"BazarrSearch"` (add to `processorEmojis` map in `pipeline.go` for consistent logging)
- `ShouldRun(pctx)` → true when enabled, English track present, no Chinese track found, `ChineseSubPath` is empty, and IDs are present
- `Process(ctx, pctx)` → triggers search, checks result, sets `pctx.ChineseSubPath` if found

## Error Handling

All Bazarr failures are non-fatal. The processor logs a warning and returns `nil`, allowing `TranslationQueueProcessor` to handle the fallback:

- Network errors (Bazarr unreachable)
- Authentication failure (wrong API key → 401)
- Episode/movie not found in Bazarr (404 — sync lag between Sonarr/Radarr and Bazarr)
- Save/permission failure (409 — Bazarr couldn't write the subtitle file)
- Other server errors (500)
- Timeout (provider search takes too long)
- Subtitle file not accessible after download (path mapping mismatch or permission issue)

## Wiring

In `cmd/fusionn/main.go`:
1. Create `bazarr.Client` when `bazarr.enabled` is true
2. Pass client to `subtitle.NewService()`

In `internal/service/subtitle/service.go`:
1. Accept optional `bazarr.Client` in `NewService()`
2. Insert `BazarrSearchProcessor` after `SDHFilter` and before `TranslationQueueProcessor`

## Scope Boundaries

**In scope:**
- Bazarr integration for Chinese subtitle search only
- TV episodes (Sonarr) and movies (Radarr)
- Graceful fallback to translation on any failure

**Out of scope:**
- English subtitle search via Bazarr
- Bazarr webhook/callback setup (synchronous API is sufficient)
- Any changes to the merge pipeline
- Subtitle provider configuration in Bazarr (already configured by user)
