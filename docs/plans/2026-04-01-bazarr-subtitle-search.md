# Bazarr Subtitle Search Integration — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Search Bazarr for human-translated Chinese subtitles before falling back to machine translation.

**Architecture:** A new `BazarrSearchProcessor` is inserted into the existing analyze pipeline (after SDHFilter, before TranslationQueue). It calls Bazarr's synchronous REST API to trigger a subtitle search, then checks the result. A thin HTTP client in `internal/client/bazarr/` encapsulates the API calls. The `ProcessMedia` method switches to a `MediaParams` struct to carry Sonarr/Radarr IDs through the pipeline.

**Tech Stack:** Go 1.23, net/http, gin, testify, httptest

**Spec:** `docs/superpowers/specs/2026-04-01-bazarr-subtitle-search-design.md`

---

## Task 1: Add Bazarr Configuration

**Files:**
- Modify: `internal/config/config.go:17-23` (add BazarrConfig to Config struct)
- Modify: `internal/config/config.go:90-98` (add BazarrConfig struct definition)
- Modify: `config/config.example.yaml` (add bazarr section)
- Test: `internal/config/config_test.go`

**Step 1: Write the failing test**

Add a test case to `internal/config/config_test.go`:

```go
func TestBazarrConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
server:
  port: 8080
bazarr:
  enabled: true
  url: "http://bazarr:6767"
  api_key: "test-api-key"
  search_timeout: 180
  language_code: "zh"
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if !cfg.Bazarr.Enabled {
		t.Error("Expected bazarr.enabled to be true")
	}
	if cfg.Bazarr.URL != "http://bazarr:6767" {
		t.Errorf("Expected url 'http://bazarr:6767', got %s", cfg.Bazarr.URL)
	}
	if cfg.Bazarr.APIKey != "test-api-key" {
		t.Errorf("Expected api_key 'test-api-key', got %s", cfg.Bazarr.APIKey)
	}
	if cfg.Bazarr.SearchTimeout != 180 {
		t.Errorf("Expected search_timeout 180, got %d", cfg.Bazarr.SearchTimeout)
	}
	if cfg.Bazarr.LanguageCode != "zh" {
		t.Errorf("Expected language_code 'zh', got %s", cfg.Bazarr.LanguageCode)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestBazarrConfig -v`
Expected: FAIL — `cfg.Bazarr` does not exist

**Step 3: Write minimal implementation**

In `internal/config/config.go`, add the `BazarrConfig` struct after `QueueConfig` (after line 113):

```go
type BazarrConfig struct {
	Enabled       bool   `mapstructure:"enabled"`
	URL           string `mapstructure:"url"`
	APIKey        string `mapstructure:"api_key"`
	SearchTimeout int    `mapstructure:"search_timeout"`
	LanguageCode  string `mapstructure:"language_code"`
}
```

Add `Bazarr` field to the `Config` struct (line 22):

```go
type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	Subtitle SubtitleConfig `mapstructure:"subtitle"`
	Redis    RedisConfig    `mapstructure:"redis"`
	Apprise  AppriseConfig  `mapstructure:"apprise"`
	Queue    QueueConfig    `mapstructure:"queue"`
	Bazarr   BazarrConfig   `mapstructure:"bazarr"`
}
```

Add bazarr section to `config/config.example.yaml` (after the `queue` section, before the Docker section):

```yaml
# Bazarr integration (search for Chinese subtitles before translating)
bazarr:
  enabled: false
  url: "http://bazarr:6767"
  api_key: ""
  search_timeout: 180   # seconds — max time for Bazarr to search providers
  language_code: "zh"   # Bazarr language code for Chinese subtitles
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/config/ -run TestBazarrConfig -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/config/config.go internal/config/config_test.go config/config.example.yaml
git commit -m "feat: add Bazarr configuration struct and example config"
```

---

## Task 2: Add Sonarr/Radarr IDs to Webhook Payloads

**Files:**
- Modify: `internal/handler/webhook.go:35-45` (add ID fields to SeriesInfo, EpisodeInfo, MovieInfo)
- Test: `internal/handler/webhook_test.go`

**Step 1: Write the failing test**

Update the existing "Valid Download Event" test cases in `internal/handler/webhook_test.go` to include IDs in the payloads. Add a new dedicated test:

```go
func TestSonarrPayloadParseIDs(t *testing.T) {
	handler := NewWebhookHandler(nil)

	payload := SonarrPayload{
		EventType: "Download",
		Series: SeriesInfo{
			ID:    42,
			Title: "Test Series",
			Path:  "/tv/Test Series",
		},
		Episodes: []EpisodeInfo{
			{
				ID:            101,
				Title:         "Test Episode",
				EpisodeNumber: 1,
				SeasonNumber:  1,
			},
		},
		EpisodeFile: EpisodeFile{
			Path: "/tv/Test Series/Season 01/S01E01.mkv",
		},
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body, _ := json.Marshal(payload)
	c.Request = httptest.NewRequest("POST", "/api/v1/webhook/sonarr", bytes.NewBuffer(body))
	c.Request.Header.Set("Content-Type", "application/json")

	handler.HandleSonarr(c)

	if w.Code != http.StatusAccepted {
		t.Errorf("Expected status %d, got %d", http.StatusAccepted, w.Code)
	}
}

func TestRadarrPayloadParseIDs(t *testing.T) {
	handler := NewWebhookHandler(nil)

	payload := RadarrPayload{
		EventType: "Download",
		Movie: MovieInfo{
			ID:     99,
			Title:  "Test Movie",
			Year:   2024,
			ImdbID: "tt1234567",
		},
		MovieFile: MovieFile{
			Path: "/movies/Test Movie (2024)/Movie.mkv",
		},
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body, _ := json.Marshal(payload)
	c.Request = httptest.NewRequest("POST", "/api/v1/webhook/radarr", bytes.NewBuffer(body))
	c.Request.Header.Set("Content-Type", "application/json")

	handler.HandleRadarr(c)

	if w.Code != http.StatusAccepted {
		t.Errorf("Expected status %d, got %d", http.StatusAccepted, w.Code)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/handler/ -run "TestSonarrPayloadParseIDs|TestRadarrPayloadParseIDs" -v`
Expected: FAIL — `ID` field does not exist on structs

**Step 3: Write minimal implementation**

In `internal/handler/webhook.go`, add `ID` fields:

```go
type SeriesInfo struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
	Path  string `json:"path"`
}

type EpisodeInfo struct {
	ID            int    `json:"id"`
	Title         string `json:"title"`
	EpisodeNumber int    `json:"episodeNumber"`
	SeasonNumber  int    `json:"seasonNumber"`
}

type MovieInfo struct {
	ID     int    `json:"id"`
	Title  string `json:"title"`
	Year   int    `json:"year"`
	ImdbID string `json:"imdbId"`
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/handler/ -v`
Expected: PASS (all existing tests still pass, new tests pass)

**Step 5: Commit**

```bash
git add internal/handler/webhook.go internal/handler/webhook_test.go
git commit -m "feat: parse Sonarr/Radarr IDs from webhook payloads"
```

---

## Task 3: Add MediaParams and IDs to ProcessingContext

**Files:**
- Modify: `internal/service/subtitle/pipeline.go:10-32` (add ID fields to ProcessingContext)
- Modify: `internal/service/subtitle/service.go:89-136` (change ProcessMedia to accept MediaParams)
- Modify: `internal/handler/webhook.go:107-119,160-173` (update callers)

**Step 1: Add fields to ProcessingContext**

In `internal/service/subtitle/pipeline.go`, add to `ProcessingContext`:

```go
type ProcessingContext struct {
	// Input
	VideoPath  string
	MediaType  string // "movie" or "episode"
	MediaTitle string
	JobID      string

	// Sonarr/Radarr IDs for Bazarr API
	SonarrSeriesID  int
	SonarrEpisodeID int
	RadarrID        int

	// Analysis results
	Analysis *AnalysisResult

	// Processing results
	EnglishSubPath string
	ChineseSubPath string
	MergedSubPath  string

	// Flags
	NeedsConversion  bool
	NeedsTranslation bool

	// Metadata for notifications/logging
	Metadata map[string]interface{}
}
```

**Step 2: Create MediaParams and update ProcessMedia**

In `internal/service/subtitle/service.go`, add the `MediaParams` struct and update `ProcessMedia`:

```go
type MediaParams struct {
	Path            string
	MediaType       string
	Title           string
	SonarrSeriesID  int
	SonarrEpisodeID int
	RadarrID        int
}

func (s *Service) ProcessMedia(ctx context.Context, params MediaParams) error {
	jobID := uuid.New().String()

	pctx := &ProcessingContext{
		VideoPath:       params.Path,
		MediaType:       params.MediaType,
		MediaTitle:      params.Title,
		JobID:           jobID,
		SonarrSeriesID:  params.SonarrSeriesID,
		SonarrEpisodeID: params.SonarrEpisodeID,
		RadarrID:        params.RadarrID,
		Metadata:        make(map[string]interface{}),
	}

	logger.Infof("📝 Analyzing subtitles for %s (job: %s)", params.Title, jobID)

	if err := s.analyzePipeline.Execute(ctx, pctx); err != nil {
		logger.Errorf("Analyze pipeline failed (job: %s): %v", jobID, err)
		return err
	}

	if pctx.EnglishSubPath != "" && pctx.ChineseSubPath != "" {
		mergeJob := &queue.MergeJob{
			JobID:       jobID,
			VideoPath:   params.Path,
			EnglishPath: pctx.EnglishSubPath,
			ChinesePath: pctx.ChineseSubPath,
			MediaTitle:  params.Title,
			MediaType:   params.MediaType,
		}

		if err := s.mergeQueue.Enqueue(mergeJob); err != nil {
			return err
		}

		logger.Infof("✅ Analysis complete, merge job enqueued (job: %s)", jobID)
	} else {
		logger.Infof("✅ Analysis complete (job: %s)", jobID)
	}

	return nil
}
```

**Step 3: Update webhook handler callers**

In `internal/handler/webhook.go`, update `HandleSonarr` (the goroutine at ~line 108):

```go
go func() {
	ctx := context.Background()
	sonarrEpisodeID := 0
	if len(payload.Episodes) > 0 {
		sonarrEpisodeID = payload.Episodes[0].ID
	}
	if err := h.subtitleService.ProcessMedia(
		ctx,
		subtitle.MediaParams{
			Path:            payload.EpisodeFile.Path,
			MediaType:       "episode",
			Title:           mediaTitle,
			SonarrSeriesID:  payload.Series.ID,
			SonarrEpisodeID: sonarrEpisodeID,
		},
	); err != nil {
		logger.Errorf("Subtitle processing failed for %s: %v", mediaTitle, err)
	}
}()
```

Update `HandleRadarr` (the goroutine at ~line 161):

```go
go func() {
	ctx := context.Background()
	if err := h.subtitleService.ProcessMedia(
		ctx,
		subtitle.MediaParams{
			Path:      payload.MovieFile.Path,
			MediaType: "movie",
			Title:     mediaTitle,
			RadarrID:  payload.Movie.ID,
		},
	); err != nil {
		logger.Errorf("Subtitle processing failed for %s: %v", mediaTitle, err)
	}
}()
```

**Step 4: Run all tests to verify nothing breaks**

Run: `go test ./... -v`
Expected: PASS — all existing tests still pass (webhook tests use `nil` subtitleService so ProcessMedia is never called)

**Step 5: Commit**

```bash
git add internal/service/subtitle/pipeline.go internal/service/subtitle/service.go internal/handler/webhook.go
git commit -m "feat: add MediaParams struct and Sonarr/Radarr IDs to ProcessingContext"
```

---

## Task 4: Create Bazarr HTTP Client

**Files:**
- Create: `internal/client/bazarr/client.go`
- Create: `internal/client/bazarr/client_test.go`

**Step 1: Write the failing test**

Create `internal/client/bazarr/client_test.go`:

```go
package bazarr

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSearchEpisodeSubtitle(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.Method != http.MethodPatch {
			t.Errorf("Expected PATCH, got %s", r.Method)
		}
		if r.URL.Path != "/api/episodes/subtitles" {
			t.Errorf("Expected /api/episodes/subtitles, got %s", r.URL.Path)
		}
		if r.Header.Get("X-API-KEY") != "test-key" {
			t.Errorf("Expected API key 'test-key', got %s", r.Header.Get("X-API-KEY"))
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.FormValue("seriesid") != "42" {
			t.Errorf("Expected seriesid=42, got %s", r.FormValue("seriesid"))
		}
		if r.FormValue("episodeid") != "101" {
			t.Errorf("Expected episodeid=101, got %s", r.FormValue("episodeid"))
		}
		if r.FormValue("language") != "zh" {
			t.Errorf("Expected language=zh, got %s", r.FormValue("language"))
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key", 30)
	err := client.SearchEpisodeSubtitle(context.Background(), 42, 101, "zh")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !called {
		t.Error("Server was not called")
	}
}

func TestSearchMovieSubtitle(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("Expected PATCH, got %s", r.Method)
		}
		if r.URL.Path != "/api/movies/subtitles" {
			t.Errorf("Expected /api/movies/subtitles, got %s", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.FormValue("radarrid") != "99" {
			t.Errorf("Expected radarrid=99, got %s", r.FormValue("radarrid"))
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key", 30)
	err := client.SearchMovieSubtitle(context.Background(), 99, "zh")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
}

func TestGetEpisodeSubtitles(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("Expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/episodes" {
			t.Errorf("Expected /api/episodes, got %s", r.URL.Path)
		}
		if r.URL.Query().Get("episodeid[]") != "101" {
			t.Errorf("Expected episodeid[]=101, got %s", r.URL.Query().Get("episodeid[]"))
		}
		resp := EpisodeListResponse{
			Data: []EpisodeData{
				{
					SonarrEpisodeID: 101,
					Subtitles: []SubtitleInfo{
						{Path: "/tv/Show/S01E01.zh.srt", Code2: "zh"},
					},
					MissingSubtitles: []MissingLanguage{},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key", 30)
	info, err := client.GetEpisodeSubtitles(context.Background(), 101)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(info.Subtitles) != 1 {
		t.Fatalf("Expected 1 subtitle, got %d", len(info.Subtitles))
	}
	if info.Subtitles[0].Code2 != "zh" {
		t.Errorf("Expected code2=zh, got %s", info.Subtitles[0].Code2)
	}
}

func TestGetMovieSubtitles(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("radarrid[]") != "99" {
			t.Errorf("Expected radarrid[]=99, got %s", r.URL.Query().Get("radarrid[]"))
		}
		resp := MovieListResponse{
			Data: []MovieData{
				{
					RadarrID: 99,
					Subtitles: []SubtitleInfo{
						{Path: "/movies/Movie/Movie.zh.srt", Code2: "zh"},
					},
					MissingSubtitles: []MissingLanguage{},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key", 30)
	info, err := client.GetMovieSubtitles(context.Background(), 99)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(info.Subtitles) != 1 {
		t.Fatalf("Expected 1 subtitle, got %d", len(info.Subtitles))
	}
}

func TestSearchEpisodeSubtitle_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key", 30)
	err := client.SearchEpisodeSubtitle(context.Background(), 42, 101, "zh")
	if err == nil {
		t.Error("Expected error for 500 response")
	}
}

func TestSearchEpisodeSubtitle_Unreachable(t *testing.T) {
	client := NewClient("http://127.0.0.1:1", "test-key", 2)
	err := client.SearchEpisodeSubtitle(context.Background(), 42, 101, "zh")
	if err == nil {
		t.Error("Expected error for unreachable server")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/client/bazarr/ -v`
Expected: FAIL — package does not exist

**Step 3: Write minimal implementation**

Create `internal/client/bazarr/client.go`:

```go
package bazarr

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

type SubtitleInfo struct {
	Path  string `json:"path"`
	Code2 string `json:"code2"`
	Name  string `json:"name"`
}

type MissingLanguage struct {
	Code2 string `json:"code2"`
	Name  string `json:"name"`
}

type EpisodeData struct {
	SonarrEpisodeID  int               `json:"sonarrEpisodeId"`
	SonarrSeriesID   int               `json:"sonarrSeriesId"`
	Path             string            `json:"path"`
	Subtitles        []SubtitleInfo    `json:"subtitles"`
	MissingSubtitles []MissingLanguage `json:"missing_subtitles"`
}

type EpisodeListResponse struct {
	Data []EpisodeData `json:"data"`
}

type MovieData struct {
	RadarrID         int               `json:"radarrId"`
	Path             string            `json:"path"`
	Subtitles        []SubtitleInfo    `json:"subtitles"`
	MissingSubtitles []MissingLanguage `json:"missing_subtitles"`
}

type MovieListResponse struct {
	Data  []MovieData `json:"data"`
	Total int         `json:"total"`
}

func NewClient(baseURL, apiKey string, timeoutSeconds int) *Client {
	if timeoutSeconds <= 0 {
		timeoutSeconds = 180
	}
	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: time.Duration(timeoutSeconds) * time.Second,
		},
	}
}

func (c *Client) SearchEpisodeSubtitle(ctx context.Context, seriesID, episodeID int, language string) error {
	form := url.Values{}
	form.Set("seriesid", strconv.Itoa(seriesID))
	form.Set("episodeid", strconv.Itoa(episodeID))
	form.Set("language", language)
	form.Set("hi", "False")
	form.Set("forced", "False")

	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, c.baseURL+"/api/episodes/subtitles?"+form.Encode(), nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("X-API-KEY", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("bazarr episode search request failed: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bazarr episode search returned status %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) SearchMovieSubtitle(ctx context.Context, radarrID int, language string) error {
	form := url.Values{}
	form.Set("radarrid", strconv.Itoa(radarrID))
	form.Set("language", language)
	form.Set("hi", "False")
	form.Set("forced", "False")

	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, c.baseURL+"/api/movies/subtitles?"+form.Encode(), nil)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("X-API-KEY", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("bazarr movie search request failed: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bazarr movie search returned status %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) GetEpisodeSubtitles(ctx context.Context, episodeID int) (*EpisodeData, error) {
	reqURL := fmt.Sprintf("%s/api/episodes?episodeid[]=%d", c.baseURL, episodeID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("X-API-KEY", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bazarr get episode failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("bazarr get episode returned status %d", resp.StatusCode)
	}

	var listResp EpisodeListResponse
	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		return nil, fmt.Errorf("decode episode response: %w", err)
	}

	if len(listResp.Data) == 0 {
		return nil, fmt.Errorf("episode %d not found in bazarr", episodeID)
	}
	return &listResp.Data[0], nil
}

func (c *Client) GetMovieSubtitles(ctx context.Context, radarrID int) (*MovieData, error) {
	reqURL := fmt.Sprintf("%s/api/movies?radarrid[]=%d", c.baseURL, radarrID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("X-API-KEY", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bazarr get movie failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("bazarr get movie returned status %d", resp.StatusCode)
	}

	var listResp MovieListResponse
	if err := json.NewDecoder(resp.Body).Decode(&listResp); err != nil {
		return nil, fmt.Errorf("decode movie response: %w", err)
	}

	if len(listResp.Data) == 0 {
		return nil, fmt.Errorf("movie (radarr_id=%d) not found in bazarr", radarrID)
	}
	return &listResp.Data[0], nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/client/bazarr/ -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/client/bazarr/
git commit -m "feat: add Bazarr HTTP client with search and query methods"
```

---

## Task 5: Create BazarrSearchProcessor

**Files:**
- Create: `internal/service/subtitle/processor_bazarr_search.go`
- Modify: `internal/service/subtitle/pipeline.go:52-64` (add emoji entry)

**Step 1: Write the processor**

Create `internal/service/subtitle/processor_bazarr_search.go`:

```go
package subtitle

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/fusionn/internal/client/bazarr"
	"github.com/fusionn/pkg/logger"
)

type BazarrSearchProcessor struct {
	client       *bazarr.Client
	languageCode string
}

func NewBazarrSearchProcessor(client *bazarr.Client, languageCode string) *BazarrSearchProcessor {
	return &BazarrSearchProcessor{
		client:       client,
		languageCode: languageCode,
	}
}

func (p *BazarrSearchProcessor) Name() string {
	return "BazarrSearch"
}

func (p *BazarrSearchProcessor) ShouldRun(pctx *ProcessingContext) bool {
	if p.client == nil {
		return false
	}
	if pctx.ChineseSubPath != "" {
		return false
	}
	if pctx.Analysis == nil || pctx.Analysis.EnglishTrack == nil {
		return false
	}
	if pctx.Analysis.ChineseTrack != nil {
		return false
	}
	if pctx.MediaType == "episode" && pctx.SonarrEpisodeID == 0 {
		return false
	}
	if pctx.MediaType == "movie" && pctx.RadarrID == 0 {
		return false
	}
	return true
}

func (p *BazarrSearchProcessor) Process(ctx context.Context, pctx *ProcessingContext) error {
	log := logger.Indent()
	log.Info("Searching Bazarr for Chinese subtitle...")

	var err error
	switch pctx.MediaType {
	case "episode":
		err = p.searchEpisode(ctx, pctx, log)
	case "movie":
		err = p.searchMovie(ctx, pctx, log)
	default:
		log.Warnf("Unknown media type %q, skipping Bazarr search", pctx.MediaType)
		return nil
	}

	if err != nil {
		log.Warnf("Bazarr search failed (will fall back to translation): %v", err)
	}
	return nil
}

func (p *BazarrSearchProcessor) searchEpisode(ctx context.Context, pctx *ProcessingContext, log logger.IndentedLogger) error {
	if err := p.client.SearchEpisodeSubtitle(ctx, pctx.SonarrSeriesID, pctx.SonarrEpisodeID, p.languageCode); err != nil {
		return err
	}

	info, err := p.client.GetEpisodeSubtitles(ctx, pctx.SonarrEpisodeID)
	if err != nil {
		return err
	}

	return p.checkAndSetSubtitle(pctx, info.Subtitles, info.MissingSubtitles, log)
}

func (p *BazarrSearchProcessor) searchMovie(ctx context.Context, pctx *ProcessingContext, log logger.IndentedLogger) error {
	if err := p.client.SearchMovieSubtitle(ctx, pctx.RadarrID, p.languageCode); err != nil {
		return err
	}

	info, err := p.client.GetMovieSubtitles(ctx, pctx.RadarrID)
	if err != nil {
		return err
	}

	return p.checkAndSetSubtitle(pctx, info.Subtitles, info.MissingSubtitles, log)
}

func (p *BazarrSearchProcessor) checkAndSetSubtitle(pctx *ProcessingContext, subtitles []bazarr.SubtitleInfo, missing []bazarr.MissingLanguage, log logger.IndentedLogger) error {
	for _, m := range missing {
		if m.Code2 == p.languageCode {
			log.Info("Bazarr did not find a Chinese subtitle")
			return nil
		}
	}

	subPath := p.findChineseSidecar(pctx.VideoPath)
	if subPath != "" {
		log.Infof("Found Chinese subtitle from Bazarr: %s", subPath)
		pctx.ChineseSubPath = subPath
		return nil
	}

	for _, s := range subtitles {
		if s.Code2 == p.languageCode && s.Path != "" {
			if _, err := os.Stat(s.Path); err == nil {
				log.Infof("Found Chinese subtitle from Bazarr API path: %s", s.Path)
				pctx.ChineseSubPath = s.Path
				return nil
			}
		}
	}

	log.Info("Bazarr search completed but subtitle file not found locally")
	return nil
}

func (p *BazarrSearchProcessor) findChineseSidecar(videoPath string) string {
	dir := filepath.Dir(videoPath)
	base := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))

	chinesePatterns := []string{
		base + ".zh.srt",
		base + ".chi.srt",
		base + ".zho.srt",
		base + ".zh-CN.srt",
		base + ".zh-Hans.srt",
		base + ".zh-TW.srt",
		base + ".zh-Hant.srt",
	}

	for _, pattern := range chinesePatterns {
		candidate := filepath.Join(dir, pattern)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}
```

**Step 2: Add emoji to pipeline.go**

In `internal/service/subtitle/pipeline.go`, add `"BazarrSearch"` to the `processorEmojis` map:

```go
var processorEmojis = map[string]string{
	"AnalyzerProcessor": "🔍",
	"Extractor":         "📤",
	"Conversion":        "🔄",
	"Merger":            "🔀",
	"Style":             "🎨",
	"FontEmbedding":     "🔤",
	"Output":            "💾",
	"Notification":      "📢",
	"Cleanup":           "🧹",
	"SDHFilter":         "🔇",
	"TranslationQueue":  "🌐",
	"BazarrSearch":      "🔎",
}
```

**Step 3: Run tests**

Run: `go build ./...`
Expected: Compiles cleanly

**Step 4: Commit**

```bash
git add internal/service/subtitle/processor_bazarr_search.go internal/service/subtitle/pipeline.go
git commit -m "feat: add BazarrSearchProcessor for Chinese subtitle search"
```

---

## Task 6: Fix TranslationQueueProcessor ShouldRun Guard

**Files:**
- Modify: `internal/service/subtitle/processor_translation_queue.go:34-39`

**Step 1: Update the ShouldRun condition**

In `internal/service/subtitle/processor_translation_queue.go`, change `ShouldRun` to also check `ChineseSubPath`:

```go
func (p *TranslationQueueProcessor) ShouldRun(pctx *ProcessingContext) bool {
	return pctx.Analysis != nil &&
		pctx.Analysis.EnglishTrack != nil &&
		pctx.Analysis.ChineseTrack == nil &&
		pctx.ChineseSubPath == ""
}
```

**Step 2: Run tests**

Run: `go test ./... -v`
Expected: PASS

**Step 3: Commit**

```bash
git add internal/service/subtitle/processor_translation_queue.go
git commit -m "fix: skip translation queue when Bazarr already provided Chinese subtitle"
```

---

## Task 7: Wire Bazarr into Service and Main

**Files:**
- Modify: `internal/service/subtitle/service.go:25-49` (accept Bazarr client, insert processor)
- Modify: `cmd/fusionn/main.go:44-96` (create Bazarr client, pass to service)

**Step 1: Update NewService to accept Bazarr client**

In `internal/service/subtitle/service.go`, change `NewService` signature and pipeline wiring:

```go
func NewService(cfg *config.Config, redisClient QueueClient, mergeQueue *queue.MergeQueue, bazarrClient *bazarr.Client) (*Service, error) {
```

Add the import for `bazarr`:
```go
import (
	"github.com/fusionn/internal/client/bazarr"
)
```

Insert `BazarrSearchProcessor` into the analyze pipeline (after `NewSDHFilterProcessor()`, before `NewTranslationQueueProcessor()`):

```go
	analyzePipeline.AddProcessor(NewAnalyzerProcessor(analyzer))
	analyzePipeline.AddProcessor(NewExtractorProcessor(analyzer))
	analyzePipeline.AddProcessor(NewSDHFilterProcessor())
	if bazarrClient != nil {
		analyzePipeline.AddProcessor(NewBazarrSearchProcessor(bazarrClient, cfg.Bazarr.LanguageCode))
	}
	analyzePipeline.AddProcessor(NewTranslationQueueProcessor(redisClient, ""))
```

**Step 2: Update main.go to create Bazarr client**

In `cmd/fusionn/main.go`, add Bazarr client creation after the Redis client setup (after line 53), and add the import:

```go
import (
	"github.com/fusionn/internal/client/bazarr"
)
```

Create the client:
```go
	var bazarrClient *bazarr.Client
	if cfg.Bazarr.Enabled {
		bazarrClient = bazarr.NewClient(cfg.Bazarr.URL, cfg.Bazarr.APIKey, cfg.Bazarr.SearchTimeout)
		logger.Infof("✅ Bazarr integration enabled: %s", cfg.Bazarr.URL)
	}
```

Update both `subtitle.NewService` calls to pass `bazarrClient`:
```go
	subtitleService, err = subtitle.NewService(cfg, redisQueueClient, mergeQueue, bazarrClient)
```

(There are two calls to `NewService` in main.go — update both at lines ~70 and ~86.)

**Step 3: Run build**

Run: `go build ./...`
Expected: Compiles cleanly

**Step 4: Run all tests**

Run: `go test ./... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/service/subtitle/service.go cmd/fusionn/main.go
git commit -m "feat: wire Bazarr client into subtitle service and pipeline"
```

---

## Task 8: Check IndentedLogger Type Compatibility

**Files:**
- Check: `pkg/logger/logger.go`

**Step 1: Verify IndentedLogger type**

The `BazarrSearchProcessor` uses `logger.IndentedLogger` as a parameter type. Verify that `logger.Indent()` returns this type and that it has `Info`, `Infof`, `Warnf` methods.

Run: `go build ./internal/service/subtitle/`
Expected: Compiles — if not, adjust the processor to use `logger` package functions directly instead of passing the indented logger around.

**Step 2: Fix if needed**

If `IndentedLogger` is not an exported type or doesn't match, refactor the processor's `searchEpisode`/`searchMovie`/`checkAndSetSubtitle` methods to call `logger.Indent()` locally or use `logger.Infof`/`logger.Warnf` directly.

**Step 3: Run full test suite**

Run: `go test ./... -v`
Expected: PASS

---

## Task 9: Final Verification

**Step 1: Run full test suite**

Run: `go test ./... -v`
Expected: All tests PASS

**Step 2: Run build**

Run: `go build ./cmd/fusionn/`
Expected: Binary compiles successfully

**Step 3: Verify pipeline order**

Mentally trace the analyze pipeline in `service.go`:
1. AnalyzerProcessor
2. ExtractorProcessor
3. SDHFilterProcessor
4. BazarrSearchProcessor (if client != nil)
5. TranslationQueueProcessor

Confirm BazarrSearchProcessor is between SDHFilter and TranslationQueue.

**Step 4: Final commit (if any fixups needed)**

```bash
git add -A
git commit -m "chore: final cleanup for Bazarr subtitle search integration"
```
