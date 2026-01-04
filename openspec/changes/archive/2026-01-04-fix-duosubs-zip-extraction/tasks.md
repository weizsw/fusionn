# Tasks: Fix DuoSubs Zip Extraction

**Change ID:** `fix-duosubs-zip-extraction`

## Task List

### 1. Update DuoSubs Executor

- [x] **1.1** Fix `internal/executor/duosubs.go`
  - [x] 1.1.1 Change command to `duosubs merge -p <primary> -s <secondary>`
  - [x] 1.1.2 Add `--output-dir` parameter instead of `-o`
  - [x] 1.1.3 Calculate expected zip filename (basename of primary + `.zip`)
  - [x] 1.1.4 Implement zip extraction logic
  - [x] 1.1.5 Locate `*_combined.ass` file inside extracted content
  - [x] 1.1.6 Return path to combined ASS file
  - [x] 1.1.7 Clean up zip file after extraction
  - [x] 1.1.8 Add error handling for zip extraction failures

### 2. Update Merger Processor

- [x] **2.1** Update `internal/service/subtitle/processor_merger.go`
  - [x] 2.1.1 Remove hardcoded output path assumption
  - [x] 2.1.2 Use returned path from `MergeDuoSubs`
  - [x] 2.1.3 Update error messages

### 3. Docker Integration

- [x] **3.1** Update `Dockerfile`
  - [x] 3.1.1 Add Python 3 runtime to final stage
  - [x] 3.1.2 Install pip and DuoSubs package
  - [x] 3.1.3 Install ffmpeg (for subtitle extraction)
  - [x] 3.1.4 Install opencc (for Traditional→Simplified conversion)
  - [x] 3.1.5 Set HF_HOME environment variable for model cache

- [x] **3.2** Update `docker-compose.yaml`
  - [x] 3.2.1 Add HuggingFace cache volume mount
  - [x] 3.2.2 Document volume purpose in comments
  - [x] 3.2.3 Add example for custom model directory

- [x] **3.3** Update `config/config.example.yaml`
  - [x] 3.3.1 Document that model field can be empty (uses default)
  - [x] 3.3.2 Add Docker volume mount instructions
  - [x] 3.3.3 Document first-run model download behavior

### 4. Testing

- [x] **4.1** Unit tests for DuoSubs executor
  - [x] 4.1.1 Test command construction
  - [x] 4.1.2 Test zip extraction
  - [x] 4.1.3 Test combined file detection
  - [x] 4.1.4 Test error cases (missing zip, corrupt zip, missing combined file)

- [x] **4.2** Integration test for merger processor
  - [x] 4.2.1 Test full merge flow with mock zip
  - [x] 4.2.2 Verify cleanup happens

- [x] **4.3** Docker tests
  - [x] 4.3.1 Test image builds successfully
  - [x] 4.3.2 Verify DuoSubs is installed and runnable
  - [x] 4.3.3 Test volume mount persists models

### 5. Documentation

- [x] **5.1** Update code comments in duosubs.go
- [x] **5.2** Document DuoSubs output format expectations
- [x] **5.3** Add Docker README section for model caching

## Task Dependencies

```
1.1.1 → 1.1.2 → 1.1.3 → 1.1.4 → 1.1.5 → 1.1.6 → 1.1.7 → 1.1.8
2.1.1 → 2.1.2 → 2.1.3 (depends on 1.1.6)
3.1.x (parallel with 1.x, 2.x)
3.2.x (parallel with 3.1.x)
3.3.x (after 3.1.x, 3.2.x)
4.1.x (depends on 1.1.x)
4.2.x (depends on 2.1.x)
4.3.x (depends on 3.1.x)
5.x (parallel with 4.x)
```

## Estimated Effort

- **DuoSubs Executor Update**: 2 hours
- **Merger Processor Update**: 30 minutes
- **Docker Integration**: 2 hours
- **Testing**: 2 hours
- **Documentation**: 1 hour

**Total**: ~7.5 hours

