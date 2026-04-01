# SDH Subtitle Filter Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Filter SDH annotations (brackets, parentheses, speaker labels, caps descriptions) from English SRT subtitles using `cleanit` CLI, preserving lyrics.

**Architecture:** New `SDHFilterProcessor` in the analyze pipeline between Extractor and TranslationQueue. Shells out to `cleanit -t no-sdh` via a new executor. Only runs when the selected English track is identified as SDH.

**Tech Stack:** Go 1.23, `cleanit` (Python CLI via `pip install cleanit`)

**Spec:** `docs/superpowers/specs/2026-04-01-sdh-filter-design.md`

---

### Task 1: Add `IsSDH` field to `Track` struct

**Files:**
- Modify: `internal/service/subtitle/analyzer.go:26-34` (Track struct)
- Modify: `internal/service/subtitle/analyzer.go:209-216` (detectEnglishSubtitle return)
- Test: `internal/service/subtitle/analyzer_test.go`

**Step 1: Write the failing test**

Add a test that asserts `IsSDH` is set on the returned track when the winning candidate is SDH.

```go
func TestDetectEnglishSubtitle_SetsIsSDH(t *testing.T) {
	analyzer := NewAnalyzer([]string{"eng"}, nil, nil, nil)

	tests := []struct {
		name      string
		streams   []executor.StreamInfo
		wantIsSDH bool
	}{
		{
			name: "SDH by disposition",
			streams: []executor.StreamInfo{
				{Index: 2, CodecType: "subtitle", CodecName: "subrip",
					Tags:        map[string]string{"language": "eng"},
					Disposition: map[string]int{"hearing_impaired": 1}},
			},
			wantIsSDH: true,
		},
		{
			name: "SDH by title",
			streams: []executor.StreamInfo{
				{Index: 2, CodecType: "subtitle", CodecName: "subrip",
					Tags: map[string]string{"language": "eng", "title": "English SDH"}},
			},
			wantIsSDH: true,
		},
		{
			name: "Non-SDH track",
			streams: []executor.StreamInfo{
				{Index: 2, CodecType: "subtitle", CodecName: "subrip",
					Tags: map[string]string{"language": "eng", "title": "English"}},
			},
			wantIsSDH: false,
		},
		{
			name: "SDH exists but non-SDH wins",
			streams: []executor.StreamInfo{
				{Index: 2, CodecType: "subtitle", CodecName: "subrip",
					Tags: map[string]string{"language": "eng", "title": "English"}},
				{Index: 3, CodecType: "subtitle", CodecName: "subrip",
					Tags:        map[string]string{"language": "eng", "title": "English SDH"},
					Disposition: map[string]int{"hearing_impaired": 1}},
			},
			wantIsSDH: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			track := analyzer.detectEnglishSubtitle(tt.streams)
			if track == nil {
				t.Fatal("expected a track, got nil")
			}
			if track.IsSDH != tt.wantIsSDH {
				t.Errorf("IsSDH = %v, want %v", track.IsSDH, tt.wantIsSDH)
			}
		})
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/service/subtitle/ -run TestDetectEnglishSubtitle_SetsIsSDH -v`
Expected: FAIL — `track.IsSDH undefined (type *Track has no field or method IsSDH)`

**Step 3: Add `IsSDH` field to Track and set it in detectEnglishSubtitle**

In `analyzer.go`, add the field to `Track`:

```go
type Track struct {
	Index           int
	Language        string
	Title           string
	CodecName       string
	Priority        int
	NeedsConversion bool
	ExtractedPath   string
	IsSDH           bool
}
```

In `detectEnglishSubtitle`, where the `Track` is returned (around line 209), add the `IsSDH` field:

```go
	best := &candidates[bestIdx]
	return &Track{
		Index:     best.stream.Index,
		Language:  best.lang,
		Title:     best.title,
		CodecName: best.stream.CodecName,
		Priority:  bestScore.Total,
		IsSDH:     isHearingImpaired(best.stream) || isSDHByTitle(best.title),
	}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/service/subtitle/ -run TestDetectEnglishSubtitle_SetsIsSDH -v`
Expected: PASS

**Step 5: Run all existing tests to verify no regressions**

Run: `go test ./internal/service/subtitle/ -v`
Expected: All tests PASS

**Step 6: Commit**

```bash
git add internal/service/subtitle/analyzer.go internal/service/subtitle/analyzer_test.go
git commit -m "feat: add IsSDH field to Track for SDH subtitle detection"
```

---

### Task 2: Create `cleanit` executor

**Files:**
- Create: `internal/executor/cleanit.go`
- Test: `internal/executor/cleanit_test.go`

**Step 1: Write the failing test**

```go
package executor

import (
	"testing"
)

func TestIsCleanitAvailable(t *testing.T) {
	result := IsCleanitAvailable()
	t.Logf("cleanit available: %v", result)
}

func TestRunCleanit_FileNotFound(t *testing.T) {
	err := RunCleanit(t.Context(), "/nonexistent/file.srt")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/executor/ -run TestIsCleanitAvailable -v`
Expected: FAIL — `undefined: IsCleanitAvailable`

**Step 3: Write the executor**

Create `internal/executor/cleanit.go`:

```go
package executor

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/fusionn/pkg/logger"
)

// RunCleanit runs cleanit with the no-sdh tag on an SRT file (modifies in-place).
func RunCleanit(ctx context.Context, srtPath string) error {
	cmd := exec.CommandContext(ctx,
		"cleanit",
		"-t", "no-sdh",
		srtPath,
	)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	logger.Debugf("Executing: %s", cmd.String())

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("cleanit failed: %w (stderr: %s)", err, stderr.String())
	}

	return nil
}

// IsCleanitAvailable checks if cleanit binary exists in PATH.
func IsCleanitAvailable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "cleanit", "--version").Run() == nil
}
```

**Step 4: Run tests**

Run: `go test ./internal/executor/ -run "TestIsCleanitAvailable|TestRunCleanit" -v`
Expected: PASS (availability depends on environment; error test should pass)

**Step 5: Commit**

```bash
git add internal/executor/cleanit.go internal/executor/cleanit_test.go
git commit -m "feat: add cleanit executor for SDH subtitle filtering"
```

---

### Task 3: Create `SDHFilterProcessor`

**Files:**
- Create: `internal/service/subtitle/processor_sdh_filter.go`
- Create: `internal/service/subtitle/processor_sdh_filter_test.go`

**Step 1: Write the failing tests**

```go
package subtitle

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/fusionn/internal/executor"
)

func TestSDHFilterProcessor_Name(t *testing.T) {
	p := NewSDHFilterProcessor()
	if p.Name() != "SDHFilter" {
		t.Errorf("Name() = %q, want %q", p.Name(), "SDHFilter")
	}
}

func TestSDHFilterProcessor_ShouldRun(t *testing.T) {
	tests := []struct {
		name string
		pctx *ProcessingContext
		want bool
	}{
		{
			name: "SDH English track present",
			pctx: &ProcessingContext{
				EnglishSubPath: "/tmp/eng.srt",
				Analysis: &AnalysisResult{
					EnglishTrack: &Track{IsSDH: true, ExtractedPath: "/tmp/eng.srt"},
				},
			},
			want: true,
		},
		{
			name: "Non-SDH English track",
			pctx: &ProcessingContext{
				EnglishSubPath: "/tmp/eng.srt",
				Analysis: &AnalysisResult{
					EnglishTrack: &Track{IsSDH: false, ExtractedPath: "/tmp/eng.srt"},
				},
			},
			want: false,
		},
		{
			name: "No English track",
			pctx: &ProcessingContext{
				Analysis: &AnalysisResult{},
			},
			want: false,
		},
		{
			name: "No analysis",
			pctx: &ProcessingContext{},
			want: false,
		},
		{
			name: "No English sub path",
			pctx: &ProcessingContext{
				EnglishSubPath: "",
				Analysis: &AnalysisResult{
					EnglishTrack: &Track{IsSDH: true},
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewSDHFilterProcessor()
			got := p.ShouldRun(tt.pctx)
			if got != tt.want {
				t.Errorf("ShouldRun() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSDHFilterProcessor_Process_CleanitNotAvailable(t *testing.T) {
	if executor.IsCleanitAvailable() {
		t.Skip("cleanit is available, skipping unavailable test")
	}

	tmpDir := t.TempDir()
	srtPath := filepath.Join(tmpDir, "test.srt")
	content := "1\n00:00:01,000 --> 00:00:02,000\n[music playing] Hello\n"
	os.WriteFile(srtPath, []byte(content), 0644)

	p := NewSDHFilterProcessor()
	pctx := &ProcessingContext{
		EnglishSubPath: srtPath,
		Analysis: &AnalysisResult{
			EnglishTrack: &Track{IsSDH: true},
		},
	}

	err := p.Process(context.Background(), pctx)
	if err != nil {
		t.Errorf("Process() should return nil on cleanit failure, got: %v", err)
	}

	// Original file should be untouched
	data, _ := os.ReadFile(srtPath)
	if string(data) != content {
		t.Error("Original file was modified despite cleanit failure")
	}
}

func TestSDHFilterProcessor_Process_WithCleanit(t *testing.T) {
	if !executor.IsCleanitAvailable() {
		t.Skip("cleanit not available")
	}

	tmpDir := t.TempDir()
	srtPath := filepath.Join(tmpDir, "test.srt")
	content := "1\n00:00:01,000 --> 00:00:02,000\n[music playing]\n\n2\n00:00:03,000 --> 00:00:04,000\nHello world\n\n"
	os.WriteFile(srtPath, []byte(content), 0644)

	p := NewSDHFilterProcessor()
	pctx := &ProcessingContext{
		EnglishSubPath: srtPath,
		Analysis: &AnalysisResult{
			EnglishTrack: &Track{IsSDH: true},
		},
	}

	err := p.Process(context.Background(), pctx)
	if err != nil {
		t.Fatalf("Process() error: %v", err)
	}

	data, _ := os.ReadFile(srtPath)
	cleaned := string(data)
	if contains(cleaned, "[music playing]") {
		t.Errorf("SDH content was not removed: %s", cleaned)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/service/subtitle/ -run TestSDHFilterProcessor -v`
Expected: FAIL — `undefined: NewSDHFilterProcessor`

**Step 3: Write the processor**

Create `internal/service/subtitle/processor_sdh_filter.go`:

```go
package subtitle

import (
	"context"
	"fmt"
	"os"

	"github.com/fusionn/internal/executor"
	"github.com/fusionn/pkg/logger"
)

// SDHFilterProcessor removes SDH annotations from English SRT subtitles using cleanit.
type SDHFilterProcessor struct{}

// NewSDHFilterProcessor creates a new SDH filter processor.
func NewSDHFilterProcessor() *SDHFilterProcessor {
	return &SDHFilterProcessor{}
}

// Name returns the processor name.
func (p *SDHFilterProcessor) Name() string {
	return "SDHFilter"
}

// ShouldRun returns true when the selected English track is SDH and has been extracted.
func (p *SDHFilterProcessor) ShouldRun(pctx *ProcessingContext) bool {
	if pctx.Analysis == nil || pctx.Analysis.EnglishTrack == nil {
		return false
	}
	return pctx.Analysis.EnglishTrack.IsSDH && pctx.EnglishSubPath != ""
}

// Process runs cleanit on the English SRT to strip SDH content.
// On failure, logs a warning and returns nil so the pipeline continues with the unfiltered SRT.
func (p *SDHFilterProcessor) Process(ctx context.Context, pctx *ProcessingContext) error {
	log := logger.Indent()

	if !executor.IsCleanitAvailable() {
		log.Warn("cleanit not found in PATH - skipping SDH filter")
		return nil
	}

	log.Infof("Filtering SDH content from: %s", pctx.EnglishSubPath)

	// Copy to temp file so original is untouched on failure
	tempPath := pctx.EnglishSubPath + ".sdh_tmp"
	data, err := os.ReadFile(pctx.EnglishSubPath)
	if err != nil {
		log.Warnf("Failed to read SRT for SDH filtering: %v", err)
		return nil
	}
	if err := os.WriteFile(tempPath, data, 0644); err != nil {
		log.Warnf("Failed to create temp file for SDH filtering: %v", err)
		return nil
	}
	defer os.Remove(tempPath)

	// Run cleanit on the temp copy
	if err := executor.RunCleanit(ctx, tempPath); err != nil {
		log.Warnf("cleanit failed: %v - continuing with unfiltered SRT", err)
		return nil
	}

	// Atomic replace: rename temp over original
	if err := os.Rename(tempPath, pctx.EnglishSubPath); err != nil {
		log.Warnf("Failed to replace SRT with filtered version: %v", err)
		return nil
	}

	log.Info("✅ SDH content filtered successfully")
	return nil
}

// IsCleanitAvailable is a package-level check for startup logging.
func IsCleanitAvailable() bool {
	return executor.IsCleanitAvailable()
}
```

**Step 4: Run tests**

Run: `go test ./internal/service/subtitle/ -run TestSDHFilterProcessor -v`
Expected: PASS (integration test skipped if cleanit not installed)

**Step 5: Commit**

```bash
git add internal/service/subtitle/processor_sdh_filter.go internal/service/subtitle/processor_sdh_filter_test.go
git commit -m "feat: add SDHFilterProcessor to clean SDH subtitles via cleanit"
```

---

### Task 4: Wire processor into pipeline and add emoji

**Files:**
- Modify: `internal/service/subtitle/service.go:44-48` (analyze pipeline)
- Modify: `internal/service/subtitle/pipeline.go:52-63` (emoji map)

**Step 1: Add emoji entry to `processorEmojis` in `pipeline.go`**

Add to the map in `pipeline.go`:

```go
"SDHFilter":     "🧹",
```

(Pick a distinct emoji — `🧹` works since it represents "cleaning". If it conflicts with Cleanup's `🧹`, use `🔇` instead.)

**Step 2: Insert SDHFilterProcessor into analyze pipeline in `service.go`**

After the `NewExtractorProcessor(analyzer)` line and before `NewTranslationQueueProcessor`:

```go
	analyzePipeline.AddProcessor(NewAnalyzerProcessor(analyzer))
	analyzePipeline.AddProcessor(NewExtractorProcessor(analyzer))
	analyzePipeline.AddProcessor(NewSDHFilterProcessor())
	analyzePipeline.AddProcessor(NewTranslationQueueProcessor(redisClient, ""))
```

**Step 3: Add startup check for cleanit availability**

After the `fusionn-font` availability check block in `service.go` (around line 68-72), add:

```go
	if !IsCleanitAvailable() {
		logger.Warn("⚠️ cleanit not found in PATH - SDH subtitle filtering disabled")
	}
```

**Step 4: Run all tests**

Run: `go test ./internal/service/subtitle/ -v`
Expected: All tests PASS

Run: `go test ./... -v`
Expected: All tests PASS

**Step 5: Commit**

```bash
git add internal/service/subtitle/service.go internal/service/subtitle/pipeline.go
git commit -m "feat: wire SDHFilterProcessor into analyze pipeline"
```

---

### Task 5: Verify end-to-end (manual)

**Step 1: Install cleanit if not already installed**

```bash
pip install cleanit
which cleanit
```

**Step 2: Run the full test suite**

```bash
go test ./... -v
```

**Step 3: Test with a sample SDH SRT file**

Create a test SRT file with SDH content and verify `cleanit -t no-sdh` removes the expected content while preserving lyrics:

```bash
cat > /tmp/test_sdh.srt << 'EOF'
1
00:00:01,000 --> 00:00:03,000
[dramatic music playing]

2
00:00:04,000 --> 00:00:06,000
(door slams)

3
00:00:07,000 --> 00:00:09,000
JOHN: Hey, what's going on?

4
00:00:10,000 --> 00:00:12,000
♪ We are the champions ♪

5
00:00:13,000 --> 00:00:15,000
I don't know what happened.
EOF

cleanit -t no-sdh /tmp/test_sdh.srt
cat /tmp/test_sdh.srt
```

Expected: entries 1 and 2 removed entirely, entry 3 has `JOHN:` stripped, entry 4 (lyrics) preserved, entry 5 unchanged.

**Step 4: Build and verify no compilation errors**

```bash
go build ./...
```
