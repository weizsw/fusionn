## Context

Currently, fusionn runs DuoSubs (ML-based subtitle alignment) as a local binary inside Docker containers. On macOS, Docker cannot access Metal GPU, forcing DuoSubs to run CPU-only, resulting in 10-50x slower processing (5+ minutes vs 20 seconds for typical episodes).

**Current State:**
- fusionn executes `duosubs` binary directly via `exec.Command()`
- All processing happens in-container with no GPU acceleration on macOS
- Path handling is simple (all files in shared volume)

**Constraints:**
- Docker on macOS cannot access Metal GPU (Linux VM limitation)
- Existing local mode must remain functional (backwards compatibility)
- Path translation needed between container and host filesystems
- Service should work without authentication (localhost-only)

**Stakeholders:**
- macOS users with Apple Silicon (primary beneficiaries of Metal GPU)
- Linux users (no change, continue using local mode)
- Future: Could extend to remote GPU servers

## Goals / Non-Goals

**Goals:**
- Enable GPU-accelerated DuoSubs on macOS by running natively on host
- Maintain backwards compatibility with local binary execution
- Automatic path translation between container and host paths
- Simple deployment (single Python service, no complex setup)
- Zero-downtime operation (service failures don't break fusionn)

**Non-Goals:**
- Authentication/authorization (service is localhost-only, trusted environment)
- Load balancing or horizontal scaling (single instance sufficient)
- Streaming progress updates via HTTP (use polling/callbacks if needed later)
- Supporting other ML tools beyond DuoSubs
- Running service on separate machines (future enhancement)

## Decisions

### Decision 1: HTTP Service vs SSH

**Chosen: HTTP REST API**

**Rationale:**
- **Security**: HTTP service exposes only DuoSubs merge API vs SSH giving full shell access
- **Simplicity**: Standard REST patterns, no key management or SSH config
- **Debugging**: Easy to test with `curl`, inspect with browser dev tools
- **Portability**: Can move to remote host or cloud GPU later without code changes
- **Docker-friendly**: `host.docker.internal` works out of box

**Alternatives Considered:**
- SSH: Requires key management, opens security hole, complex firewall rules
- Unix sockets: Not accessible from Docker on macOS
- gRPC: Overkill for simple request/response pattern

### Decision 2: FastAPI vs Flask

**Chosen: FastAPI**

**Rationale:**
- **Type safety**: Pydantic models catch invalid requests at API boundary
- **Auto-documentation**: OpenAPI schema for free (useful for testing)
- **Async support**: Better for potential future enhancements (streaming, websockets)
- **Modern**: Better Python 3.10+ support, active community

**Alternatives Considered:**
- Flask: Simpler but lacks built-in validation and async support
- Direct HTTP server: Too low-level for our needs

### Decision 3: Path Translation Strategy

**Chosen: Client-side path translation (fusionn sends container paths, service translates)**

**Rationale:**
- **Single source of truth**: Path mapping configured in one place (fusionn config)
- **Service simplicity**: Service doesn't need to know about container paths
- **Flexibility**: Different containers can use different mount points
- **Testability**: Easy to test path translation logic in Go

**Alternatives Considered:**
- Server-side translation: Would require env var config on service, less flexible
- No translation: Would require manual path conversion, error-prone

### Decision 4: Execution Mode Configuration

**Chosen: Mode selection via `subtitle.duosubs.mode` config field**

**Rationale:**
- **Explicit**: Clear intent (local vs http), easy to understand
- **Gradual migration**: Users can switch modes without code changes
- **Testing**: Can test both modes in different environments
- **Default safe**: Defaults to "local" for backwards compatibility

**Alternatives Considered:**
- Auto-detection: Too magical, hard to debug when wrong mode is chosen
- Compile-time selection: Too inflexible, requires rebuilds

### Decision 5: Error Handling Strategy

**Chosen: Fail-fast with health checks**

**Rationale:**
- **Startup validation**: HTTP mode validates service connectivity at startup
- **Clear errors**: Failed health check prevents service start with clear message
- **Graceful degradation**: Service failures logged but don't crash fusionn
- **Retry logic**: Can be added later at HTTP client level

**Alternatives Considered:**
- Silent fallback to local: Too magical, hides configuration errors
- Retry forever: Blocks processing, no clear failure signal

## Risks / Trade-offs

### Risk 1: Service Availability
**Risk:** If HTTP service crashes, subtitle processing fails  
**Mitigation:**
- Health check at startup warns of misconfiguration
- Clear error messages in logs point to service issue
- Fallback option: Users can switch to local mode via config change
- Future: Add automatic fallback to local mode after N retries

### Risk 2: Path Mapping Errors
**Risk:** Incorrect path translation causes file-not-found errors  
**Mitigation:**
- Validate both files exist before calling service (in HTTP client)
- Service returns detailed error with actual path attempted
- Example configuration in `config.example.yaml` with comments
- Documentation with common pitfall examples

### Risk 3: Performance Overhead
**Risk:** HTTP call adds latency vs direct execution  
**Trade-off:**
- Network latency: ~1-5ms for localhost HTTP (negligible vs 5min processing)
- JSON serialization: ~1ms for small payloads
- **Net benefit:** 10-50x faster processing far outweighs HTTP overhead
- Only affects macOS users (Linux continues using local mode)

### Risk 4: Deployment Complexity
**Risk:** Users must run both fusionn container and Python service  
**Mitigation:**
- Service is optional (local mode still works)
- Simple setup: `pip install -r requirements.txt && python service.py`
- README with step-by-step instructions
- Environment variable configuration (no complex files)
- Future: Docker Compose example or sidecar pattern

### Risk 5: Version Compatibility
**Risk:** DuoSubs CLI changes break service wrapper  
**Mitigation:**
- Service wraps stable `duosubs` Python API (not CLI)
- Pin duosubs version in `requirements.txt`
- Service returns version info in health check endpoint
- Error messages include duosubs version for debugging

## Migration Plan

### Deployment Steps

1. **Create Python service** (new):
   - Add `duosubs-service/` directory with FastAPI wrapper
   - Add `requirements.txt` with pinned versions
   - Add README with setup instructions

2. **Update fusionn codebase**:
   - Add `internal/client/duosubs/http.go` (HTTP client)
   - Modify `internal/executor/duosubs.go` (add mode switching)
   - Update `internal/config/config.go` (add HTTP config fields)
   - Update `internal/service/subtitle/processor_merger.go` (initialize with mode)
   - Update `config/config.example.yaml` (document HTTP mode)

3. **Testing**:
   - Unit tests for HTTP client path translation
   - Integration test: local mode still works
   - Integration test: HTTP mode with service running
   - Test error cases (service down, invalid paths)

4. **Documentation**:
   - Update main README with HTTP mode explanation
   - Add service README with setup guide
   - Add troubleshooting section for common issues

### Rollback Strategy

If HTTP mode causes issues:
1. Set `subtitle.duosubs.mode: "local"` in config (no code changes needed)
2. Restart fusionn (or wait for hot-reload to pick up change)
3. Processing continues with local binary

No data loss risk - all subtitle files are persistent, jobs can retry.

### Gradual Rollout

1. **Phase 1**: Release with default `mode: "local"` (no behavior change)
2. **Phase 2**: Document HTTP mode for early adopters (macOS users)
3. **Phase 3**: After validation, update docs to recommend HTTP mode on macOS

## Open Questions

**Q1:** Should we add authentication to the HTTP service?  
**Answer:** No for v1. Service runs on localhost only (trusted environment). Can add API keys in future if users want to run service remotely.

**Q2:** Should we support multiple concurrent requests?  
**Answer:** Yes, FastAPI handles this automatically with async/await. Each request gets its own subprocess for duosubs execution.

**Q3:** Should we cache models in the service?  
**Answer:** Not needed - DuoSubs already caches models in HuggingFace cache after first run. Subsequent calls reuse loaded models.

**Q4:** What about Windows/Linux users?  
**Answer:** Windows has similar GPU limitations in Docker. Linux users with NVIDIA can use local mode with `--gpus all` Docker flag. HTTP mode is optional for all platforms.

**Q5:** Should service output stream in real-time?  
**Answer:** No for v1. HTTP response returns after completion. Real-time streaming would require WebSocket or SSE, adding complexity. Current approach is simpler and sufficient.
