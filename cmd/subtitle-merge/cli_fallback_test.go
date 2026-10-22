package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// buildSubtitleMerge compiles the CLI binary into an isolated t.TempDir() for the test.
func buildSubtitleMerge(t *testing.T) string {
	t.Helper()
	binPath := filepath.Join(t.TempDir(), "subtitle-merge")
	cmd := exec.Command("go", "build", "-o", binPath, ".")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build subtitle-merge: %v\n%s", err, string(out))
	}
	return binPath
}

// sanitizedSubprocessEnv filters out all FUSIONN_* environment variables so host
// overrides (e.g. viper AutomaticEnv) cannot hijack the test run to external endpoints.
func sanitizedSubprocessEnv() []string {
	var env []string
	for _, e := range os.Environ() {
		if !strings.HasPrefix(e, "FUSIONN_") {
			env = append(env, e)
		}
	}
	return env
}

func writeTempSRT(t *testing.T, dir, filename, content string) string {
	t.Helper()
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write %s: %v", filename, err)
	}
	return path
}

func writeTempConfig(t *testing.T, dir, baseURL, cacheDir string, retries int) string {
	t.Helper()
	cfg := fmt.Sprintf(`server:
  host: "127.0.0.1"
  port: 8080
subtitle:
  enabled: true
  engine: "llm"
  llm:
    base_url: %q
    model: "test-model"
    response_format: "json_object"
    timeout_seconds: 5
    workers: 1
    retries: %d
    window_seconds: 210
    overlap_seconds: 15
    cache_enabled: true
    cache_dir: %q
    en_chars: 65
    zh_chars: 25
    span_ms: 5500
    en_gap_ms: 400
    zh_gap_ms: 500
    fill_gap_ms: 500
  ass_style:
    enabled: true
    play_res_x: 1920
    play_res_y: 1080
    primary_font: "PingFang SC"
    primary_size: 52
    secondary_font: "Helvetica Neue"
    secondary_size: 36
    scale_x: 100
    line_height_ratio: 1.23
`, baseURL+"/v1", retries, cacheDir)

	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(cfg), 0644); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}
	return path
}

type parsedDialogue struct {
	layer   string
	start   string
	end     string
	style   string
	name    string
	marginL string
	marginR string
	marginV string
	effect  string
	text    string
}

func parseDialogueEvents(t *testing.T, assContent string) []parsedDialogue {
	t.Helper()
	var events []parsedDialogue
	inEvents := false
	for _, rawLine := range strings.Split(assContent, "\n") {
		line := strings.TrimRight(rawLine, "\r")
		if strings.HasPrefix(line, "[Events]") {
			inEvents = true
			continue
		}
		if inEvents && strings.HasPrefix(line, "Dialogue:") {
			parts := strings.SplitN(line, ",", 10)
			if len(parts) < 10 {
				t.Fatalf("malformed Dialogue line: %s", line)
			}
			layer := strings.TrimSpace(strings.TrimPrefix(parts[0], "Dialogue:"))
			events = append(events, parsedDialogue{
				layer:   layer,
				start:   strings.TrimSpace(parts[1]),
				end:     strings.TrimSpace(parts[2]),
				style:   strings.TrimSpace(parts[3]),
				name:    strings.TrimSpace(parts[4]),
				marginL: strings.TrimSpace(parts[5]),
				marginR: strings.TrimSpace(parts[6]),
				marginV: strings.TrimSpace(parts[7]),
				effect:  strings.TrimSpace(parts[8]),
				text:    parts[9],
			})
		}
	}
	return events
}

const sampleEnSRT = `1
00:00:01,000 --> 00:00:03,000
You go ashore and wait.

2
00:00:04,000 --> 00:00:06,000
Bring all your trouble to the doorstep.
`

const sampleZhSRT = `1
00:00:01,200 --> 00:00:03,200
你在岸上等我。

2
00:00:04,200 --> 00:00:06,200
把你的麻烦带到门口。
`

func TestCLIFallback_Subprocess_LLM503FallbackToNativeASS(t *testing.T) {
	bin := buildSubtitleMerge(t)

	// Upstream LLM returns HTTP 503 for all window requests
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream model service overloaded (503)", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	testDir := t.TempDir()
	cacheDir := filepath.Join(testDir, "cache")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		t.Fatalf("mkdir cache: %v", err)
	}

	enPath := writeTempSRT(t, testDir, "en.srt", sampleEnSRT)
	zhPath := writeTempSRT(t, testDir, "zh.srt", sampleZhSRT)
	cfgPath := writeTempConfig(t, testDir, server.URL, cacheDir, 0)
	outDir := filepath.Join(testDir, "output")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, "--config", cfgPath, "--en", enPath, "--zh", zhPath, "--out", outDir)
	cmd.Env = sanitizedSubprocessEnv()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		t.Fatalf("CLI subprocess failed: %v\nstderr:\n%s\nstdout:\n%s", err, stderr.String(), stdout.String())
	}

	// 1. Verify stderr degradation warning
	stderrStr := stderr.String()
	wantWarn := "Warning: LLM alignment partially failed (1 windows); fell back to native timing and cue order for failed windows"
	if !strings.Contains(stderrStr, wantWarn) {
		t.Errorf("expected stderr to contain %q, got:\n%s", wantWarn, stderrStr)
	}

	// 2. Verify stdout status line and fallback window count
	stdoutStr := stdout.String()
	if !strings.Contains(stdoutStr, "fallback_windows: 1") {
		t.Errorf("expected stdout to report 'fallback_windows: 1', got:\n%s", stdoutStr)
	}
	if !strings.Contains(stdoutStr, "bilingual.ass") {
		t.Errorf("expected stdout to reference bilingual.ass, got:\n%s", stdoutStr)
	}

	// 3. Deep parse and verify bilingual.ass
	assPath := filepath.Join(outDir, "bilingual.ass")
	assBytes, err := os.ReadFile(assPath)
	if err != nil {
		t.Fatalf("failed to read bilingual.ass: %v", err)
	}
	assText := string(assBytes)

	// ASS header and sections
	if !strings.Contains(assText, "[Script Info]") {
		t.Error("bilingual.ass missing [Script Info]")
	}
	if !strings.Contains(assText, "[V4+ Styles]") {
		t.Error("bilingual.ass missing [V4+ Styles]")
	}
	if !strings.Contains(assText, "[Events]") {
		t.Error("bilingual.ass missing [Events]")
	}

	events := parseDialogueEvents(t, assText)
	if len(events) != 4 {
		t.Fatalf("expected exactly 4 dialogue events (2 EN + 2 ZH), got %d:\n%s", len(events), assText)
	}

	// Verify exact timing, styles, and text of the 4 native fallback events
	// Event 0: EN 1 [1.00s..3.00s]
	// Event 1: ZH 1 [1.20s..3.20s]
	// Event 2: EN 2 [4.00s..6.00s]
	// Event 3: ZH 2 [4.20s..6.20s]
	expectedEvents := []struct {
		start    string
		end      string
		style    string
		textPart string
	}{
		{"0:00:01.00", "0:00:03.00", "Eng", "You go ashore and wait."},
		{"0:00:01.20", "0:00:03.20", "Default", "你在岸上等我。"},
		{"0:00:04.00", "0:00:06.00", "Eng", "Bring all your trouble to the doorstep."},
		{"0:00:04.20", "0:00:06.20", "Default", "把你的麻烦带到门口。"},
	}

	for i, exp := range expectedEvents {
		ev := events[i]
		if ev.start != exp.start || ev.end != exp.end {
			t.Errorf("event %d time mismatch: got %s..%s, want %s..%s", i, ev.start, ev.end, exp.start, exp.end)
		}
		if ev.style != exp.style {
			t.Errorf("event %d style mismatch: got %s, want %s", i, ev.style, exp.style)
		}
		if !strings.Contains(ev.text, exp.textPart) {
			t.Errorf("event %d text mismatch: %q does not contain %q", i, ev.text, exp.textPart)
		}
	}

	// 4. Unmarshal and verify sources.json
	sourcesPath := filepath.Join(outDir, "sources.json")
	sourcesBytes, err := os.ReadFile(sourcesPath)
	if err != nil {
		t.Fatalf("failed to read sources.json: %v", err)
	}
	var bundle struct {
		Fingerprint string `json:"fingerprint"`
		Version     int    `json:"version"`
		English     []struct {
			ID    int    `json:"id"`
			Start int    `json:"start"`
			End   int    `json:"end"`
			Plain string `json:"plain"`
		} `json:"en"`
		Chinese []struct {
			ID    int    `json:"id"`
			Start int    `json:"start"`
			End   int    `json:"end"`
			Plain string `json:"plain"`
		} `json:"zh"`
	}
	if err := json.Unmarshal(sourcesBytes, &bundle); err != nil {
		t.Fatalf("unmarshal sources.json: %v", err)
	}
	if bundle.Fingerprint == "" {
		t.Fatal("bundle fingerprint in sources.json is empty")
	}
	if len(bundle.English) != 2 || len(bundle.Chinese) != 2 {
		t.Fatalf("unexpected cue counts: EN=%d, ZH=%d", len(bundle.English), len(bundle.Chinese))
	}
	if bundle.English[0].ID != 1 || bundle.English[0].Start != 1000 || bundle.English[0].End != 3000 || bundle.English[0].Plain != "You go ashore and wait." {
		t.Errorf("unexpected bundle EN[0]: %+v", bundle.English[0])
	}
	if bundle.Chinese[0].ID != 1 || bundle.Chinese[0].Start != 1200 || bundle.Chinese[0].End != 3200 || bundle.Chinese[0].Plain != "你在岸上等我。" {
		t.Errorf("unexpected bundle ZH[0]: %+v", bundle.Chinese[0])
	}

	// 5. Unmarshal and verify alignment/mapping.json
	mappingPath := filepath.Join(outDir, "alignment", "mapping.json")
	mappingBytes, err := os.ReadFile(mappingPath)
	if err != nil {
		t.Fatalf("failed to read alignment/mapping.json: %v", err)
	}
	var mappingData struct {
		SourceFingerprint string `json:"source_fingerprint"`
		Model             string `json:"model"`
		Groups            []struct {
			EnglishIDs []int `json:"en_ids"`
			ChineseIDs []int `json:"zh_ids"`
		} `json:"groups"`
	}
	if err := json.Unmarshal(mappingBytes, &mappingData); err != nil {
		t.Fatalf("unmarshal mapping.json: %v", err)
	}
	if mappingData.SourceFingerprint != bundle.Fingerprint {
		t.Errorf("mapping fingerprint mismatch: %s vs bundle %s", mappingData.SourceFingerprint, bundle.Fingerprint)
	}
	if len(mappingData.Groups) == 0 {
		t.Fatal("mapping groups is empty")
	}
	// Verify that all source cue IDs are accounted for across groups
	var mappedEN, mappedZH []int
	for _, g := range mappingData.Groups {
		mappedEN = append(mappedEN, g.EnglishIDs...)
		mappedZH = append(mappedZH, g.ChineseIDs...)
	}
	sort.Ints(mappedEN)
	sort.Ints(mappedZH)
	if len(mappedEN) != 2 || mappedEN[0] != 1 || mappedEN[1] != 2 {
		t.Errorf("mapped EN IDs mismatch: %v", mappedEN)
	}
	if len(mappedZH) != 2 || mappedZH[0] != 1 || mappedZH[1] != 2 {
		t.Errorf("mapped ZH IDs mismatch: %v", mappedZH)
	}

	// 6. Unmarshal and verify alignment/audit.json
	auditPath := filepath.Join(outDir, "alignment", "audit.json")
	auditBytes, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("failed to read alignment/audit.json: %v", err)
	}
	var auditData struct {
		FailedWindows  []int `json:"failed_windows"`
		WindowFailures []struct {
			Window int    `json:"window"`
			Reason string `json:"reason"`
		} `json:"window_failures"`
		HTTPAttempts int `json:"http_attempts_this_run"`
	}
	if err := json.Unmarshal(auditBytes, &auditData); err != nil {
		t.Fatalf("unmarshal audit.json: %v", err)
	}
	if len(auditData.FailedWindows) != 1 || auditData.FailedWindows[0] != 0 {
		t.Fatalf("expected FailedWindows [0], got %v", auditData.FailedWindows)
	}
	if len(auditData.WindowFailures) != 1 || auditData.WindowFailures[0].Window != 0 {
		t.Fatalf("expected exactly 1 window failure for window 0, got %+v", auditData.WindowFailures)
	}
	if !strings.Contains(auditData.WindowFailures[0].Reason, "503") {
		t.Errorf("expected audit failure reason to mention 503, got %q", auditData.WindowFailures[0].Reason)
	}
	if auditData.HTTPAttempts < 1 {
		t.Errorf("expected audit HTTPAttempts >= 1, got %d", auditData.HTTPAttempts)
	}

	// 7. Unmarshal and verify run.json
	runPath := filepath.Join(outDir, "run.json")
	runBytes, err := os.ReadFile(runPath)
	if err != nil {
		t.Fatalf("failed to read run.json: %v", err)
	}
	var runStats struct {
		SourceFingerprint string  `json:"source_fingerprint"`
		HTTPAttempts      int     `json:"http_attempts"`
		MappingGroups     int     `json:"mapping_groups"`
		Seconds           float64 `json:"seconds"`
	}
	if err := json.Unmarshal(runBytes, &runStats); err != nil {
		t.Fatalf("unmarshal run.json: %v", err)
	}
	if runStats.SourceFingerprint != bundle.Fingerprint {
		t.Errorf("run.json fingerprint mismatch: got %s, want %s", runStats.SourceFingerprint, bundle.Fingerprint)
	}
	if runStats.HTTPAttempts != auditData.HTTPAttempts {
		t.Errorf("run.json http_attempts mismatch: got %d, want %d", runStats.HTTPAttempts, auditData.HTTPAttempts)
	}
	if runStats.MappingGroups != len(mappingData.Groups) {
		t.Errorf("run.json mapping_groups mismatch: got %d, want %d", runStats.MappingGroups, len(mappingData.Groups))
	}
	if runStats.Seconds <= 0 {
		t.Errorf("expected positive runtime seconds, got %f", runStats.Seconds)
	}
}

func TestCLIFallback_Subprocess_HTTP401NoRetryFallback(t *testing.T) {
	bin := buildSubtitleMerge(t)

	var reqCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqCount++
		http.Error(w, "invalid api key (401)", http.StatusUnauthorized)
	}))
	defer server.Close()

	testDir := t.TempDir()
	cacheDir := filepath.Join(testDir, "cache")
	_ = os.MkdirAll(cacheDir, 0755)

	enPath := writeTempSRT(t, testDir, "en.srt", sampleEnSRT)
	zhPath := writeTempSRT(t, testDir, "zh.srt", sampleZhSRT)
	// Configured with 3 retries, but 401 must fail-fast without retry
	cfgPath := writeTempConfig(t, testDir, server.URL, cacheDir, 3)
	outDir := filepath.Join(testDir, "output")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, "--config", cfgPath, "--en", enPath, "--zh", zhPath, "--out", outDir)
	cmd.Env = sanitizedSubprocessEnv()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		t.Fatalf("CLI subprocess failed on 401: %v\nstderr:\n%s\nstdout:\n%s", err, stderr.String(), stdout.String())
	}

	if reqCount != 1 {
		t.Errorf("expected 401 to fail-fast without retry (reqCount=1), got %d", reqCount)
	}

	if !strings.Contains(stderr.String(), "Warning: LLM alignment partially failed (1 windows)") {
		t.Errorf("missing fallback warning in stderr:\n%s", stderr.String())
	}
	if !strings.Contains(stdout.String(), "fallback_windows: 1") {
		t.Errorf("missing fallback_windows: 1 in stdout:\n%s", stdout.String())
	}
}

// TestCLIFallback_Subprocess_HostEnvPoisoningImmunity proves that sanitizedSubprocessEnv
// prevents host FUSIONN_* environment variables (e.g. FUSIONN_SUBTITLE_LLM_BASE_URL)
// from hijacking the subprocess to a bogus or malicious endpoint.
func TestCLIFallback_Subprocess_HostEnvPoisoningImmunity(t *testing.T) {
	bin := buildSubtitleMerge(t)

	// Valid local httptest server returning 503 (degradable)
	var validServerHit bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		validServerHit = true
		http.Error(w, "upstream 503", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	testDir := t.TempDir()
	cacheDir := filepath.Join(testDir, "cache")
	_ = os.MkdirAll(cacheDir, 0755)

	enPath := writeTempSRT(t, testDir, "en.srt", sampleEnSRT)
	zhPath := writeTempSRT(t, testDir, "zh.srt", sampleZhSRT)
	cfgPath := writeTempConfig(t, testDir, server.URL, cacheDir, 0)
	outDir := filepath.Join(testDir, "output")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Inject a poisoned endpoint into host env that would fail hard if inherited
	t.Setenv("FUSIONN_SUBTITLE_LLM_BASE_URL", "http://127.0.0.1:59999/poisoned-nonexistent-v1")

	cmd := exec.CommandContext(ctx, bin, "--config", cfgPath, "--en", enPath, "--zh", zhPath, "--out", outDir)
	cmd.Env = sanitizedSubprocessEnv() // Sanitized environment strips FUSIONN_*
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err != nil {
		t.Fatalf("subprocess failed with sanitized env: %v\nstderr: %s", err, stderr.String())
	}

	if !validServerHit {
		t.Fatal("sanitized subprocess did not hit the configured httptest server; was it hijacked?")
	}
	if !strings.Contains(stderr.String(), "Warning: LLM alignment partially failed (1 windows)") {
		t.Errorf("missing fallback warning in stderr:\n%s", stderr.String())
	}
}
