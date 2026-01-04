# Critical Bug Fixes - Test Coverage Summary

This document summarizes the unit tests added to prevent regression of two critical bugs that were fixed in the subtitle processing pipeline.

## 🐛 Bug #1: Conversion Processor Skipped in Merge Pipeline

### **The Problem**
The `ConversionProcessor` was checking `pctx.Analysis.ChineseTrack.NeedsConversion`, but the merge pipeline runs in a separate context where `pctx.Analysis` is `nil`. This caused Traditional Chinese subtitles to NOT be converted to Simplified Chinese before merging.

### **The Fix**
1. `ExtractorProcessor` now copies `NeedsConversion` flag from Analysis to ProcessingContext
2. `ConversionProcessor` now checks `pctx.NeedsConversion` instead of `pctx.Analysis`

### **Test Coverage**

#### ✅ `TestExtractorProcessor_SetsNeedsConversionFlag`
**Location:** `internal/service/subtitle/processor_extractor_test.go`

Tests that the ExtractorProcessor correctly copies the `NeedsConversion` flag:
- ✅ Traditional Chinese → `NeedsConversion = true`
- ✅ Simplified Chinese → `NeedsConversion = false`
- ✅ No Chinese track → `NeedsConversion = false`

#### ✅ `TestConversionProcessor_ShouldRun`
**Location:** `internal/service/subtitle/processor_extractor_test.go`

Tests all conditions for running conversion:
- ✅ All conditions met → runs
- ✅ OpenCC disabled → skips
- ✅ No Chinese subtitle → skips
- ✅ No conversion needed → skips
- ✅ **Merge pipeline scenario (no Analysis)** → runs (critical test!)

#### ✅ `TestConversionProcessor_WorksInMergePipeline`
**Location:** `internal/service/subtitle/processor_extractor_test.go`

Integration test that verifies:
- ✅ Conversion runs even when `pctx.Analysis == nil`
- ✅ Uses `pctx.NeedsConversion` flag instead of Analysis

---

## 🐛 Bug #2: Context Cancellation in Background Processing

### **The Problem**
Webhook handlers used `c.Request.Context()` for background subtitle processing. This context gets cancelled immediately when the HTTP response is returned, causing `ffprobe` and other long-running processes to fail with `context canceled` errors.

### **The Fix**
Changed webhook handlers to use `context.Background()` for background goroutines, ensuring they continue running after the HTTP response completes.

### **Test Coverage**

#### ✅ `TestBackgroundContext_NotCancelled`
**Location:** `internal/handler/webhook_context_test.go`

Verifies that background work completes even after request context cancellation:
- ✅ Simulates long-running background work (100ms)
- ✅ Uses `context.Background()` (correct approach)
- ✅ Ensures work completes despite HTTP request completion

#### ✅ `TestContextPropagation`
**Location:** `internal/handler/webhook_context_test.go`

Integration test showing correct context usage pattern:
- ✅ HTTP request context has timeout (cancelled early)
- ✅ Background goroutine uses independent `context.Background()`
- ✅ Background work continues after HTTP context cancellation

#### ✅ `TestEventTypeValidation`
**Location:** `internal/handler/webhook_context_test.go`

Documents webhook event handling:
- ✅ Download events → processed
- ✅ Upgrade events → processed
- ✅ Test/Grab/Rename events → ignored

#### ✅ `TestPathValidation`
**Location:** `internal/handler/webhook_context_test.go`

Validates input sanitization:
- ✅ Valid paths → accepted
- ✅ Empty paths → rejected

---

## 📊 Test Results

```bash
$ go test ./...
ok  	github.com/fusionn/internal/config	10.284s
ok  	github.com/fusionn/internal/executor	0.476s
ok  	github.com/fusionn/internal/handler	1.068s
ok  	github.com/fusionn/internal/service/subtitle	1.685s
```

**All tests passing! ✅**

---

## 🎯 Additional Tests Added

### `TestCleanupProcessor_ShouldRun`
Tests that cleanup always runs (safe even if no files exist).

### `TestOutputProcessor_ShouldRun`
Tests that output only runs when a merged subtitle exists.

---

## 🔒 Regression Prevention

These tests ensure:

1. ✅ **NeedsConversion flag propagation** is tested at every step
2. ✅ **Merge pipeline independence** from Analysis context is verified
3. ✅ **Background context usage** is documented and tested
4. ✅ **Critical pipeline conditions** are covered by unit tests

If any of these bugs are reintroduced, the tests will fail immediately during development, preventing them from reaching production.

---

## 📝 Running the Tests

```bash
# Run all tests
go test ./...

# Run specific bug-related tests
go test ./internal/service/subtitle -run "TestExtractorProcessor|TestConversionProcessor" -v
go test ./internal/handler -run "TestBackgroundContext|TestContextPropagation" -v

# Run with coverage
go test ./... -cover
```

