## 1. fusionn Queue Message Updates

- [x] 1.1 Update `TranslationJob` struct in `internal/queue/redis_client.go` to add `SubtitlePath` field and remove `CallbackURL`
- [x] 1.2 Update `TranslationQueueProcessor` in `internal/service/subtitle/processor_translation_queue.go` to populate `SubtitlePath` from `pctx.EnglishSubPath`
- [x] 1.3 Remove callback URL generation code from `internal/service/subtitle/service.go` line 46

## 2. fusionn Subtitle Extraction Path Changes

- [x] 2.1 Update `ExtractSubtitles()` in `internal/service/subtitle/analyzer.go` to extract to media directory instead of `/tmp`
- [x] 2.2 Implement path generation: `<video-dir>/<video-name>.eng.srt` using `filepath.Dir()` and `filepath.Base()`
- [x] 2.3 Add config field `subtitle.extracted_subtitle_suffix` (default: "eng") to `internal/config/config.go`
- [x] 2.4 Update extraction to use configurable suffix from config
- [x] 2.5 Update `config/config.example.yaml` with new `extracted_subtitle_suffix` field

## 3. fusionn Callback Handler

- [x] 3.1 Create `internal/handler/callback.go` with `CallbackHandler` struct
- [x] 3.2 Implement `HandleTranslationCallback()` method to accept POST at `/api/v1/callback/translation`
- [x] 3.3 Define `TranslationCallbackPayload` struct with fields: `JobID`, `VideoPath`, `EngSubtitlePath`, `ChsSubtitlePath`
- [x] 3.4 Add payload validation (required fields, file existence checks)
- [x] 3.5 Call `subtitleService.ProcessWithSubtitles()` to enqueue merge job
- [x] 3.6 Return HTTP 202 Accepted on success, 400/503 on errors
- [x] 3.7 Add duplicate callback handling (check if job already in merge queue)
- [x] 3.8 Register callback route in `cmd/fusionn/main.go` router setup

## 4. fusionn Redis Configuration

- [x] 4.1 Add Redis config section to `internal/config/config.go` with fields: `Enabled`, `Host`, `Port`, `Database`, `Password`, `QueueKey`
- [x] 4.2 Update `config/config.example.yaml` with Redis configuration section (default queue_key: `fusionn:translation_queue`)
- [x] 4.3 Update Redis client initialization in `cmd/fusionn/main.go` to conditionally create client if enabled

## 5. fusionn-subs Job Message Updates

- [x] 5.1 Update `JobMessage` struct in `internal/types/job.go` to add `JobID`, `MediaTitle`, `MediaType` fields
- [x] 5.2 Rename `Path` field to `SubtitlePath` in `JobMessage`
- [x] 5.3 Remove `FileName`, `Provider`, `Overview` fields from `JobMessage`
- [x] 5.4 Update `Validate()` method to check new required fields
- [x] 5.5 Update `OutputPath()` method to work with `SubtitlePath` instead of `Path`

## 6. fusionn-subs Callback Payload Updates

- [x] 6.1 Update `Payload` struct in `internal/client/callback/client.go` to add `JobID` field
- [x] 6.2 Ensure payload includes `VideoPath`, `EngSubtitlePath`, `ChsSubtitlePath`
- [x] 6.3 Update `Send()` method to populate new payload structure

## 7. fusionn-subs Callback Retry Logic

- [x] 7.1 Add retry config to `internal/config/config.go`: `MaxRetries` and `RetryBackoffSeconds` under callback section
- [x] 7.2 Implement exponential backoff retry in `internal/client/callback/client.go` `Send()` method
- [x] 7.3 Use retry backoff: [1, 2, 4, 8, 16] seconds (configurable)
- [x] 7.4 Log each retry attempt and final failure
- [x] 7.5 Update `config/config.example.yaml` with callback retry configuration

## 8. fusionn-subs Translation Retry Logic

- [x] 8.1 Add `MaxTranslationRetries` config field to translator section in `internal/config/config.go`
- [x] 8.2 Wrap `Translate()` calls in retry loop (max 3 attempts by default)
- [x] 8.3 Log each translation attempt and failures
- [x] 8.4 Update `config/config.example.yaml` with translation retry configuration

## 9. fusionn-subs Worker Updates

- [x] 9.1 Update `processJob()` in `internal/service/worker/worker.go` to use new `JobMessage` fields
- [x] 9.2 Update logging to include `JobID` and `MediaTitle`
- [x] 9.3 Ensure callback sends `JobID` from job message

## 10. Configuration Documentation

- [x] 10.1 Update fusionn `config/config.example.yaml` with all new Redis and subtitle fields
- [x] 10.2 Update fusionn-subs `config/config.example.yaml` with callback and translation retry fields
- [x] 10.3 Update fusionn README.md with Redis configuration section
- [x] 10.4 Update fusionn-subs README.md with updated job message format

## 11. Docker Compose Setup

- [x] 11.1 Create example `docker-compose.yml` in fusionn root with services: redis, fusionn, fusionn-subs
- [x] 11.2 Configure shared media volume mount for both fusionn and fusionn-subs
- [x] 11.3 Configure Redis connection environment variables for both services
- [x] 11.4 Configure fusionn-subs callback URL to point to fusionn service
- [x] 11.5 Add service dependencies (fusionn-subs depends on redis and fusionn)

## 12. Testing & Validation

- [x] 12.1 Test full flow: webhook → extract → queue → translate → callback → merge
- [x] 12.2 Verify subtitle files are created in media directory with correct naming
- [x] 12.3 Test Redis connection failure handling
- [x] 12.4 Test callback retry logic (bring fusionn down during translation)
- [x] 12.5 Test translation retry logic (simulate AI provider failure)
- [x] 12.6 Verify cleanup processor removes intermediate subtitle files
- [x] 12.7 Test duplicate callback handling
