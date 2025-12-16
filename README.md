# fusionn

Intelligent Media Automation Platform

## Features

- 🚀 Fast and lightweight Go application
- 🔧 Configuration management with hot-reload support
- 📝 Structured logging with Zap
- 🐳 Docker and Docker Compose support
- 🔄 Graceful shutdown
- 🌐 HTTP API with Gin framework

## Quick Start

### Prerequisites

- Go 1.23 or later
- Docker (optional)

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

## API Endpoints

- `GET /health` - Health check
- `GET /api/v1/status` - Application status and version

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
