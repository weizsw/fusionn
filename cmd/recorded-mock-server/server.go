package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
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

type RecordedWindow struct {
	Index   int
	Core    [2]int
	Model   string
	Content string
	Usage   json.RawMessage
}

type ReplayServer struct {
	Windows        map[[2]int]*RecordedWindow // [start_ms, end_ms] -> window
	WindowsByIndex map[int]*RecordedWindow
	FailWindow     int // window index to fail, -1 for none
	FailCode       int // HTTP status code to return on failure, default 503
	mu             sync.Mutex
	Stats          ServerStats
}

type rawCacheRecord struct {
	Index    *int            `json:"index"`
	Request  json.RawMessage `json:"request"`
	Attempts []struct {
		Seconds      float64         `json:"seconds"`
		Model        string          `json:"model"`
		Usage        json.RawMessage `json:"usage"`
		FinishReason string          `json:"finish_reason"`
		Content      string          `json:"content"`
		Error        string          `json:"error"`
	} `json:"attempts"`
	Response json.RawMessage `json:"response"`
}

func parseCoreMS(rawRequest json.RawMessage) ([2]int, string, error) {
	var req struct {
		Model    string `json:"model"`
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(rawRequest, &req); err != nil {
		return [2]int{}, "", fmt.Errorf("unmarshal request: %w", err)
	}
	for _, m := range req.Messages {
		if m.Role == "user" {
			var userContent struct {
				CoreMS []int `json:"core_ms"`
			}
			if err := json.Unmarshal([]byte(m.Content), &userContent); err == nil && len(userContent.CoreMS) == 2 {
				return [2]int{userContent.CoreMS[0], userContent.CoreMS[1]}, req.Model, nil
			}
		}
	}
	return [2]int{}, "", fmt.Errorf("could not extract core_ms from user message")
}

// isValidAlignmentContent validates that content is valid JSON with a top-level {alignments: [...]} array.
func isValidAlignmentContent(content string) bool {
	if strings.TrimSpace(content) == "" {
		return false
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(content), &obj); err != nil {
		return false
	}
	alignments, ok := obj["alignments"]
	if !ok || alignments == nil {
		return false
	}
	_, isArray := alignments.([]any)
	return isArray
}

// NewReplayServer loads recorded cache files and initializes the replay server.
// Fails fast on missing directory, corrupt JSON, missing core_ms, or files without valid content.
func NewReplayServer(cacheDir string, failWindow, failCode int) (*ReplayServer, error) {
	if failCode <= 0 {
		failCode = http.StatusServiceUnavailable
	}

	server := &ReplayServer{
		Windows:        make(map[[2]int]*RecordedWindow),
		WindowsByIndex: make(map[int]*RecordedWindow),
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

	foundJSON := false
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		foundJSON = true
		path := filepath.Join(cacheDir, entry.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read cache file %s: %w", entry.Name(), err)
		}

		var record rawCacheRecord
		if err := json.Unmarshal(raw, &record); err != nil {
			return nil, fmt.Errorf("unmarshal cache file %s: %w", entry.Name(), err)
		}

		// Determine window index
		winIdx := -1
		if record.Index != nil {
			winIdx = *record.Index
		} else {
			parts := strings.Split(entry.Name(), "-")
			if len(parts) > 0 {
				if parsed, err := strconv.Atoi(parts[0]); err == nil {
					winIdx = parsed
				}
			}
		}
		if winIdx < 0 {
			return nil, fmt.Errorf("cache file %s has no valid window index", entry.Name())
		}

		// Extract core_ms and request model
		coreMS, reqModel, err := parseCoreMS(record.Request)
		if err != nil {
			return nil, fmt.Errorf("cache file %s: %w", entry.Name(), err)
		}

		// Determine successful attempt content, model, and usage.
		// Select the final successful attempt conforming to the alignment protocol.
		var chosenModel string
		var chosenContent string
		var chosenUsage json.RawMessage

		for i := len(record.Attempts) - 1; i >= 0; i-- {
			att := record.Attempts[i]
			if att.Error == "" && att.FinishReason == "stop" && isValidAlignmentContent(att.Content) {
				chosenModel = att.Model
				chosenContent = att.Content
				chosenUsage = att.Usage
				break
			}
		}

		if chosenContent == "" && len(record.Response) > 0 && isValidAlignmentContent(string(record.Response)) {
			chosenContent = string(record.Response)
		}

		if chosenContent == "" {
			return nil, fmt.Errorf("cache file %s has no valid successful attempt conforming to alignment protocol", entry.Name())
		}

		if chosenModel == "" {
			chosenModel = reqModel
		}

		win := &RecordedWindow{
			Index:   winIdx,
			Core:    coreMS,
			Model:   chosenModel,
			Content: chosenContent,
			Usage:   chosenUsage,
		}

		server.Windows[coreMS] = win
		server.WindowsByIndex[winIdx] = win
	}

	if !foundJSON || len(server.Windows) == 0 {
		return nil, fmt.Errorf("no valid cache records found in %s", cacheDir)
	}

	return server, nil
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
		if err != nil || len(buf) == 0 {
			http.Error(w, `{"error":{"message":"empty or unreadable body"}}`, http.StatusBadRequest)
			return
		}

		var req struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.Unmarshal(buf, &req); err != nil {
			http.Error(w, `{"error":{"message":"invalid request json"}}`, http.StatusBadRequest)
			return
		}

		var reqCore [2]int
		foundCore := false
		for _, m := range req.Messages {
			if m.Role == "user" {
				var userContent struct {
					CoreMS []int `json:"core_ms"`
				}
				if err := json.Unmarshal([]byte(m.Content), &userContent); err == nil && len(userContent.CoreMS) == 2 {
					reqCore = [2]int{userContent.CoreMS[0], userContent.CoreMS[1]}
					foundCore = true
					break
				}
			}
		}

		if !foundCore {
			http.Error(w, `{"error":{"message":"request missing user message or valid core_ms"}}`, http.StatusBadRequest)
			return
		}

		// Strict identity match against loaded windows: NO division, NO sequential guessing!
		s.mu.Lock()
		win, exists := s.Windows[reqCore]
		if !exists {
			s.mu.Unlock()
			http.Error(w, fmt.Sprintf(`{"error":{"message":"unknown window core_ms: [%d, %d]"}}`, reqCore[0], reqCore[1]), http.StatusBadRequest)
			return
		}

		s.Stats.TotalRequests++
		s.Stats.WindowAttempts[win.Index]++
		nowStr := time.Now().UTC().Format(time.RFC3339Nano)

		if win.Index == s.FailWindow {
			s.Stats.FailedAttempts++
			s.Stats.Requests = append(s.Stats.Requests, RequestLog{
				Timestamp:   nowStr,
				WindowIndex: win.Index,
				Status:      s.FailCode,
				Method:      r.Method,
				Path:        r.URL.Path,
			})
			s.mu.Unlock()

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(s.FailCode)
			resp := map[string]any{
				"error": map[string]any{
					"message": fmt.Sprintf("service temporarily unavailable for window %d (simulated degradable error)", win.Index),
					"type":    "server_error",
					"code":    s.FailCode,
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
			return
		}

		s.Stats.Requests = append(s.Stats.Requests, RequestLog{
			Timestamp:   nowStr,
			WindowIndex: win.Index,
			Status:      http.StatusOK,
			Method:      r.Method,
			Path:        r.URL.Path,
		})
		s.mu.Unlock()

		respObj := map[string]any{
			"id":      fmt.Sprintf("chatcmpl-mock-win%d", win.Index),
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   win.Model,
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]any{
						"role":    "assistant",
						"content": win.Content,
					},
					"finish_reason": "stop",
				},
			},
		}
		if len(win.Usage) > 0 {
			respObj["usage"] = win.Usage
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
