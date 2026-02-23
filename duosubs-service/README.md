# DuoSubs HTTP Service

A lightweight FastAPI service that runs DuoSubs on the host machine, enabling Metal GPU acceleration on macOS.

## Why?

Docker on macOS cannot access Metal GPU. This service runs natively on the host with full GPU access, while `fusionn` runs in Docker and calls it via HTTP.

## Setup

### 1. Install Dependencies

```bash
cd duosubs-service
python3 -m venv venv
source venv/bin/activate
pip install -r requirements.txt
```

### 2. Configure Environment

```bash
export HOST_MEDIA_PATH="/path/to/your/media"  # Same path as Docker volume
export DUOSUBS_PORT=8765
```

### 3. Run Service

```bash
python service.py
```

The service will start on `http://0.0.0.0:8765`

## Usage

### Health Check

```bash
curl http://localhost:8765/health
```

### Merge Subtitles

```bash
curl -X POST http://localhost:8765/merge \
  -H "Content-Type: application/json" \
  -d '{
    "primary_path": "/data/media/tv/Show/show.chs.srt",
    "secondary_path": "/data/media/tv/Show/show.eng.srt",
    "output_dir": "/data/media/tv/Show",
    "container_prefix": "/data",
    "host_prefix": "/Users/you/media"
  }'
```

## Docker Integration

Update `fusionn` config to use this service:

```yaml
subtitle:
  duosubs:
    mode: "http"  # Use HTTP instead of local binary
    url: "http://host.docker.internal:8765"  # macOS host from container
    container_path_prefix: "/data"
    host_path_prefix: "/Users/you/media"
```

## Path Mapping

The service automatically translates paths between container and host:

- **Container**: `/data/media/tv/Show/file.srt`
- **Host**: `/Users/you/media/media/tv/Show/file.srt`

## Production Deployment

For production, use a process manager:

```bash
# Using systemd (Linux)
sudo systemctl enable duosubs-service
sudo systemctl start duosubs-service

# Using launchd (macOS)
# See: https://www.launchd.info/
```
