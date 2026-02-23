## 1. Python HTTP Service

- [x] 1.1 Create `duosubs-service/` directory structure
- [x] 1.2 Create `service.py` with FastAPI application and endpoints (`/health`, `/merge`)
- [x] 1.3 Implement `MergeRequest` and `MergeResponse` Pydantic models with validation
- [x] 1.4 Implement `translate_path()` function for container-to-host path conversion
- [x] 1.5 Implement merge endpoint handler with duosubs execution and error handling
- [x] 1.6 Add timeout handling (10 minute default) for duosubs subprocess
- [x] 1.7 Create `requirements.txt` with pinned versions (fastapi, uvicorn, pydantic, duosubs)
- [x] 1.8 Create `README.md` with setup instructions and usage examples
- [x] 1.9 Test service locally: health check, successful merge, error cases

## 2. Go HTTP Client

- [x] 2.1 Create `internal/client/duosubs/` package directory
- [x] 2.2 Create `http.go` with `HTTPClient` struct and configuration
- [x] 2.3 Implement `NewHTTPClient()` constructor with timeout configuration
- [x] 2.4 Implement `Merge()` method with JSON request/response handling
- [x] 2.5 Implement `HealthCheck()` method for service availability testing
- [x] 2.6 Add error handling for connection failures, timeouts, and HTTP errors
- [x] 2.7 Test HTTP client with mock server: successful merge, service down, timeout

## 3. DuoSubs Executor Refactoring

- [x] 3.1 Update `internal/executor/duosubs.go` to add `DuoSubsMode` type and mode constants
- [x] 3.2 Add HTTP configuration fields to `DuoSubsConfig` struct
- [x] 3.3 Create `NewDuoSubsExecutor()` function to initialize executor with mode selection
- [x] 3.4 Implement health check logic in executor initialization for HTTP mode
- [x] 3.5 Refactor `MergeDuoSubs()` to route to `mergeDuoSubsLocal()` or `mergeDuoSubsHTTP()`
- [x] 3.6 Extract existing logic into `mergeDuoSubsLocal()` function
- [x] 3.7 Implement `mergeDuoSubsHTTP()` function that calls HTTP client
- [x] 3.8 Test mode routing: local mode still works, HTTP mode calls service

## 4. Configuration Updates

- [x] 4.1 Update `internal/config/config.go` to add mode and HTTP fields to `DuoSubsConfig`
- [x] 4.2 Add validation for HTTP mode configuration (URL, prefixes required)
- [x] 4.3 Update `config/config.example.yaml` with mode selection and HTTP configuration
- [x] 4.4 Add detailed comments explaining local vs HTTP mode use cases
- [x] 4.5 Document path prefix mapping with examples
- [x] 4.6 Test configuration validation: valid local, valid HTTP, invalid HTTP (missing fields)

## 5. Service Integration

- [x] 5.1 Update `internal/service/subtitle/processor_merger.go` to call `NewMergerProcessor()` with error handling
- [x] 5.2 Modify `NewMergerProcessor()` to return error from `NewDuoSubsExecutor()`
- [x] 5.3 Update `Process()` method to use initialized executor config
- [x] 5.4 Update `internal/service/subtitle/service.go` to handle error from `NewMergerProcessor()`
- [x] 5.5 Update `cmd/fusionn/main.go` to handle error from `NewService()`
- [x] 5.6 Test service initialization: local mode succeeds, HTTP mode with bad config fails cleanly

## 6. Build & Compilation

- [x] 6.1 Run `go build ./...` to verify compilation
- [x] 6.2 Fix any import errors or type mismatches
- [x] 6.3 Run `go mod tidy` to update dependencies
- [x] 6.4 Verify no lint errors with `golangci-lint run` (if available)

## 7. Integration Testing

- [x] 7.1 Test local mode: Run fusionn with default config, verify subtitle merge works
- [x] 7.2 Test HTTP mode setup: Start duosubs-service on host, configure fusionn for HTTP
- [x] 7.3 Test successful HTTP merge: Send webhook, verify subtitle processed via HTTP
- [x] 7.4 Test HTTP service unreachable: Stop service, verify clear error message
- [x] 7.5 Test path translation: Verify files are found with correct host paths
- [x] 7.6 Test timeout handling: Set low timeout, verify operation is cancelled
- [x] 7.7 Test mode switching: Change config from local to HTTP, verify hot-reload works

## 8. Documentation

- [x] 8.1 Update main `README.md` with DuoSubs HTTP mode section
- [x] 8.2 Add deployment guide for macOS users (HTTP mode recommended)
- [x] 8.3 Add troubleshooting section for common path mapping issues
- [x] 8.4 Document performance comparison (local vs HTTP on macOS)
- [x] 8.5 Add Docker Compose example with both fusionn and duosubs-service (optional)
- [x] 8.6 Document environment variables for duosubs-service configuration

## 9. Cleanup & Polish

- [x] 9.1 Review all error messages for clarity and actionability
- [x] 9.2 Ensure log messages use appropriate levels (Info, Warn, Error)
- [x] 9.3 Verify streaming output from DuoSubs still works in local mode
- [x] 9.4 Check for any TODOs or debug code left in implementation
- [x] 9.5 Verify backwards compatibility: existing configs work without changes
