## Context

Current apprise client in fusionn uses JSON payloads with `application/json` content-type, resulting in EOF errors. fusionn-air has a working implementation using form-encoded data with the `resty` HTTP client library, including retry logic and proper timeout handling.

Apprise API expects form data, not JSON. The EOF error indicates the server closes the connection when receiving unexpected content-type.

## Goals / Non-Goals

**Goals:**
- Fix EOF errors by switching from JSON to form-encoded data
- Add retry logic to handle transient network failures
- Match fusionn-air's proven implementation pattern
- Improve timeout and error handling

**Non-Goals:**
- Changing notification message formats or content
- Adding new notification types beyond existing (success, info, warning, failure)
- Modifying when/where notifications are triggered

## Decisions

### Decision 1: Use resty instead of standard http.Client

**Rationale:** fusionn-air uses `github.com/go-resty/resty/v2` which provides:
- Built-in retry logic with configurable backoff
- Cleaner form data API
- Response type binding
- Consistent error handling

**Alternatives Considered:**
- Keep `http.Client` and manually add retry: More code, harder to maintain
- Use different HTTP library: resty is already proven in fusionn-air

### Decision 2: Form Data over JSON

**Rationale:** Apprise API expects `application/x-www-form-urlencoded` data. This is why fusionn-air works - it uses `SetFormData()`.

**Changes:**
- Remove `json.Marshal()` and JSON payload struct
- Use `SetFormData()` with map of string key-values
- Field name: use `tags` (plural) not `tag` per Apprise API spec

### Decision 3: Retry Configuration

**Settings:**
- Timeout: 30 seconds (increased from 10s to match fusionn-air)
- Retry count: 2 attempts (fusionn-air uses this)
- Retry wait: 1 second between attempts

**Rationale:** Network requests to containerized services can experience transient failures. Two retries with 1s wait gives enough opportunity without excessive delay.

### Decision 4: Enhanced Error Handling

**Pattern:** Apprise returns HTTP 200 with error details in JSON body when misconfigured or service unavailable.

**Changes:**
- Parse response JSON for `error` field
- Return descriptive error messages including status codes
- Check both `resp.IsError()` and parsed error field

## Risks / Trade-offs

**Risk:** Adding resty dependency increases binary size  
→ **Mitigation:** resty is lightweight and already used in fusionn-air's proven pattern

**Risk:** Form data might not support complex message formatting  
→ **Mitigation:** Current notification messages are simple text - no special formatting needed

**Trade-off:** Retry logic adds latency on repeated failures  
→ **Acceptable:** 2 retries with 1s wait = max 2s additional delay on total failure, which is reasonable for non-critical notifications

## Migration Plan

**Deployment:**
1. Add `github.com/go-resty/resty/v2` to `go.mod`
2. Refactor `internal/notification/apprise.go` to use form data and resty
3. No config changes needed - existing apprise config fields remain the same
4. Rebuild and restart service

**Rollback:**
- Revert single file change if issues arise
- No database or state changes involved

**Testing:**
- Unit test with mock HTTP server
- Integration test with real Apprise instance to verify notifications send successfully
