# Sonarr/Radarr Webhook Reference

## Sonarr Webhook Configuration

**Settings → Connect → Add → Webhook**

- **URL**: `http://fusionn:8080/api/v1/webhook/sonarr`
- **Method**: POST
- **Triggers**: 
  - ✅ On Download
  - ✅ On Upgrade
  - ❌ On Rename (ignored by fusionn)
  - ❌ On Test (ignored by fusionn)

## Sonarr Webhook Payload

```json
{
  "eventType": "Download",
  "series": {
    "id": 1,
    "title": "Series Name",
    "path": "/tv/Series Name",
    "tvdbId": 12345,
    "tvMazeId": 67890,
    "imdbId": "tt1234567"
  },
  "episodes": [
    {
      "id": 123,
      "episodeNumber": 1,
      "seasonNumber": 1,
      "title": "Episode Title",
      "airDate": "2024-01-01",
      "airDateUtc": "2024-01-01T00:00:00Z"
    }
  ],
  "episodeFile": {
    "id": 456,
    "relativePath": "Season 01/Series.Name.S01E01.1080p.WEB-DL.mkv",
    "path": "/tv/Series Name/Season 01/Series.Name.S01E01.1080p.WEB-DL.mkv",
    "quality": "WEBDL-1080p",
    "qualityVersion": 1,
    "releaseGroup": "GROUP",
    "sceneName": "Series.Name.S01E01.1080p.WEB-DL-GROUP"
  }
}
```

**Key Fields for fusionn:**
- `eventType`: Filter to "Download" and "Upgrade" only
- `episodeFile.path`: **Absolute path to video file** (used for ffprobe/ffmpeg)
- `series.title`, `episodes[0].episodeNumber`, `episodes[0].seasonNumber`: Metadata for logging

## Radarr Webhook Configuration

**Settings → Connect → Add → Webhook**

- **URL**: `http://fusionn:8080/api/v1/webhook/radarr`
- **Method**: POST
- **Triggers**:
  - ✅ On Download
  - ✅ On Upgrade
  - ❌ On Rename (ignored by fusionn)
  - ❌ On Test (ignored by fusionn)

## Radarr Webhook Payload

```json
{
  "eventType": "Download",
  "movie": {
    "id": 1,
    "title": "Movie Title",
    "year": 2024,
    "folderPath": "/movies/Movie Title (2024)",
    "tmdbId": 12345,
    "imdbId": "tt1234567"
  },
  "movieFile": {
    "id": 456,
    "relativePath": "Movie.Title.2024.1080p.BluRay.x264-GROUP.mkv",
    "path": "/movies/Movie Title (2024)/Movie.Title.2024.1080p.BluRay.x264-GROUP.mkv",
    "quality": "Bluray-1080p",
    "qualityVersion": 1,
    "releaseGroup": "GROUP",
    "sceneName": "Movie.Title.2024.1080p.BluRay.x264-GROUP"
  }
}
```

**Key Fields for fusionn:**
- `eventType`: Filter to "Download" and "Upgrade" only
- `movieFile.path`: **Absolute path to video file** (used for ffprobe/ffmpeg)
- `movie.title`, `movie.year`, `movie.imdbId`: Metadata for logging

## Implementation Notes

### Event Type Filtering

Only process these event types:
- `"Download"` - New media downloaded and imported
- `"Upgrade"` - Existing media upgraded to better quality

Ignore these event types:
- `"Test"` - Test webhook (sent when configuring webhook)
- `"Rename"` - File renamed (no need to re-process subtitles)
- `"Grab"` - Download started (file not ready yet)

### File Path Handling

Both Sonarr and Radarr provide:
- `path`: **Absolute filesystem path** (e.g., `/tv/Series/Season 01/Episode.mkv`)
- `relativePath`: Relative to library root (e.g., `Season 01/Episode.mkv`)

**Use `path` (absolute path) for all file operations.**

### Error Scenarios

1. **File Not Found**: If `path` doesn't exist on filesystem, return HTTP 404
2. **Invalid Event Type**: If event type is "Test" or "Rename", return HTTP 200 (acknowledge but don't process)
3. **Missing Required Fields**: If `path` is missing, return HTTP 400 with error details

### Example Webhook Test

**Sonarr Test:**
```bash
curl -X POST http://localhost:8080/api/v1/webhook/sonarr \
  -H "Content-Type: application/json" \
  -d '{
    "eventType": "Download",
    "episodeFile": {
      "path": "/tv/TestSeries/S01E01.mkv"
    },
    "series": {
      "title": "Test Series"
    },
    "episodes": [
      {"episodeNumber": 1, "seasonNumber": 1}
    ]
  }'
```

**Radarr Test:**
```bash
curl -X POST http://localhost:8080/api/v1/webhook/radarr \
  -H "Content-Type: application/json" \
  -d '{
    "eventType": "Download",
    "movieFile": {
      "path": "/movies/TestMovie/Movie.mkv"
    },
    "movie": {
      "title": "Test Movie",
      "year": 2024
    }
  }'
```

## References

- **Sonarr Webhook Docs**: https://wiki.servarr.com/sonarr/settings#connect
- **Radarr Webhook Docs**: https://wiki.servarr.com/radarr/settings#connect
- **Webhook Testing**: Use Sonarr/Radarr's "Test" button in webhook settings (fusionn will ignore test events)

