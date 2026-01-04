# Implementation Tasks

## 1. Configuration Schema

- [x] 1.1 Add subtitle configuration to `internal/config/config.go`
  - [x] 1.1.1 Define `SubtitleConfig` struct (DuoSubs model, Redis connection, Apprise, enabled flag)
  - [x] 1.1.2 Add Redis configuration (host, port, password, database, queue_key)
  - [x] 1.1.3 Add DuoSubs configuration (model name, device, timeout, output_same_dir)
  - [x] 1.1.4 Add subtitle fallback preferences (English variants, Chinese variants)
  - [x] 1.1.5 Add Apprise configuration (enabled, base_url, key, tag)
  - [x] 1.1.6 Add OpenCC configuration (enabled, config file)
- [x] 1.2 Update `config/config.example.yaml` with subtitle section (including Apprise)
- [x] 1.3 Write configuration validation tests

## 2. Webhook Handler Capability

- [x] 2.1 Create `internal/handler/webhook.go`
  - [x] 2.1.1 Implement Sonarr webhook endpoint (POST /api/v1/webhook/sonarr)
  - [x] 2.1.2 Implement Radarr webhook endpoint (POST /api/v1/webhook/radarr)
  - [x] 2.1.3 Parse webhook payload (extract file path, media type, metadata)
  - [x] 2.1.4 Validate webhook payload structure
  - [x] 2.1.5 Enqueue subtitle processing job
- [x] 2.2 Add webhook routes to `cmd/fusionn/main.go`
- [x] 2.3 Write webhook handler tests (table-driven tests for various payloads)

## 3. Subtitle Analyzer Capability & Pipeline

- [x] 3.1 Create `internal/service/subtitle/analyzer.go`
  - [x] 3.1.1 Implement ffprobe wrapper to list subtitle tracks
  - [x] 3.1.2 Detect subtitle language codes (ISO 639-1/639-2)
  - [x] 3.1.3 Identify English subtitles (eng, eng-sdh variants)
  - [x] 3.1.4 Identify Chinese subtitles (zh-Hans, zh-Hant, zho, chi variants)
  - [x] 3.1.5 Extract subtitle tracks to temporary SRT files
- [x] 3.2 Create `internal/executor/ffmpeg.go` for ffmpeg/ffprobe execution
- [x] 3.3 Create `internal/service/subtitle/pipeline.go` for extensible processor architecture
- [x] 3.4 Write analyzer tests (mock ffprobe output, test language detection)

## 4. Subtitle Merger Capability

- [x] 4.1 Create `internal/service/subtitle/processor_merger.go`
  - [x] 4.1.1 Implement fallback logic for English subtitles (eng → eng-sdh)
  - [x] 4.1.2 Implement fallback logic for Chinese subtitles (zh-Hans → zh-Hant → conversion)
  - [x] 4.1.3 Integrate OpenCC for Traditional → Simplified conversion (if needed)
  - [x] 4.1.4 Call DuoSubs via Python executor to merge subtitles
  - [x] 4.1.5 Handle DuoSubs output (merged ASS file)
  - [x] 4.1.6 Copy merged subtitle file to final destination
- [x] 4.2 Create `internal/executor/duosubs.go` for DuoSubs CLI execution
- [x] 4.3 Write merger tests (mock DuoSubs execution, test fallback logic)

## 5. Translation Queue Capability

- [x] 5.1 Create `internal/queue/redis_client.go`
  - [x] 5.1.1 Implement Redis connection with retry logic
  - [x] 5.1.2 Implement Redis queue publish (LPUSH or RPUSH)
  - [x] 5.1.3 Define translation job message schema (JSON)
- [x] 5.2 Create `internal/service/subtitle/processor_translation_queue.go`
  - [x] 5.2.1 Enqueue translation job when Chinese subtitle missing
  - [x] 5.2.2 Include callback URL in job payload
- [x] 5.3 Create `internal/handler/callback.go`
  - [x] 5.3.1 Implement translation callback endpoint (POST /api/v1/callback/translation)
  - [x] 5.3.2 Validate callback payload
  - [x] 5.3.3 Trigger subtitle merge after translation completes
- [x] 5.4 Add callback route to `cmd/fusionn/main.go`
- [ ] 5.5 Create mock callback client for testing (simulates fusionn-subs response)
- [ ] 5.6 Write Redis client tests and callback handler tests (using mock)

## 6. Integration Testing

- [ ] 6.1 Create integration test for full pipeline
  - [ ] 6.1.1 Test Sonarr webhook → subtitle merge → success
  - [ ] 6.1.2 Test Radarr webhook → subtitle merge → success
  - [ ] 6.1.3 Test missing Chinese subtitle → Redis queue publish
  - [ ] 6.1.4 Test translation callback → subtitle merge
  - [ ] 6.1.5 Test error handling (missing video, ffprobe failure, DuoSubs failure)

## 7. Validation

- [ ] 7.1 Run `make test` and ensure all tests pass
- [ ] 7.2 Run `make lint` and fix any linter errors
- [ ] 7.3 Test with real Sonarr/Radarr instance in development environment
- [ ] 7.4 Verify subtitle merge output quality with sample videos
- [ ] 7.5 Verify Redis queue integration with mocked fusionn-subs callback
- [ ] 7.6 Verify Apprise notifications for success and failure cases

