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
- Python 3 (for the word-alignment HTTP tests)
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

The configuration manager detects file changes. Subtitle services and queued merge workers capture their engine, connection, and style settings at startup: restart fusionn after changing these settings.

### Subtitle Processing

Enable automatic dual-language subtitle generation:

```yaml
subtitle:
  enabled: true
  # ... other subtitle settings
```

### Native LLM merge or DuoSubs

New configurations default to the native Go ID-mapping engine. No Python merger,
DuoSubs service, or embedding model is needed for LLM mode. An optional CPU
word-alignment container supplies times for long subtitles. Select explicitly:

```yaml
subtitle:
  enabled: true
  engine: "llm" # use "duosubs" to keep the existing local/HTTP engine
  llm:
    base_url: "http://host.docker.internal:3425/v1" # Docker Desktop -> host
    model: "group/auto-gfmodel"
    api_key_env: "" # optional environment variable NAME, never a key value
    cache_enabled: true
    cache_dir: "/data/llm-subtitle-cache"
    en_chars: 65
    zh_chars: 20 # tested setting; the code default is 25
```

Use `http://127.0.0.1:3425/v1` when running on the host itself. Linux containers
need a reachable host address/network configuration. Mount the cache directory
persistently; style changes reuse ID mappings. Old configs with a `duosubs` block
and no `engine` continue to select DuoSubs. Keep the original DuoSubs deployment
for that mode; the fusionn image does not install a local duosubs executable.

The Compose file starts `word-align` beside fusionn and sets the internal URL.
It uses `Qwen/Qwen3-ForcedAligner-0.6B-hf` on CPU and keeps downloaded weights in
the Hugging Face volume. It does not need a host MLX service or a media mount.
Run `docker compose up -d --build` to build the service. The first request also
downloads the model. For a standalone run, set `subtitle.llm.word_align_url` to a
reachable service URL, or leave it empty to use proportional timing.
The service starts its model worker on the first request. After 300 seconds
without an alignment request, it stops the worker and releases model memory.
Set `ALIGN_IDLE_SECONDS` on `word-align` to change this interval. The next
request reloads the model from the cache. Active requests keep their worker.

Long multiline source cues use the configured `en_chars` and `zh_chars` limits.
Original single-line cues stay whole, even above these limits. They do not
request word alignment. Joined source cues still obey the limits.
The renderer keeps source text and order, holds a short language across slices,
and splits only the long side. It reuses source boundaries before adding word
boundaries. Invalid word times or a service failure use proportional timing.
Chinese proportional slices reserve the minimum reading time when the total
interval permits it. New fragments need 1.2 seconds of continuous display.
If splitting would make reading too fast, the renderer keeps the original event
and line wrapping. Speaker text uses its original breaks when wrapping is needed.

The reading limits come from the configuration. The tested S02E08 values are
`en_chars: 65` and `zh_chars: 20`. The code default for `zh_chars` remains 25.
Restart fusionn after changing these values. English length examples and the
current playback results are in the [integration guide](docs/llm-subtitle-integration.zh-CN.md#英文长度与配置调整参考).

Word times do not have a persistent cache yet. With video and an alignment URL,
each run requests word times again. The ID-mapping cache is separate.
Follow-up work is recorded in [TODO.md](TODO.md).

The shared style now uses a 1920×1080 script canvas and the experimental
Chinese-above-English defaults. Legacy 384×288 style values are migrated at load
when `play_res_y` is absent; set both play resolutions explicitly when copying new
1080p values. Fonts must be installed or embedded (e.g. Noto Sans CJK SC / DejaVu
Sans on Linux). A script canvas is not the physical resolution of a television.

See the [中文集成与迁移说明](docs/llm-subtitle-integration.zh-CN.md) for cache import,
TV letterbox geometry, supported formats, cancellation and reproducible checks.
For an isolated run without starting the server:

```bash
go run ./cmd/subtitle-merge --config config/config.yaml \
  --en /tmp/english.srt --zh /tmp/chinese.srt --out /tmp/new-bilingual-output
```

The output directory must be new. `--video` plus absolute `--en-stream` and
`--zh-stream` can extract directly into that directory; `--mapping` imports a
validated existing mapping without LLM requests. The Docker image includes
`subtitle-merge` as a standalone binary as well.

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

Queued jobs use the fusionn-subs-compatible JSON shape. The identity fields are optional but are included when Sonarr/Radarr provides them so downstream glossary reuse can key terms by stable media IDs:

```json
{
  "job_id": "uuid",
  "video_path": "/media/Show/S01E01.mkv",
  "subtitle_path": "/media/Show/S01E01.eng.srt",
  "media_title": "Show S01E01",
  "media_type": "episode",
  "source_system": "sonarr",
  "media_id": "42",
  "external_ids": {
    "sonarr": "42",
    "tvdb": "355620",
    "imdb": "tt8201186"
  },
  "season": 1,
  "episode": 1
}
```

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
