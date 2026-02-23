## Why

Apprise notifications are failing with "Post http://apprise:8000/notify/apprise: EOF" errors. The current implementation sends JSON payloads with `application/json` content-type, but Apprise API expects form-encoded data. fusionn-air successfully uses form data and includes retry logic with resty client.

## What Changes

- Switch from JSON payload to form-encoded data in apprise client
- Replace standard `http.Client` with `resty` library for built-in retry logic
- Add `tags` field (plural) instead of `tag` for proper Apprise API compatibility
- Increase timeout from 10s to 30s and add retry mechanism (2 retries with 1s wait)
- Improve error handling to parse Apprise error responses (returns 200 with error in JSON body)

## Capabilities

### New Capabilities
<!-- No new capabilities being introduced -->

### Modified Capabilities
<!-- The notification capability is an implementation detail, not a spec-level capability -->

## Impact

**Affected Code:**
- `internal/notification/apprise.go` - complete refactor to use form data and resty client

**Dependencies:**
- Add `github.com/go-resty/resty/v2` dependency

**Systems:**
- Apprise notification service integration will become reliable and match fusionn-air's working implementation
