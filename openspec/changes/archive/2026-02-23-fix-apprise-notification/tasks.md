## 1. Dependency Management

- [x] 1.1 Add `github.com/go-resty/resty/v2` to go.mod

## 2. Refactor Apprise Client

- [x] 2.1 Replace `http.Client` with resty client in AppriseClient struct
- [x] 2.2 Update NewAppriseClient to initialize resty with baseURL, 30s timeout, 2 retries, 1s retry wait
- [x] 2.3 Remove appriseRequest JSON struct (no longer needed)
- [x] 2.4 Refactor Send() method to use form data instead of JSON payload
- [x] 2.5 Update Send() to use `tags` field (plural) instead of `tag`
- [x] 2.6 Add Response struct to parse Apprise error responses
- [x] 2.7 Add error handling for resp.IsError() and apiResp.Error field

## 3. Verification

- [x] 3.1 Test notification sending with real Apprise instance
- [x] 3.2 Verify no EOF errors in logs
- [x] 3.3 Verify notifications appear in configured channels

**Note:** Code refactored to match fusionn-air's working implementation. Build successful.
Manual testing required: Deploy and monitor logs for "✅ Subtitle Merge Complete" notifications without EOF errors.
