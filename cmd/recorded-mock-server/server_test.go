package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func createMockCacheDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	for i := 0; i < 3; i++ {
		content := fmt.Sprintf(`{"alignments":[{"en_ids":[%d],"zh_ids":[%d],"layer":"bottom","kind":"dialogue"}]}`, i+1, i+1)
		fileData := map[string]interface{}{
			"index": i,
			"attempts": []map[string]interface{}{
				{"content": content},
			},
			"response": map[string]interface{}{
				"alignments": []map[string]interface{}{
					{"en_ids": []int{i + 1}, "zh_ids": []int{i + 1}, "layer": "bottom", "kind": "dialogue"},
				},
			},
		}
		raw, err := json.Marshal(fileData)
		if err != nil {
			t.Fatalf("marshal mock cache file %d: %v", i, err)
		}
		filename := filepath.Join(dir, fmt.Sprintf("%03d-mockhash%d.json", i, i))
		if err := os.WriteFile(filename, raw, 0644); err != nil {
			t.Fatalf("write mock cache file %d: %v", i, err)
		}
	}
	return dir
}

func sendMockChatRequest(t *testing.T, ts *httptest.Server, coreStartMS int) (int, map[string]interface{}) {
	t.Helper()
	reqBody := map[string]interface{}{
		"model": "codex/gpt-6.1-sol",
		"messages": []map[string]string{
			{"role": "system", "content": "Align cues."},
			{"role": "user", "content": fmt.Sprintf(`{"core_ms":[%d,%d],"en":[{"id":1,"start":1,"end":2,"text":"test"}],"zh":[{"id":1,"start":1,"end":2,"text":"测试"}]}`, coreStartMS, coreStartMS+210000)},
		},
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("marshal request body: %v", err)
	}

	resp, err := http.Post(ts.URL+"/v1/chat/completions", "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		t.Fatalf("post to mock server: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&result)
	return resp.StatusCode, result
}

func TestReplayServer_TDD_WindowSuccessAndFailure(t *testing.T) {
	cacheDir := createMockCacheDir(t)

	// Window 1 is configured to fail with 503
	server, err := NewReplayServer(cacheDir, 1, 503)
	if err != nil {
		t.Fatalf("NewReplayServer: %v", err)
	}

	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	// Window 0 (core_ms 0..210000) should succeed (200 OK)
	status0, body0 := sendMockChatRequest(t, ts, 0)
	if status0 != http.StatusOK {
		t.Fatalf("expected 200 OK for window 0, got %d", status0)
	}
	choices, ok := body0["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		t.Fatalf("expected choices in response body: %v", body0)
	}
	msg := choices[0].(map[string]interface{})["message"].(map[string]interface{})
	contentStr := msg["content"].(string)
	if !bytes.Contains([]byte(contentStr), []byte(`"en_ids":[1]`)) {
		t.Errorf("unexpected content for window 0: %s", contentStr)
	}

	// Window 1 (core_ms 210000..420000) should fail with 503
	for attempt := 1; attempt <= 3; attempt++ {
		status1, _ := sendMockChatRequest(t, ts, 210000)
		if status1 != http.StatusServiceUnavailable {
			t.Fatalf("attempt %d: expected 503 for window 1, got %d", attempt, status1)
		}
	}

	// Window 2 (core_ms 420000..630000) should succeed (200 OK)
	status2, body2 := sendMockChatRequest(t, ts, 420000)
	if status2 != http.StatusOK {
		t.Fatalf("expected 200 OK for window 2, got %d", status2)
	}
	choices2 := body2["choices"].([]interface{})
	msg2 := choices2[0].(map[string]interface{})["message"].(map[string]interface{})
	contentStr2 := msg2["content"].(string)
	if !bytes.Contains([]byte(contentStr2), []byte(`"en_ids":[3]`)) {
		t.Errorf("unexpected content for window 2: %s", contentStr2)
	}

	// Verify stats
	stats := server.GetStats()
	if stats.TotalRequests != 5 { // 1 for win 0 + 3 for win 1 + 1 for win 2
		t.Errorf("expected 5 total requests, got %d", stats.TotalRequests)
	}
	if stats.WindowAttempts[0] != 1 {
		t.Errorf("expected 1 attempt for window 0, got %d", stats.WindowAttempts[0])
	}
	if stats.WindowAttempts[1] != 3 {
		t.Errorf("expected 3 attempts for window 1, got %d", stats.WindowAttempts[1])
	}
	if stats.WindowAttempts[2] != 1 {
		t.Errorf("expected 1 attempt for window 2, got %d", stats.WindowAttempts[2])
	}
	if stats.FailedAttempts != 3 {
		t.Errorf("expected 3 failed attempts, got %d", stats.FailedAttempts)
	}

	// Verify GET /stats
	respStats, err := http.Get(ts.URL + "/stats")
	if err != nil {
		t.Fatalf("GET /stats: %v", err)
	}
	defer respStats.Body.Close()
	if respStats.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 from /stats, got %d", respStats.StatusCode)
	}
	var httpStats ServerStats
	if err := json.NewDecoder(respStats.Body).Decode(&httpStats); err != nil {
		t.Fatalf("decode /stats: %v", err)
	}
	if httpStats.TotalRequests != 5 || httpStats.FailedAttempts != 3 {
		t.Errorf("unexpected stats from HTTP: %+v", httpStats)
	}
}

func TestReplayServer_TDD_UnknownWindowAndMalformed(t *testing.T) {
	cacheDir := createMockCacheDir(t)
	server, err := NewReplayServer(cacheDir, -1, 503)
	if err != nil {
		t.Fatalf("NewReplayServer: %v", err)
	}

	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	// Window 99 (core_ms 99*210000) not in cache -> 404
	status, _ := sendMockChatRequest(t, ts, 99*210000)
	if status != http.StatusNotFound {
		t.Errorf("expected 404 for unknown window, got %d", status)
	}

	// Malformed JSON -> 400
	resp, err := http.Post(ts.URL+"/v1/chat/completions", "application/json", bytes.NewReader([]byte("{malformed")))
	if err != nil {
		t.Fatalf("post malformed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400 for malformed json, got %d", resp.StatusCode)
	}
}

func TestReplayServer_RealPhaseBCache(t *testing.T) {
	realCacheDir := "/tmp/fusionn-phase-b-cache-gpt61/subtitlellm-v1"
	if _, err := os.Stat(realCacheDir); os.IsNotExist(err) {
		t.Skip("skipping real cache test, dir does not exist")
	}

	server, err := NewReplayServer(realCacheDir, 2, 503)
	if err != nil {
		t.Fatalf("NewReplayServer with real cache: %v", err)
	}

	if len(server.Responses) != 15 {
		t.Fatalf("expected 15 cached responses, got %d", len(server.Responses))
	}

	ts := httptest.NewServer(server.Handler())
	defer ts.Close()

	// Window 0 succeeds
	status0, body0 := sendMockChatRequest(t, ts, 0)
	if status0 != http.StatusOK {
		t.Fatalf("window 0 status: %d", status0)
	}
	choices0 := body0["choices"].([]interface{})
	if len(choices0) == 0 {
		t.Fatalf("empty choices for window 0")
	}

	// Window 2 (core_ms 420000) should fail with 503
	status2, _ := sendMockChatRequest(t, ts, 420000)
	if status2 != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 for window 2, got %d", status2)
	}

	// Window 14 (core_ms 14*210000 = 2940000) succeeds
	status14, _ := sendMockChatRequest(t, ts, 2940000)
	if status14 != http.StatusOK {
		t.Fatalf("window 14 status: %d", status14)
	}

	stats := server.GetStats()
	if stats.TotalRequests != 3 || stats.FailedAttempts != 1 {
		t.Errorf("unexpected stats: %+v", stats)
	}
}
