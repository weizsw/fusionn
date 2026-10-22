package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

type RequestLog struct {
	Timestamp   string `json:"timestamp"`
	WindowIndex int    `json:"window_index"`
	Status      int    `json:"status"`
	Method      string `json:"method"`
	Path        string `json:"path"`
}

type ServerStats struct {
	TotalRequests  int          `json:"total_requests"`
	FailedAttempts int          `json:"failed_attempts"`
	WindowAttempts map[int]int  `json:"window_attempts"`
	Requests       []RequestLog `json:"requests"`
}

type ReplayServer struct {
	Responses       map[int]string // window index -> completion content string
	CoreStartToWin  map[int]int    // core_start_ms -> window index
	FailWindow      int            // window to fail, -1 for none
	FailCode        int            // HTTP status code to return on failure, default 503
	mu              sync.Mutex
	Stats           ServerStats
	sequentialIndex int
}

func NewReplayServer(cacheDir string, failWindow, failCode int) (*ReplayServer, error) {
	if failCode <= 0 {
		failCode = http.StatusServiceUnavailable
	}

	server := &ReplayServer{
		Responses:      make(map[int]string),
		CoreStartToWin: make(map[int]int),
		FailWindow:     failWindow,
		FailCode:       failCode,
		Stats: ServerStats{
			WindowAttempts: make(map[int]int),
			Requests:       make([]RequestLog, 0),
		},
	}

	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return nil, fmt.Errorf("read cache dir %s: %w", cacheDir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(cacheDir, entry.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var data map[string]interface{}
		if err := json.Unmarshal(raw, &data); err != nil {
			continue
		}

		// Determine window index
		winIdx := -1
		if idxVal, ok := data["index"].(float64); ok {
			winIdx = int(idxVal)
		} else {
			// Extract from filename like "001-..."
			parts := strings.Split(entry.Name(), "-")
			if len(parts) > 0 {
				if parsed, err := strconv.Atoi(parts[0]); err == nil {
					winIdx = parsed
				}
			}
		}

		if winIdx < 0 {
			continue
		}

		// Determine completion content
		var contentStr string
		if attempts, ok := data["attempts"].([]interface{}); ok && len(attempts) > 0 {
			if firstAtt, ok := attempts[0].(map[string]interface{}); ok {
				if c, ok := firstAtt["content"].(string); ok && c != "" {
					contentStr = c
				}
			}
		}
		if contentStr == "" {
			if resp, ok := data["response"].(map[string]interface{}); ok {
				respBytes, err := json.Marshal(resp)
				if err == nil {
					contentStr = string(respBytes)
				}
			}
		}

		if contentStr != "" {
			server.Responses[winIdx] = contentStr
			// Standard step is 210,000ms
			server.CoreStartToWin[winIdx*210000] = winIdx
		}
	}

	if len(server.Responses) == 0 {
		return nil, fmt.Errorf("no valid cached responses found in %s", cacheDir)
	}

	return server, nil
}

var coreMSRegex = regexp.MustCompile(`"core_ms"\s*:\s*\[\s*(\d+)`)

func (s *ReplayServer) identifyWindowIndex(bodyBytes []byte) int {
	// Try parsing JSON body to inspect messages
	var req map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &req); err == nil {
		if msgs, ok := req["messages"].([]interface{}); ok {
			for _, m := range msgs {
				if msgMap, ok := m.(map[string]interface{}); ok {
					if content, ok := msgMap["content"].(string); ok {
						match := coreMSRegex.FindStringSubmatch(content)
						if len(match) > 1 {
							if startMS, err := strconv.Atoi(match[1]); err == nil {
								s.mu.Lock()
								win, exists := s.CoreStartToWin[startMS]
								s.mu.Unlock()
								if exists {
									return win
								}
								// Fallback: estimate by 210000ms
								return startMS / 210000
							}
						}
					}
				}
			}
		}
	}

	// Try regex directly on raw body
	match := coreMSRegex.FindSubmatch(bodyBytes)
	if len(match) > 1 {
		if startMS, err := strconv.Atoi(string(match[1])); err == nil {
			s.mu.Lock()
			win, exists := s.CoreStartToWin[startMS]
			s.mu.Unlock()
			if exists {
				return win
			}
			return startMS / 210000
		}
	}

	// Fallback to sequential
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := s.sequentialIndex % len(s.Responses)
	s.sequentialIndex++
	return idx
}

func (s *ReplayServer) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		defer s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(s.Stats)
	})

	chatHandler := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if r.Body == nil {
			http.Error(w, `{"error":{"message":"empty body"}}`, http.StatusBadRequest)
			return
		}
		buf, err := io.ReadAll(r.Body)
		_ = r.Body.Close()
		if err != nil {
			http.Error(w, `{"error":{"message":"cannot read body"}}`, http.StatusBadRequest)
			return
		}

		// Quick check for valid JSON
		var testJSON map[string]interface{}
		if err := json.Unmarshal(buf, &testJSON); err != nil {
			http.Error(w, `{"error":{"message":"invalid json"}}`, http.StatusBadRequest)
			return
		}

		winIdx := s.identifyWindowIndex(buf)

		s.mu.Lock()
		s.Stats.TotalRequests++
		s.Stats.WindowAttempts[winIdx]++
		nowStr := time.Now().UTC().Format(time.RFC3339Nano)

		if winIdx == s.FailWindow {
			s.Stats.FailedAttempts++
			s.Stats.Requests = append(s.Stats.Requests, RequestLog{
				Timestamp:   nowStr,
				WindowIndex: winIdx,
				Status:      s.FailCode,
				Method:      r.Method,
				Path:        r.URL.Path,
			})
			s.mu.Unlock()

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(s.FailCode)
			resp := map[string]interface{}{
				"error": map[string]interface{}{
					"message": fmt.Sprintf("service temporarily unavailable for window %d (simulated degradable error)", winIdx),
					"type":    "server_error",
					"code":    s.FailCode,
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		content, exists := s.Responses[winIdx]
		if !exists {
			s.Stats.Requests = append(s.Stats.Requests, RequestLog{
				Timestamp:   nowStr,
				WindowIndex: winIdx,
				Status:      http.StatusNotFound,
				Method:      r.Method,
				Path:        r.URL.Path,
			})
			s.mu.Unlock()
			http.Error(w, fmt.Sprintf(`{"error":{"message":"window %d not found in recorded cache"}}`, winIdx), http.StatusNotFound)
			return
		}

		s.Stats.Requests = append(s.Stats.Requests, RequestLog{
			Timestamp:   nowStr,
			WindowIndex: winIdx,
			Status:      http.StatusOK,
			Method:      r.Method,
			Path:        r.URL.Path,
		})
		s.mu.Unlock()

		respObj := map[string]interface{}{
			"id":      fmt.Sprintf("chatcmpl-mock-win%d", winIdx),
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   "codex/gpt-6.1-sol",
			"choices": []map[string]interface{}{
				{
					"index": 0,
					"message": map[string]interface{}{
						"role":    "assistant",
						"content": content,
					},
					"finish_reason": "stop",
				},
			},
			"usage": map[string]interface{}{
				"prompt_tokens":     150,
				"completion_tokens": 100,
				"total_tokens":      250,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(respObj)
	}

	mux.HandleFunc("/v1/chat/completions", chatHandler)
	mux.HandleFunc("/chat/completions", chatHandler)

	return mux
}

func (s *ReplayServer) GetStats() ServerStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	res := ServerStats{
		TotalRequests:  s.Stats.TotalRequests,
		FailedAttempts: s.Stats.FailedAttempts,
		WindowAttempts: make(map[int]int),
		Requests:       make([]RequestLog, len(s.Stats.Requests)),
	}
	for k, v := range s.Stats.WindowAttempts {
		res.WindowAttempts[k] = v
	}
	copy(res.Requests, s.Stats.Requests)
	return res
}
