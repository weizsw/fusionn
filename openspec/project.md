# Project Context

## Purpose

Intelligent Media Automation Platform - A lightweight, extensible Go application designed to provide HTTP API endpoints for media automation workflows. The platform emphasizes configuration-driven operation, graceful operation, and clean architecture patterns.

## Tech Stack

- **Language**: Go 1.25.5
- **HTTP Framework**: Gin Web Framework (github.com/gin-gonic/gin v1.10.0)
- **Configuration**: Viper with hot-reload support (github.com/spf13/viper v1.19.0)
- **Logging**: Structured logging with Zap (go.uber.org/zap v1.27.0)
- **Containerization**: Docker and Docker Compose
- **Build Tool**: Make with conventional targets (build, test, lint, docker)

## Project Conventions

### Code Style

- **Follow Uber Go Style Guide, Go Code Review Comments, and Effective Go**
- File naming: lowercase with snake_case (e.g., `user_service.go`)
- Test files: `*_test.go` suffix
- Error handling: Always explicit, never ignore errors, wrap with context using `fmt.Errorf` with `%w`
- Logging: Use structured logging with appropriate levels (Debug/Info/Error/Fatal)
- No global variables except where absolutely necessary (prefer dependency injection)
- Constants over magic numbers/strings
- Functional options pattern for complex configuration

### Architecture Patterns

- **Layered Architecture**: `cmd/` (entry point) → `internal/` (business logic) → `pkg/` (reusable libraries)
- **Standard Project Layout**:
  - `cmd/fusionn/` - Application entry point with main.go
  - `internal/` - Private application code (config, version, business logic)
  - `pkg/` - Reusable library code (logger)
  - `config/` - Configuration files (YAML)
- **Dependency Injection**: Pass dependencies explicitly through constructors
- **Context Pattern**: Use context.Context for request-scoped values, cancellation, timeouts
- **Graceful Shutdown**: 30-second timeout for clean HTTP server shutdown
- **Middleware Pattern**: Request logging, recovery, and cross-cutting concerns
- **Hot-reload Configuration**: Configuration changes detected and applied without restart

### Testing Strategy

- **Unit Tests**: Focus on individual functions/packages in isolation
- **Table-Driven Tests**: Preferred for testing multiple input/output scenarios
- **Test Coverage**: Run with `make test-cover` to generate HTML coverage reports
- **Race Detection**: Always run tests with `-race` flag
- **Benchmarking**: Use Go's built-in benchmarking for performance-critical code
- **Test Organization**: Tests in same package as code being tested (e.g., `config_test.go` next to `config.go`)

### Git Workflow

- **Version Management**: Semantic versioning (SemVer) with Git tags
- **Build Versioning**: `VERSION=$(git describe --tags --always --dirty)` embedded at build time
- **Makefile Targets**: Standardized targets for build, test, lint, docker operations
- **Linting**: Use golangci-lint for code quality checks

## Domain Context

- **Media Automation**: Platform designed to orchestrate media processing workflows
- **HTTP API First**: RESTful API design with JSON responses
- **Configuration-Driven**: Behavior controlled through YAML configuration, overridable via environment variables
- **Production-Ready**: Includes health checks, version endpoints, structured logging, graceful shutdown
- **Development vs Production**: Environment-aware behavior (development mode has verbose logging, production uses release mode)

## Important Constraints

- **Go Version**: Requires Go 1.23 or later
- **Configuration**: Must provide `config/config.yaml` or override with `CONFIG_PATH` environment variable
- **Environment Variables**: Use `FUSIONN_*` prefix for configuration overrides (e.g., `FUSIONN_SERVER_PORT=9090`)
- **HTTP Timeouts**: Read timeout: 10s, Write timeout: 30s, Idle timeout: 60s
- **Clean Shutdown**: Maximum 30-second graceful shutdown period
- **Logger Sync**: Must call `logger.Sync()` before application exit to flush buffered logs

## External Dependencies

- **Configuration Management**: Viper (supports YAML, environment variables, hot-reload)
- **HTTP Server**: Gin (fast HTTP router with middleware support)
- **Logging**: Uber Zap (high-performance structured logging)
- **Container Runtime**: Docker for production deployments
- **Development Tools**: golangci-lint for linting, go test for testing
