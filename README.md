# Fusionn

A Go-based subtitle processing service with translation capabilities, error tracking, and health monitoring.

## Features

- Subtitle merging and batch processing
- Async subtitle processing
- Multiple translation providers (LLM, DeepLX)
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

### Option 1: Docker Compose (Recommended)

1. Create a `docker-compose.yml` file:

```yaml
services:
  fusionn:
    image: ghcr.io/weizsw/fusionn:latest
    container_name: fusionn
    environment:
      - PUID=501
      - PGID=20
      - TZ=Asia/Shanghai
      - UMASK=022
    volumes:
      - /path/to/media:/data     # Mount your media directory
      - ./configs:/app/configs   # Mount config directory
    ports:
      - 4664:4664
    restart: unless-stopped
    depends_on:
      - redis
    networks:
      - fusionn_default

  gpt-subtrans:
    image: ghcr.io/weizsw/gpt-subtrans:latest
    container_name: gpt-subtrans
    volumes:
      - ./gpt-subtrans/configs:/app/configs
      - /path/to/media:/data
    environment:
      - PUID=501
      - PGID=20
      - TZ=Asia/Shanghai
      - CONFIG_PATH=/app/configs/config.json
      - DOCKER_ENV=true
    restart: unless-stopped
    networks:
      - fusionn_default

  redis:
    image: redis:alpine
    container_name: fusionn-redis
    command: redis-server --save "" --maxmemory 100mb --maxmemory-policy allkeys-lru
    ports:
      - "6379:6379"
    restart: unless-stopped
    networks:
      - fusionn_default

  fusionn-ui:
    image: ghcr.io/weizsw/fusionn-ui:latest
    container_name: fusionn-ui
    ports:
      - "5664:3000"
    restart: unless-stopped
    depends_on:
      - fusionn
    networks:
      - fusionn_default

networks:
  fusionn_default:
    name: fusionn_default
    driver: bridge
```

2. Create configuration:

```bash
mkdir -p configs gpt-subtrans/configs
```

3. Create `configs/config.yml` with your configuration (see Configuration section)

4. Start the services:

```bash
docker compose up -d
```

The UI will be available at `http://localhost:5664`

### Option 2: Manual Installation

1. Clone the repository
2. Install dependencies:

```bash
go mod download
```

3. Set up development tools:

```bash
make setup
```

4. Configure the application (see Configuration section)

5. Start the application:

```bash
make run
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
