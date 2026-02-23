# fusionn

Intelligent Media Automation Platform

## Features

- 🚀 Fast and lightweight Go application
- 🔧 Configuration management with hot-reload support
- 📝 Structured logging with Zap
- 🐳 Docker and Docker Compose support
- 🔄 Graceful shutdown
- 🌐 HTTP API with Gin framework
- 🎬 Sonarr/Radarr webhook integration for subtitle automation
- 🔤 Automatic dual-language subtitle generation (English + Chinese)
- 🌐 Redis-based translation queue for external translation services
- 🔠 Automatic font embedding in dual-language subtitles

## Quick Start

### Prerequisites

- Go 1.23 or later
- Docker (optional)
- Redis server (if using translation queue feature)

### Local Development

1. Copy the example configuration:

```bash
cp config/config.example.yaml config/config.yaml
```

2. Run the application:

```bash
make run
```

Or build and run:

```bash
make build
./fusionn
```

### Docker

Build and run with Docker Compose:

```bash
docker compose up -d
```

View logs:

```bash
docker compose logs -f
```

Stop:

```bash
docker compose down
```

## Configuration

Configuration is loaded from `config/config.yaml` by default. You can override this with the `CONFIG_PATH` environment variable.

The application supports hot-reload - changes to the config file are automatically detected and applied without restart.

### Subtitle Processing

Enable automatic dual-language subtitle generation:

```yaml
subtitle:
  enabled: true
  # ... other subtitle settings
```

### Redis Integration

For automatic translation of missing Chinese subtitles, configure Redis to queue jobs for external translation services (e.g., fusionn-subs):

```yaml
redis:
  host: "redis"
  port: 6379
  password: ""
  database: 0
  queue_key: "fusionn:translation_queue"
```

When Chinese subtitles are missing from media files, fusionn will:
1. Extract the English subtitle track to the media directory (`.eng.srt`)
2. Queue a translation job to Redis
3. Wait for the translation service to callback with the translated Chinese subtitle
4. Merge both subtitles into a dual-language `.zh-CN.ass` file

The translation service (like [fusionn-subs](https://github.com/weizsw/fusionn-subs)) polls the Redis queue, translates subtitles using AI, and callbacks to fusionn when complete.

See `config/config.example.yaml` for full configuration options.

### Font Embedding

Automatically embed fonts into ASS subtitle files for better portability and consistent rendering across all devices:

**Setup:**

1. Enable font embedding in your config:

```yaml
subtitle:
  font_embedding:
    enabled: true
    fonts_dir: "/app/fonts"
    timeout_seconds: 300  # Optional, defaults to 300
```

2. Mount your fonts directory in `docker-compose.yml`:

```yaml
volumes:
  - ./fonts:/app/fonts:ro
```

3. Add your font files (TTF/OTF) to the `./fonts` directory

**How it works:**

fusionn will automatically:
- Detect fonts referenced in ASS files
- Subset fonts to only characters actually used (typically 90%+ size reduction)
- Embed subsetted fonts directly into the ASS file using UUEncode format
- Fall back gracefully if fonts are missing (subtitle still works, just without embedded fonts)

**Benefits:**
- **Portable** - Embedded ASS files work on any device without requiring font installation
- **Smaller** - Font subsetting dramatically reduces file sizes
- **Automatic** - No manual intervention needed, happens during subtitle processing

**Troubleshooting:**
- **Missing fonts warning**: Place required font files in your fonts directory and ensure the font family names match those used in the ASS file
- **Timeout**: Increase `timeout_seconds` if processing large font files
- **Feature not working**: Check logs for `⚠️ fusionn-font binary not found` - this indicates the Docker image wasn't built correctly

## API Endpoints

- `GET /health` - Health check
- `GET /api/v1/status` - Application status and version
- `POST /api/v1/webhook/sonarr` - Sonarr import webhook
- `POST /api/v1/webhook/radarr` - Radarr import webhook
- `POST /api/v1/callback/translation` - Translation completion callback (from fusionn-subs)

## Development

### Build

```bash
make build
```

### Test

```bash
make test
```

### Lint

```bash
make lint
```

### Clean

```bash
make clean
```

## Environment Variables

- `ENV` - Set to `production` for production mode (default: development)
- `CONFIG_PATH` - Path to configuration file (default: `config/config.yaml`)
- `FUSIONN_*` - Any config value can be overridden with env vars (e.g., `FUSIONN_SERVER_PORT=9090`)

## License

MIT
