## Why

fusionn currently queues translation jobs to Redis when Chinese subtitles are missing, but has no worker to process them. fusionn-subs is a separate translation service that polls Redis and translates English subtitles to Chinese using AI. This integration connects the two services to enable automatic dual-subtitle generation for media without Chinese subtitles.

## What Changes

- Update Redis queue message format to include extracted English subtitle path
- Modify fusionn extractor to write subtitles to media directory instead of temp directory
- Add callback endpoint in fusionn to receive translation completion notifications
- Update fusionn-subs job message structure to match fusionn's queue format
- Add retry logic for translation attempts and callback requests in fusionn-subs
- Update callback payload to include job tracking information
- Configure both services to share media library filesystem via Docker volumes

## Capabilities

### New Capabilities
- `translation-callback`: Handle callbacks from fusionn-subs when translation completes

### Modified Capabilities
- `translation-queue`: Update message format to include subtitle path, remove callback URL
- `subtitle-analyzer`: Extract subtitles to media directory with standardized naming

## Impact

**fusionn affected components:**
- `internal/queue/redis_client.go` - TranslationJob struct
- `internal/service/subtitle/analyzer.go` - Subtitle extraction paths
- `internal/service/subtitle/processor_translation_queue.go` - Queue message construction
- `internal/handler/` - New callback handler
- `config/config.example.yaml` - Redis configuration

**fusionn-subs affected components:**
- `internal/types/job.go` - JobMessage struct
- `internal/client/callback/client.go` - Callback payload and retry logic
- `internal/service/worker/worker.go` - Job processing
- `internal/service/translator/` - Translation retry logic
- `config/config.example.yaml` - Retry configuration

**Deployment:**
- Both services must mount same media library volume
- Redis server required for job queue
- docker-compose.yml configuration updates
