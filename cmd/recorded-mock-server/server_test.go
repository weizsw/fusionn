package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func makeTestCacheFile(t *testing.T, dir string, index int, coreStart, coreEnd int, content string) {
	t.Helper()
	reqBody := fmt.Sprintf(`{"model":"test-model","messages":[{"role":"system","content":"test prompt"},{"role":"user","content":"{\"core_ms\":[%d,%d],\"en\":[],\"zh\":[]}"}]}`, coreStart, coreEnd)
	record := map[string]any{
		"index":        index,
		"request_hash": fmt.Sprintf("hash-%d", index),
		"request":      json.RawMessage(reqBody),
		"attempts": []map[string]any{
			{
				"model":         "test-model",
				"finish_reason": "stop",
				"content":       content,
				"usage": map[string]int{
					"prompt_tokens":     12,
					"completion_tokens": 34,
				},
			},
		},
		"response": json.RawMessage(content),
	}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	fName := fmt.Sprintf("%03d-test.json", index)
	fPath := filepath.Join(dir, fName)
	if err := os.WriteFile(fPath, data, 0644); err != nil {
		t.Fatal(err)
	}
}

func sendChatRequest(t *testing.T, ts *httptest.Server, coreStart, coreEnd int) (*http.Response, string) {
	t.Helper()
	reqBody := fmt.Sprintf(`{"model":"client-model","messages":[{"role":"system","content":"sys"},{"role":"user","content":"{\"core_ms\":[%d,%d],\"en\":[],\"zh\":[]}"}]}`, coreStart, coreEnd)
	resp, err := ts.Client().Post(ts.URL+"/chat/completions", "application/json", bytes.NewReader([]byte(reqBody)))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp, string(body)
}

func TestReplayServer_StrictIdentityAndNon210SecFixture(t *testing.T) {
	tempDir := t.TempDir()
	// Create two fixtures with arbitrary, non-210-second intervals
	makeTestCacheFile(t, tempDir, 0, 1000, 5000, `{"alignments":[{"en_ids":[1],"zh_ids":[1],"layer":"bottom","kind":"dialogue"}]}`)
	makeTestCacheFile(t, tempDir, 1, 5000, 12000, `{"alignments":[{"en_ids":[2],"zh_ids":[2],"layer":"bottom","kind":"dialogue"}]}`)

	server, err := NewReplayServer(tempDir, -1, 503)
	if err != nil {
		t.Fatalf("NewReplayServer: %v", err)
	}

	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	// 1. Exact match on window 0 [1000, 5000]
	resp0, body0 := sendChatRequest(t, ts, 1000, 5000)
	if resp0.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for window 0, got %d: %s", resp0.StatusCode, body0)
	}
	var chatResp0 struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal([]byte(body0), &chatResp0); err != nil {
		t.Fatalf("unmarshal resp: %v", err)
	}
	if chatResp0.Model != "test-model" {
		t.Errorf("expected model 'test-model', got %q", chatResp0.Model)
	}
	if chatResp0.Usage.PromptTokens != 12 || chatResp0.Usage.CompletionTokens != 34 {
		t.Errorf("expected preserved recorded usage 12/34, got %+v", chatResp0.Usage)
	}

	// 2. Exact match on window 1 [5000, 12000]
	resp1, body1 := sendChatRequest(t, ts, 5000, 12000)
	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for window 1, got %d: %s", resp1.StatusCode, body1)
	}

	// 3. Unknown core_ms [9999, 19999] MUST return HTTP 400
	respUnknown, bodyUnknown := sendChatRequest(t, ts, 9999, 19999)
	if respUnknown.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for unknown core_ms, got %d: %s", respUnknown.StatusCode, bodyUnknown)
	}
}

func TestReplayServer_MissingCoreMS_Returns400(t *testing.T) {
	tempDir := t.TempDir()
	makeTestCacheFile(t, tempDir, 0, 0, 1000, `{"alignments":[]}`)

	server, err := NewReplayServer(tempDir, -1, 503)
	if err != nil {
		t.Fatalf("NewReplayServer: %v", err)
	}

	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	// Missing core_ms in user message
	badReq := `{"model":"m","messages":[{"role":"user","content":"{\"en\":[]}"}]}`
	resp, errBad := ts.Client().Post(ts.URL+"/chat/completions", "application/json", bytes.NewReader([]byte(badReq)))
	if errBad != nil {
		t.Fatalf("post bad req: %v", errBad)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for missing core_ms, got %d", resp.StatusCode)
	}

	// Completely invalid JSON
	respInv, errInv := ts.Client().Post(ts.URL+"/chat/completions", "application/json", bytes.NewReader([]byte("not json")))
	if errInv != nil {
		t.Fatalf("post inv req: %v", errInv)
	}
	defer respInv.Body.Close()
	if respInv.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid json, got %d", respInv.StatusCode)
	}
}

func TestReplayServer_AttemptSelection_PicksSuccessfulAttempt(t *testing.T) {
	tempDir := t.TempDir()

	// Record with attempt 0 failed, attempt 1 succeeded
	reqBody := `{"model":"test-model","messages":[{"role":"user","content":"{\"core_ms\":[0,1000],\"en\":[],\"zh\":[]}"}]}`
	record := map[string]any{
		"index":        0,
		"request_hash": "hash-attempt-select",
		"request":      json.RawMessage(reqBody),
		"attempts": []map[string]any{
			{
				"model":         "test-model",
				"finish_reason": "error",
				"error":         "rate limit exceeded 429",
				"content":       "",
			},
			{
				"model":         "test-model-success",
				"finish_reason": "stop",
				"content":       `{"alignments":[{"en_ids":[10],"zh_ids":[20],"layer":"bottom","kind":"dialogue"}]}`,
			},
		},
	}
	data, _ := json.Marshal(record)
	_ = os.WriteFile(filepath.Join(tempDir, "000-attempt.json"), data, 0644)

	server, err := NewReplayServer(tempDir, -1, 503)
	if err != nil {
		t.Fatalf("NewReplayServer: %v", err)
	}

	win := server.Windows[[2]int{0, 1000}]
	if win == nil {
		t.Fatalf("window [0, 1000] not found")
	}
	if win.Model != "test-model-success" {
		t.Errorf("expected chosen model from attempt 1 'test-model-success', got %q", win.Model)
	}
	if win.Content != `{"alignments":[{"en_ids":[10],"zh_ids":[20],"layer":"bottom","kind":"dialogue"}]}` {
		t.Errorf("expected chosen content from attempt 1, got %q", win.Content)
	}
}

func TestReplayServer_FailWindow_StableRetry503AndStats(t *testing.T) {
	tempDir := t.TempDir()
	makeTestCacheFile(t, tempDir, 0, 0, 1000, `{"alignments":[]}`)
	makeTestCacheFile(t, tempDir, 1, 1000, 2000, `{"alignments":[]}`)

	// Configure window 1 to fail with HTTP 503
	server, err := NewReplayServer(tempDir, 1, 503)
	if err != nil {
		t.Fatalf("NewReplayServer: %v", err)
	}

	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	// Window 0 succeeds
	resp0, _ := sendChatRequest(t, ts, 0, 1000)
	if resp0.StatusCode != http.StatusOK {
		t.Errorf("expected 200 for window 0, got %d", resp0.StatusCode)
	}

	// Window 1 fails on attempt 1
	resp1a, _ := sendChatRequest(t, ts, 1000, 2000)
	if resp1a.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected 503 for window 1, got %d", resp1a.StatusCode)
	}

	// Retry window 1 (attempt 2) stably fails with 503
	resp1b, _ := sendChatRequest(t, ts, 1000, 2000)
	if resp1b.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected 503 on retry for window 1, got %d", resp1b.StatusCode)
	}

	// Retry window 1 (attempt 3) stably fails with 503
	resp1c, _ := sendChatRequest(t, ts, 1000, 2000)
	if resp1c.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected 503 on retry for window 1, got %d", resp1c.StatusCode)
	}

	stats := server.GetStats()
	if stats.TotalRequests != 4 {
		t.Errorf("expected 4 total requests, got %d", stats.TotalRequests)
	}
	if stats.FailedAttempts != 3 {
		t.Errorf("expected 3 failed attempts, got %d", stats.FailedAttempts)
	}
	if stats.WindowAttempts[0] != 1 {
		t.Errorf("expected 1 attempt for window 0, got %d", stats.WindowAttempts[0])
	}
	if stats.WindowAttempts[1] != 3 {
		t.Errorf("expected 3 attempts for window 1, got %d", stats.WindowAttempts[1])
	}

	// Verify /stats endpoint returns the same
	statsResp, errGet := ts.Client().Get(ts.URL + "/stats")
	if errGet != nil {
		t.Fatal(errGet)
	}
	defer statsResp.Body.Close()
	var endpointStats ServerStats
	if err := json.NewDecoder(statsResp.Body).Decode(&endpointStats); err != nil {
		t.Fatal(err)
	}
	if endpointStats.TotalRequests != 4 || endpointStats.FailedAttempts != 3 {
		t.Errorf("unexpected endpoint stats: %+v", endpointStats)
	}
}

func TestReplayServer_CorruptFileOrEmptyDir_FailsFast(t *testing.T) {
	// Empty dir
	emptyDir := t.TempDir()
	_, errEmpty := NewReplayServer(emptyDir, -1, 503)
	if errEmpty == nil {
		t.Errorf("expected error for empty dir, got nil")
	}

	// Corrupt JSON file
	badDir := t.TempDir()
	_ = os.WriteFile(filepath.Join(badDir, "000-bad.json"), []byte("{bad json}"), 0644)
	_, errBad := NewReplayServer(badDir, -1, 503)
	if errBad == nil {
		t.Errorf("expected error for bad json, got nil")
	}
}

func TestReplayServer_RealPhaseBCache(t *testing.T) {
	cacheDir := os.Getenv("FUSIONN_RECORDED_CACHE_DIR")
	if cacheDir == "" {
		t.Skip("skipping real cache test: opt in via FUSIONN_RECORDED_CACHE_DIR")
	}

	server, err := NewReplayServer(cacheDir, -1, 503)
	if err != nil {
		t.Fatalf("failed to initialize replay server from real cache: %v", err)
	}

	if len(server.Windows) != 15 {
		t.Errorf("expected 15 windows loaded, got %d", len(server.Windows))
	}
}
