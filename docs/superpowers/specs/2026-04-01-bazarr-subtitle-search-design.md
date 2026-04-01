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
- No Chinese subtitle was found by the analyzer
- The required Sonarr/Radarr IDs are present in the processing context

If any condition is not met, the processor is a no-op.

## Bazarr API Interaction

Bazarr's REST API (flask-restx, auth via `X-API-KEY` header) provides synchronous subtitle search endpoints. The PATCH call blocks until all configured providers have been searched, eliminating the need for polling.

### TV Episodes

1. **Trigger search:** `PATCH /api/episodes/subtitles` with `seriesid`, `episodeid`, `language=zh`, `hi=False`, `forced=False`
2. **Check result:** `GET /api/episodes?episodeid[]=X` — inspect `subtitles` and `missing_subtitles` fields

### Movies

1. **Trigger search:** `PATCH /api/movies/subtitles` with `radarrid`, `language=zh`, `hi=False`, `forced=False`
2. **Check result:** `GET /api/movies?radarrid[]=X` — inspect `subtitles` and `missing_subtitles` fields

### Subtitle File Pickup

Bazarr places downloaded subtitles as sidecar files next to the video (e.g., `video.zh.srt`). After a successful search:
1. Use the subtitle path from Bazarr's GET response if available
2. Fall back to scanning the video's directory for newly created Chinese `.srt` files

## Data Flow Changes

### Webhook Payload Parsing

Sonarr and Radarr webhooks already include numeric IDs that Bazarr uses as its primary identifiers. Fusionn currently ignores them.

**Sonarr additions:**
- `series.id` → `SonarrSeriesID`
- `episodes[].id` → `SonarrEpisodeID`

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

- `Name()` → `"BazarrSearch"`
- `ShouldRun(pctx)` → true when enabled, no Chinese subtitle found, and IDs are present
- `Process(ctx, pctx)` → triggers search, checks result, sets `pctx.ChineseSubPath` if found

## Error Handling

All Bazarr failures are non-fatal. The processor logs a warning and returns `nil`, allowing `TranslationQueueProcessor` to handle the fallback:

- Network errors (Bazarr unreachable)
- API errors (auth failure, 404, 500)
- Timeout (provider search takes too long)
- Subtitle file not accessible after download

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
