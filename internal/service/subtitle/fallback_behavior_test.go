package subtitle

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fusionn/internal/executor"
	"github.com/fusionn/internal/subtitlellm"
)

type parsedDialogueEvent struct {
	start string
	end   string
	style string
	text  string
}

func parseASSEvents(t *testing.T, assContent string) []parsedDialogueEvent {
	t.Helper()
	var events []parsedDialogueEvent
	inEvents := false
	lines := strings.Split(assContent, "\n")
	for _, rawLine := range lines {
		line := strings.TrimRight(rawLine, "\r")
		if strings.HasPrefix(line, "[Events]") {
			inEvents = true
			continue
		}
		if inEvents && strings.HasPrefix(line, "Dialogue:") {
			parts := strings.SplitN(line, ",", 10)
			if len(parts) < 10 {
				t.Fatalf("malformed dialogue line: %s", line)
			}
			events = append(events, parsedDialogueEvent{
				start: parts[1],
				end:   parts[2],
				style: parts[3],
				text:  parts[9],
			})
		}
	}
	return events
}

// TestFallbackBehavior_LLM503FallbackToNativeASS tests the public behavior when
// upstream LLM fails with HTTP 503 Service Unavailable.
// Expected behavior:
// 1. Process() succeeds (does not abort the whole episode merge).
// 2. A readable bilingual ASS file is generated at pctx.MergedSubPath.
// 3. All source cues (both EN and ZH), chronological order and original timing are intact.
// 4. Audit evidence is persisted and traceable (recording FailedWindows and degradation reason).
func TestFallbackBehavior_LLM503FallbackToNativeASS(t *testing.T) {
	videoPath, enPath, zhPath := mergerFixture(t)

	// Upstream LLM returns HTTP 503 for all window requests
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream model service overloaded (503)", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	llm := llmMergerTestConfig(t, pairingHandler(t))
	llm.BaseURL = server.URL + "/v1"
	llm.CacheDir = t.TempDir()
	llm.Retries = 0

	processor := newLLMTestProcessor(t, llm)
	processor.probe = func(context.Context, string) (*executor.FFProbeOutput, error) {
		return nil, errors.New("no probe")
	}

	pctx := &ProcessingContext{
		VideoPath:      videoPath,
		EnglishSubPath: enPath,
		ChineseSubPath: zhPath,
		Metadata:       map[string]interface{}{},
	}

	err := processor.Process(context.Background(), pctx)
	if err != nil {
		t.Fatalf("Process() returned fatal error on upstream 503: %v", err)
	}

	// MergedSubPath must be generated and readable
	if pctx.MergedSubPath == "" {
		t.Fatal("pctx.MergedSubPath is empty after 503 fallback")
	}
	content, readErr := os.ReadFile(pctx.MergedSubPath)
	if readErr != nil {
		t.Fatalf("read merged sub: %v", readErr)
	}
	assText := string(content)

	// Verify valid ASS headers
	if !strings.Contains(assText, "[Script Info]") || !strings.Contains(assText, "[Events]") {
		t.Fatalf("merged sub is not valid ASS:\n%s", assText)
	}
	if !strings.Contains(assText, "Collisions: Normal") {
		t.Error("expected 'Collisions: Normal' header in fallback ASS")
	}

	// Parse dialogue events and deeply verify timing, order, and cue texts
	// The failed window falls back to timing-based pairing for dialogue.
	// Bracketed sound cues and captions have no reliable timing partner and
	// stay one-sided.
	events := parseASSEvents(t, assText)
	if len(events) != 3 {
		t.Fatalf("expected 3 dialogue events (1 pair + 2 one-sided), got %d:\n%s", len(events), assText)
	}

	// Verify exact timing and text
	expected := []struct {
		start string
		end   string
		texts []string
	}{
		{start: "0:00:01.00", end: "0:00:03.00", texts: []string{"你好", "Hello"}},
		{start: "0:00:04.00", end: "0:00:06.00", texts: []string{"[phone ringing]"}},
		{start: "0:00:04.00", end: "0:00:06.00", texts: []string{"【三天后】"}},
	}

	for i, exp := range expected {
		ev := events[i]
		if ev.start != exp.start || ev.end != exp.end {
			t.Errorf("event [%d] timing mismatch: got %s->%s, want %s->%s", i, ev.start, ev.end, exp.start, exp.end)
		}
		for _, text := range exp.texts {
			if !strings.Contains(ev.text, text) {
				t.Errorf("event [%d] text mismatch: got %q, want %q", i, ev.text, text)
			}
		}
	}

	// Verify chronological order (start times must be non-decreasing)
	for i := 1; i < len(events); i++ {
		if events[i].start < events[i-1].start {
			t.Errorf("event [%d] start time (%s) < event [%d] start time (%s)", i, events[i].start, i-1, events[i-1].start)
		}
	}

	// Verify degraded metadata flag is recorded
	if degraded, ok := pctx.Metadata["llm_degraded"].(bool); !ok || !degraded {
		t.Errorf("expected llm_degraded metadata to be true, got %v", pctx.Metadata["llm_degraded"])
	}
	// The bracketed sound cue is not counted as dialogue.
	wantPairing := subtitlellm.Pairing{ENPaired: 1, ENTotal: 1, ZHPaired: 1, ZHTotal: 1}
	if pairing, ok := pctx.Metadata[MetadataLLMPairing].(subtitlellm.Pairing); !ok || pairing != wantPairing {
		t.Errorf("llm_pairing metadata = %+v, want %+v", pctx.Metadata[MetadataLLMPairing], wantPairing)
	}

	// Verify audit evidence is persisted and traceable
	auditPath, ok := pctx.Metadata[MetadataLLMAuditPath].(string)
	if !ok || auditPath == "" {
		t.Fatal("audit artifact path missing in metadata")
	}
	auditData, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("read audit file: %v", err)
	}
	var audit subtitlellm.Audit
	if err := json.Unmarshal(auditData, &audit); err != nil {
		t.Fatalf("unmarshal audit: %v", err)
	}
	if len(audit.FailedWindows) != 1 || audit.FailedWindows[0] != 0 {
		t.Errorf("expected FailedWindows to record window 0 failure, got %+v", audit.FailedWindows)
	}
	if len(audit.WindowFailures) != 1 {
		t.Fatalf("expected 1 WindowFailure, got %+v", audit.WindowFailures)
	}
	if !strings.Contains(audit.WindowFailures[0].Reason, "HTTP 503") {
		t.Errorf("expected reason to contain HTTP 503, got %s", audit.WindowFailures[0].Reason)
	}
}

// TestFallbackBehavior_NegativeCasesPreserved verifies that fatal conditions like
// context cancellation, bad source files, and cache I/O errors remain hard errors.
func TestFallbackBehavior_NegativeCasesPreserved(t *testing.T) {
	t.Run("context cancellation returns error", func(t *testing.T) {
		videoPath, enPath, zhPath := mergerFixture(t)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "upstream 503", http.StatusServiceUnavailable)
		}))
		defer server.Close()

		llm := llmMergerTestConfig(t, pairingHandler(t))
		llm.BaseURL = server.URL + "/v1"
		processor := newLLMTestProcessor(t, llm)
		processor.probe = func(context.Context, string) (*executor.FFProbeOutput, error) { return nil, errors.New("no probe") }

		ctx, cancel := context.WithCancel(context.Background())
		cancel() // pre-cancel context

		pctx := &ProcessingContext{VideoPath: videoPath, EnglishSubPath: enPath, ChineseSubPath: zhPath, Metadata: map[string]interface{}{}}
		err := processor.Process(ctx, pctx)
		if err == nil {
			t.Fatal("expected cancellation error, got nil")
		}
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
		if pctx.MergedSubPath != "" {
			t.Fatalf("merged sub path should not be set on cancellation: %s", pctx.MergedSubPath)
		}
	})

	t.Run("bad source input returns error", func(t *testing.T) {
		videoPath, _, zhPath := mergerFixture(t)
		llm := llmMergerTestConfig(t, pairingHandler(t))
		processor := newLLMTestProcessor(t, llm)
		processor.probe = func(context.Context, string) (*executor.FFProbeOutput, error) { return nil, errors.New("no probe") }

		// Pass non-existent English subtitle path
		pctx := &ProcessingContext{VideoPath: videoPath, EnglishSubPath: filepath.Join(t.TempDir(), "nonexistent.srt"), ChineseSubPath: zhPath, Metadata: map[string]interface{}{}}
		err := processor.Process(context.Background(), pctx)
		if err == nil {
			t.Fatal("expected error for non-existent source, got nil")
		}
		if !strings.Contains(err.Error(), "load subtitle sources") {
			t.Fatalf("expected load subtitle sources error, got %v", err)
		}
	})

	t.Run("non-permission cache IO error returns error", func(t *testing.T) {
		videoPath, enPath, zhPath := mergerFixture(t)
		dir := t.TempDir()
		// Create a file where cache directory should be (triggers ENOTDIR)
		fileAsDir := filepath.Join(dir, "blocked-cache")
		if err := os.WriteFile(fileAsDir, []byte("regular file"), 0644); err != nil {
			t.Fatal(err)
		}

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "upstream 503", http.StatusServiceUnavailable)
		}))
		defer server.Close()

		llm := llmMergerTestConfig(t, pairingHandler(t))
		llm.BaseURL = server.URL + "/v1"
		llm.CacheEnabled = true
		llm.CacheDir = fileAsDir
		processor := newLLMTestProcessor(t, llm)
		processor.probe = func(context.Context, string) (*executor.FFProbeOutput, error) { return nil, errors.New("no probe") }

		pctx := &ProcessingContext{VideoPath: videoPath, EnglishSubPath: enPath, ChineseSubPath: zhPath, Metadata: map[string]interface{}{}}
		err := processor.Process(context.Background(), pctx)
		if err == nil {
			t.Fatal("expected hard error on cache ENOTDIR, got nil")
		}
	})

	t.Run("artifact persistence failure returns error", func(t *testing.T) {
		videoPath, enPath, zhPath := mergerFixture(t)
		dir := t.TempDir()
		// Create a regular file where the llm-alignment artifact directory would be created
		blockedArtifacts := filepath.Join(dir, "llm-alignment")
		if err := os.WriteFile(blockedArtifacts, []byte("regular file"), 0644); err != nil {
			t.Fatal(err)
		}

		server := httptest.NewServer(pairingHandler(t))
		defer server.Close()

		llm := llmMergerTestConfig(t, pairingHandler(t))
		llm.BaseURL = server.URL + "/v1"
		llm.CacheEnabled = true
		llm.CacheDir = dir // artifact root will be under dir/llm-alignment/...
		processor := newLLMTestProcessor(t, llm)
		processor.probe = func(context.Context, string) (*executor.FFProbeOutput, error) { return nil, errors.New("no probe") }

		pctx := &ProcessingContext{VideoPath: videoPath, EnglishSubPath: enPath, ChineseSubPath: zhPath, Metadata: map[string]interface{}{}}
		err := processor.Process(context.Background(), pctx)
		if err == nil {
			t.Fatal("expected hard error on artifact persistence failure, got nil")
		}
		if !strings.Contains(err.Error(), "persist alignment artifacts") {
			t.Fatalf("expected error mentioning persist alignment artifacts, got %v", err)
		}
	})
}

// TestFallbackBehavior_HTTP401NoRetryFallbackWithReason verifies that HTTP 401
// is not retried, but gracefully degrades with the sanitized reason recorded.
func TestFallbackBehavior_HTTP401NoRetryFallbackWithReason(t *testing.T) {
	videoPath, enPath, zhPath := mergerFixture(t)

	var requestCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		http.Error(w, "Unauthorized API key", http.StatusUnauthorized)
	}))
	defer server.Close()

	llm := llmMergerTestConfig(t, pairingHandler(t))
	llm.BaseURL = server.URL + "/v1"
	llm.CacheDir = t.TempDir()
	llm.Retries = 3 // Even with 3 retries configured, 401 must not retry

	processor := newLLMTestProcessor(t, llm)
	processor.probe = func(context.Context, string) (*executor.FFProbeOutput, error) {
		return nil, errors.New("no probe")
	}

	pctx := &ProcessingContext{
		VideoPath:      videoPath,
		EnglishSubPath: enPath,
		ChineseSubPath: zhPath,
		Metadata:       map[string]interface{}{},
	}

	err := processor.Process(context.Background(), pctx)
	if err != nil {
		t.Fatalf("Process() error = %v, want fallback", err)
	}

	if requestCount != 1 {
		t.Fatalf("request count = %d, want 1 (HTTP 401 must not be retried)", requestCount)
	}

	failures, ok := pctx.Metadata[MetadataLLMWindowFailures].([]subtitlellm.WindowFailure)
	if !ok || len(failures) == 0 {
		t.Fatalf("expected WindowFailures in metadata, got %v", pctx.Metadata[MetadataLLMWindowFailures])
	}
	if !strings.Contains(failures[0].Reason, "HTTP 401") {
		t.Errorf("expected failure reason to contain HTTP 401, got %q", failures[0].Reason)
	}
}

// TestFallbackBehavior_CacheDisabledTraceability verifies that when cache is disabled,
// warnings and metadata still preserve the window failure reasons.
func TestFallbackBehavior_CacheDisabledTraceability(t *testing.T) {
	videoPath, enPath, zhPath := mergerFixture(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Bad Request", http.StatusBadRequest)
	}))
	defer server.Close()

	llm := llmMergerTestConfig(t, pairingHandler(t))
	llm.BaseURL = server.URL + "/v1"
	llm.CacheEnabled = false // cache disabled
	llm.CacheDir = ""
	llm.Retries = 0

	processor := newLLMTestProcessor(t, llm)
	processor.probe = func(context.Context, string) (*executor.FFProbeOutput, error) {
		return nil, errors.New("no probe")
	}

	pctx := &ProcessingContext{
		VideoPath:      videoPath,
		EnglishSubPath: enPath,
		ChineseSubPath: zhPath,
		Metadata:       map[string]interface{}{},
	}

	err := processor.Process(context.Background(), pctx)
	if err != nil {
		t.Fatalf("Process() error = %v, want fallback", err)
	}

	if degraded, ok := pctx.Metadata[MetadataLLMDegraded].(bool); !ok || !degraded {
		t.Errorf("expected MetadataLLMDegraded=true, got %v", pctx.Metadata[MetadataLLMDegraded])
	}

	failures, ok := pctx.Metadata[MetadataLLMWindowFailures].([]subtitlellm.WindowFailure)
	if !ok || len(failures) != 1 {
		t.Fatalf("expected 1 WindowFailure in metadata with cache disabled, got %v", pctx.Metadata[MetadataLLMWindowFailures])
	}
	if !strings.Contains(failures[0].Reason, "HTTP 400") {
		t.Errorf("expected failure reason to contain HTTP 400, got %q", failures[0].Reason)
	}
}
