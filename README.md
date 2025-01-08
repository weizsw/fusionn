# Fusionn

A Go-based subtitle processing service with translation capabilities, error tracking, and health monitoring.

## Features

- Subtitle merging and batch processing
- Async subtitle processing
- Multiple translation providers (LLM, DeepLX)
- Redis caching
- SQLite storage
- Sentry error tracking
- Health monitoring
- CORS support
- Live reload development

## API Endpoints

```go
r.GET("/", s.HelloWorldHandler)
r.GET("/health", s.healthHandler)
r.POST("/api/v1/merge", wrapHandler(s.mergeHandler.Merge))
r.POST("/api/v1/batch", wrapHandler(s.batchHandler.Batch))
r.POST("/api/v1/async_merge", wrapHandler(s.asyncMergeHandler.AsyncMerge))
```

## API Documentation

### POST /api/v1/merge

Extracts and processes subtitle files.

**Request Body**

```json
{
    "file_path": "/path/to/video/file",
    "series_tvdbid": "12345",    // Optional, for TVDB lookup
    "season_number": "1",         // Optional, for TVDB lookup
    "episode_numbers": "1"        // Optional, for TVDB lookup
}
```

**Response**

```json
{
    "message": "success"
}
```

### POST /api/v1/batch

Processes multiple files in a directory.

**Request Body**

```json
{
    "path": "/path/to/directory"
}
```

**Response**

```json
{
    "message": "success"
}
```

### POST /api/v1/async_merge

Asynchronously merges Chinese and English subtitles with video.

**Request Body**

```json
{
    "chs_subtitle_path": "/path/to/chinese.srt",
    "eng_subtitle_path": "/path/to/english.srt", 
    "video_path": "/path/to/video.mkv"
}
```

**Response**

```json
{
    "message": "success"
}
```

## Error Responses

All endpoints may return error responses in this format:

```json
{
    "error": "Error message description"
}
```

Common HTTP status codes:

- 200: Success
- 400: Bad Request (invalid parameters)
- 500: Internal Server Error

## Installation

1. Clone the repository
2. Install dependencies:

```bash
go mod download
```

3. Set up development tools:

```bash
make setup
```

## Configuration

Create a `config.yml` file in the project root or `configs/` directory:

```yaml
apprise:
  enabled: true
  url: http://your-apprise-url

sqlite:
  enabled: true
  path: ./sqlite.db

translate:
  enabled: true
  provider: llm  # or deeplx

deeplx:
  local: false
  url: http://your-deeplx-url

llm:
  base: https://your-llm-base
  endpoint: /chat/completions
  api_key: your-api-key
  model: your-model
  language: Chinese

redis:
  addr: 127.0.0.1:6379
  password: your-password
  db: 0

sentry:
  enabled: true
  dsn: your-sentry-dsn
  sample_rate: 0.1
```

## Development Commands

```bash
# Show available commands
make help

# Build and test
make all

# Build only
make build

# Run tests
make test

# Run application
make run

# Live reload development
make watch

# Initialize database
make init-db

# Generate wire dependencies
make wire

# Clean build artifacts
make clean
```

## Development

The project uses Air for live reloading during development. Configuration is in `.air.toml`.

## Error Handling

The service uses Sentry for error tracking and panic recovery:

```go
func RecoverWithSentry() gin.HandlerFunc {
    return func(c *gin.Context) {
        defer func() {
            if err := recover(); err != nil {
                if hub := sentrygin.GetHubFromContext(c); hub != nil {
                    hub.Recover(err)
                }
                c.AbortWithStatus(http.StatusInternalServerError)
            }
        }()
        c.Next()
    }
}
```

## Contributing

1. Fork the repository
2. Create your feature branch
3. Commit your changes
4. Push to the branch
5. Create a Pull Request

## License

This project is licensed under the MIT License.
