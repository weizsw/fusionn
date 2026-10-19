package subtitlellm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fusionn/internal/config"
)

func testCues(count int, step int) ([]Cue, []Cue) {
	en := make([]Cue, 0, count)
	zh := make([]Cue, 0, count)
	for i := 1; i <= count; i++ {
		start := (i - 1) * step
		en = append(en, Cue{ID: i, Start: start, End: start + step/2, OriginalStart: start, OriginalEnd: start + step/2,
			Raw: "en", Plain: "en", Display: "en", Style: "Default", Alignment: 2, Changes: []string{}})
		zh = append(zh, Cue{ID: i, Start: start, End: start + step/2, OriginalStart: start, OriginalEnd: start + step/2,
			Raw: "zh", Plain: "zh", Display: "zh", Style: "Default", Alignment: 2, Changes: []string{}})
	}
	return en, zh
}

// testBundle builds a bundle whose fingerprint matches its contents.
func testBundle(t *testing.T, en, zh []Cue) Bundle {
	t.Helper()
	bundle := Bundle{Version: Version, EN: en, ZH: zh}
	fingerprint, err := bundleDigest(bundle)
	if err != nil {
		t.Fatal(err)
	}
	bundle.Fingerprint = fingerprint
	return bundle
}

// testConfig starts a server and returns settings pointing at it.
func testConfig(t *testing.T, serverURL string) config.LLMConfig {
	t.Helper()
	return config.LLMConfig{
		BaseURL:        serverURL,
		Model:          "test-model",
		ResponseFormat: config.ResponseFormatJSONObject,
		TimeoutSeconds: 5,
		Workers:        2,
		Retries:        1,
		WindowSeconds:  210,
		OverlapSeconds: 15,
		CacheEnabled:   true,
		CacheDir:       t.TempDir(),
	}
}

func alignmentHandler(t *testing.T, calls *int32, mu *sync.Mutex) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		*calls++
		mu.Unlock()
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %q", r.URL.Path)
		}
		var payload requestPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode payload: %v", err)
			return
		}
		view := struct {
			EN []struct{ ID int } `json:"en"`
			ZH []struct{ ID int } `json:"zh"`
		}{}
		if err := json.Unmarshal([]byte(payload.Messages[1].Content), &view); err != nil {
			t.Errorf("decode view: %v", err)
			return
		}
		alignments := make([]map[string]any, 0)
		for i := range view.EN {
			alignments = append(alignments, map[string]any{
				"en_ids": []int{view.EN[i].ID}, "zh_ids": []int{view.ZH[i].ID}, "layer": "bottom",
			})
		}
		content, _ := json.Marshal(map[string]any{"alignments": alignments})
		response := map[string]any{
			"model":   "resolved-test-model",
			"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": string(content)}}},
			"usage":   map[string]any{"prompt_tokens": 1, "completion_tokens": 2},
		}
		_ = json.NewEncoder(w).Encode(response)
	}
}

func TestAlignSuccessAndAudit(t *testing.T) {
	en, zh := testCues(3, 1000)
	bundle := testBundle(t, en, zh)
	var calls int32
	var mu sync.Mutex
	server := httptest.NewServer(alignmentHandler(t, &calls, &mu))
	defer server.Close()
	cfg := testConfig(t, server.URL+"/v1")
	cfg.ReasoningEffort = "low"
	cfg.APIKey = "secret-key"

	mapping, audit, err := NewEngine().Align(context.Background(), bundle, cfg)
	if err != nil {
		t.Fatalf("Align() error = %v", err)
	}
	if len(mapping.Groups) != 3 {
		t.Fatalf("groups = %d, want 3", len(mapping.Groups))
	}
	for _, group := range mapping.Groups {
		if group.Origin != OriginModel || len(group.ENIDs) != 1 || len(group.ZHIDs) != 1 {
			t.Fatalf("unexpected group %+v", group)
		}
	}
	if mapping.SourceFingerprint != bundle.Fingerprint {
		t.Fatal("source fingerprint mismatch")
	}
	if mapping.Parameters.Model != "test-model" || mapping.Parameters.Endpoint != server.URL+"/v1" {
		t.Fatalf("parameters = %+v", mapping.Parameters)
	}
	if audit.Coverage != (Coverage{EN: 3, ZH: 3}) || len(audit.Restored["en"]) != 0 {
		t.Fatalf("audit = %+v", audit)
	}
	if len(audit.ResolvedModels) != 1 || audit.ResolvedModels[0] != "resolved-test-model" {
		t.Fatalf("resolved models = %v", audit.ResolvedModels)
	}
	if audit.HTTPAttempts != 1 {
		t.Fatalf("http attempts = %d", audit.HTTPAttempts)
	}
	if mapping.Fingerprint == "" {
		t.Fatal("mapping fingerprint missing")
	}
	if _, err := mappingDigest(mapping); err != nil {
		t.Fatal(err)
	}
}

func TestAlignRequestShape(t *testing.T) {
	en, zh := testCues(1, 1000)
	bundle := testBundle(t, en, zh)
	var captured map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer env-secret" {
			t.Errorf("authorization = %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Errorf("decode: %v", err)
		}
		content := `{"alignments":[{"en_ids":[1],"zh_ids":[1],"layer":"bottom"}]}`
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "m", "choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": content}}},
		})
	}))
	defer server.Close()
	t.Setenv("TEST_LLM_KEY", "env-secret")
	cfg := testConfig(t, server.URL+"/v1")
	cfg.APIKeyEnv = "TEST_LLM_KEY"
	cfg.APIKey = "ignored-literal"
	if _, _, err := NewEngine().Align(context.Background(), bundle, cfg); err != nil {
		t.Fatalf("Align() error = %v", err)
	}
	if _, ok := captured["reasoning_effort"]; ok {
		t.Fatal("reasoning_effort sent when empty")
	}
	if captured["model"] != "test-model" {
		t.Fatalf("model = %v", captured["model"])
	}
	if captured["response_format"].(map[string]any)["type"] != "json_object" {
		t.Fatalf("response_format = %v", captured["response_format"])
	}
	if captured["max_tokens"].(float64) != maxTokens {
		t.Fatalf("max_tokens = %v", captured["max_tokens"])
	}
}

func TestAlignJSONSchemaAndNoResponseFormat(t *testing.T) {
	en, zh := testCues(1, 1000)
	bundle := testBundle(t, en, zh)
	for _, format := range []string{config.ResponseFormatJSONSchema, config.ResponseFormatNone} {
		t.Run(format, func(t *testing.T) {
			var captured map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewDecoder(r.Body).Decode(&captured)
				content := `{"alignments":[{"en_ids":[1],"zh_ids":[1],"layer":"bottom"}]}`
				_ = json.NewEncoder(w).Encode(map[string]any{
					"model": "m", "choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": content}}},
				})
			}))
			defer server.Close()
			cfg := testConfig(t, server.URL+"/v1")
			cfg.ResponseFormat = format
			if _, _, err := NewEngine().Align(context.Background(), bundle, cfg); err != nil {
				t.Fatalf("Align() error = %v", err)
			}
			_, present := captured["response_format"]
			if format == config.ResponseFormatNone && present {
				t.Fatal("response_format sent for none")
			}
			if format == config.ResponseFormatJSONSchema {
				rf := captured["response_format"].(map[string]any)
				if rf["type"] != "json_schema" {
					t.Fatalf("response_format = %v", rf)
				}
			}
		})
	}
}

func TestAlignRetriesThenSucceeds(t *testing.T) {
	en, zh := testCues(1, 1000)
	bundle := testBundle(t, en, zh)
	var calls int32
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		current := calls
		mu.Unlock()
		if current == 1 {
			http.Error(w, "upstream secret-key exploded", http.StatusInternalServerError)
			return
		}
		content := `{"alignments":[{"en_ids":[1],"zh_ids":[1],"layer":"bottom"}]}`
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "m", "choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": content}}},
		})
	}))
	defer server.Close()
	cfg := testConfig(t, server.URL+"/v1")
	cfg.APIKey = "secret-key"
	if _, _, err := NewEngine().Align(context.Background(), bundle, cfg); err != nil {
		t.Fatalf("Align() error = %v", err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
}

func TestAlignFailureIsErrorNotPartialSuccess(t *testing.T) {
	en, zh := testCues(1, 1000)
	bundle := testBundle(t, en, zh)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "secret-key should never be echoed", http.StatusInternalServerError)
	}))
	defer server.Close()
	cfg := testConfig(t, server.URL+"/v1")
	cfg.APIKey = "secret-key"
	mapping, audit, err := NewEngine().Align(context.Background(), bundle, cfg)
	if err == nil {
		t.Fatal("expected failed-window error")
	}
	if len(mapping.Groups) == 0 {
		t.Fatal("partial mapping should retain restored groups for auditing")
	}
	if len(audit.FailedWindows) != 1 {
		t.Fatalf("failed windows = %v", audit.FailedWindows)
	}
	combined := err.Error()
	for _, value := range audit.Rejected {
		combined += " " + string(value.Value)
	}
	if strings.Contains(combined, "secret-key") {
		t.Fatal("credential leaked into error/audit")
	}
	entries, readErr := os.ReadDir(filepath.Join(cfg.CacheDir, cacheNamespace))
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		t.Fatal(readErr)
	}
	for _, entry := range entries {
		data, _ := os.ReadFile(filepath.Join(cfg.CacheDir, cacheNamespace, entry.Name()))
		if strings.Contains(string(data), "secret-key") {
			t.Fatalf("credential leaked into cache file %s", entry.Name())
		}
	}
}

func TestAlignTimeout(t *testing.T) {
	en, zh := testCues(1, 1000)
	bundle := testBundle(t, en, zh)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(1500 * time.Millisecond)
	}))
	defer server.Close()
	cfg := testConfig(t, server.URL+"/v1")
	cfg.TimeoutSeconds = 1
	cfg.Retries = 0
	started := time.Now()
	_, _, err := NewEngine().Align(context.Background(), bundle, cfg)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if time.Since(started) > 5*time.Second {
		t.Fatal("timeout not applied")
	}
}

func TestAlignCancellationReturnsContextError(t *testing.T) {
	en, zh := testCues(1, 1000)
	bundle := testBundle(t, en, zh)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(1500 * time.Millisecond)
	}))
	defer server.Close()
	cfg := testConfig(t, server.URL+"/v1")
	cfg.Retries = 3
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	_, _, err := NewEngine().Align(ctx, bundle, cfg)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestAlignCacheReuseAndSettingsInvalidation(t *testing.T) {
	en, zh := testCues(2, 1000)
	bundle := testBundle(t, en, zh)
	var calls int32
	var mu sync.Mutex
	server := httptest.NewServer(alignmentHandler(t, &calls, &mu))
	defer server.Close()
	cfg := testConfig(t, server.URL+"/v1")
	engine := NewEngine()

	mapping1, audit1, err := engine.Align(context.Background(), bundle, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || audit1.CacheHits != 0 {
		t.Fatalf("first run calls=%d cacheHits=%d", calls, audit1.CacheHits)
	}
	mapping2, audit2, err := engine.Align(context.Background(), bundle, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("cache miss on identical run: calls=%d", calls)
	}
	if audit2.CacheHits != 1 {
		t.Fatalf("cache hits = %d", audit2.CacheHits)
	}
	if mapping1.Fingerprint != mapping2.Fingerprint {
		t.Fatal("cache changed mapping fingerprint")
	}

	// Styles are not cache state: they live only in the source bundle and are
	// deliberately excluded from the cache key and request payload. The
	// prototype fingerprint changes when styles change, so alignment refuses
	// the bundle outright instead of silently reusing a stale mapping.

	// Reasoning effort is a request setting and must miss the cache.
	effort := cfg
	effort.ReasoningEffort = "high"
	if _, _, err := engine.Align(context.Background(), bundle, effort); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("settings change reused cache: calls=%d", calls)
	}

	// Window settings must also miss.
	windowed := cfg
	windowed.WindowSeconds = 420
	windowed.OverlapSeconds = 15
	if _, _, err := engine.Align(context.Background(), bundle, windowed); err != nil {
		t.Fatal(err)
	}
	if calls != 3 {
		t.Fatalf("window change reused cache: calls=%d", calls)
	}

	// A different model must not reuse another model's mapping.
	modeled := cfg
	modeled.Model = "other-model"
	if _, _, err := engine.Align(context.Background(), bundle, modeled); err != nil {
		t.Fatal(err)
	}
	if calls != 4 {
		t.Fatalf("model change reused cache: calls=%d", calls)
	}

	// A cached response that no longer even parses is corruption: it is
	// discarded, audited, and re-requested (covering TestAlignRecoversCorruptCacheAsMiss).
	entries, err := os.ReadDir(filepath.Join(cfg.CacheDir, cacheNamespace))
	if err != nil || len(entries) == 0 {
		t.Fatalf("cache entries: %v %v", entries, err)
	}
	// Corrupt every entry; only the base-config entries are read by this call,
	// so exactly those produce corruption audit records and re-requests.
	for _, entry := range entries {
		writeFile(t, filepath.Join(cfg.CacheDir, cacheNamespace, entry.Name()), "{truncated")
	}
	callsBefore := calls
	mapping, audit, err := engine.Align(context.Background(), bundle, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if calls != callsBefore+1 {
		t.Fatalf("corrupt cache entry was not re-requested: calls %d -> %d", callsBefore, calls)
	}
	if len(audit.CacheCorrupted) != 1 {
		t.Fatalf("cache corruption audit = %+v", audit.CacheCorrupted)
	}
	assertCoverage(t, mapping, bundle)
}

func TestAlignCacheDisabled(t *testing.T) {
	en, zh := testCues(1, 1000)
	bundle := testBundle(t, en, zh)
	var calls int32
	var mu sync.Mutex
	server := httptest.NewServer(alignmentHandler(t, &calls, &mu))
	defer server.Close()
	cfg := testConfig(t, server.URL+"/v1")
	cfg.CacheEnabled = false
	engine := NewEngine()
	if _, _, err := engine.Align(context.Background(), bundle, cfg); err != nil {
		t.Fatal(err)
	}
	if _, _, err := engine.Align(context.Background(), bundle, cfg); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("cache disabled but calls = %d", calls)
	}
}

func TestAlignInvalidMapsAreQuarantinedAndRestored(t *testing.T) {
	en, zh := testCues(4, 1000)
	bundle := testBundle(t, en, zh)
	content := `{"alignments":[
{"en_ids":[1],"zh_ids":[1],"layer":"bottom"},
{"en_ids":[true],"zh_ids":[2],"layer":"bottom"},
{"en_ids":[2,2],"zh_ids":[2],"layer":"bottom"},
{"en_ids":[],"zh_ids":[],"layer":"bottom"},
{"en_ids":[3],"zh_ids":[3],"layer":"middle"},
{"en_ids":[99],"zh_ids":[3],"layer":"bottom"},
{"en_ids":[2],"zh_ids":[2],"layer":"bottom","text":"invented"}
]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "m", "choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": content}}},
		})
	}))
	defer server.Close()
	cfg := testConfig(t, server.URL+"/v1")
	mapping, audit, err := NewEngine().Align(context.Background(), bundle, cfg)
	if err != nil {
		t.Fatalf("Align() error = %v", err)
	}
	if len(audit.Rejected) != 6 {
		t.Fatalf("rejected = %d (%+v), want 6", len(audit.Rejected), audit.Rejected)
	}
	// One accepted pair plus one restored group per remaining source cue.
	if len(mapping.Groups) != 7 {
		t.Fatalf("groups = %d, want 7", len(mapping.Groups))
	}
	reasons := map[string]bool{}
	for _, rejected := range audit.Rejected {
		reasons[rejected.Reason] = true
	}
	for _, want := range []string{"id_type", "duplicate_id_in_group", "empty_group", "invalid_layer", "id_outside_window", "row_structure"} {
		if !reasons[want] {
			t.Fatalf("missing rejection reason %q in %v", want, reasons)
		}
	}
	// Every source cue is still covered exactly once after restoration.
	assertCoverage(t, mapping, bundle)
}

func TestAlignTooFarAndNativePositionConflict(t *testing.T) {
	en := []Cue{
		{ID: 1, Start: 0, End: 1000, Style: "Default", Changes: []string{}},
		{ID: 2, Start: 100000, End: 101000, Style: "Default", Changes: []string{}},
	}
	zh := []Cue{
		{ID: 1, Start: 100000, End: 101000, Style: "Default", Changes: []string{}},
		{ID: 2, Start: 100000, End: 101000, Style: "Default", Alignment: 8, Top: true, Changes: []string{}},
	}
	en[1].Top = true
	en[1].Alignment = 8
	bundle := testBundle(t, en, zh)
	content := `{"alignments":[
{"en_ids":[1],"zh_ids":[1],"layer":"bottom"},
{"en_ids":[2],"zh_ids":[2],"layer":"top"}
]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "m", "choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": content}}},
		})
	}))
	defer server.Close()
	cfg := testConfig(t, server.URL+"/v1")
	_, audit, err := NewEngine().Align(context.Background(), bundle, cfg)
	if err != nil {
		t.Fatalf("Align() error = %v", err)
	}
	reasons := map[string]bool{}
	for _, rejected := range audit.Rejected {
		reasons[rejected.Reason] = true
	}
	if !reasons["too_far"] {
		t.Fatalf("expected too_far rejection, got %+v", audit.Rejected)
	}
}

func TestAlignGroupSpanOver30s(t *testing.T) {
	en := []Cue{
		{ID: 1, Start: 0, End: 20000, Style: "Default", Changes: []string{}},
		{ID: 2, Start: 30000, End: 40000, Style: "Default", Changes: []string{}},
	}
	zh := []Cue{{ID: 1, Start: 0, End: 40000, Style: "Default", Changes: []string{}}}
	bundle := testBundle(t, en, zh)
	content := `{"alignments":[{"en_ids":[1,2],"zh_ids":[1],"layer":"bottom"}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "m", "choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": content}}},
		})
	}))
	defer server.Close()
	cfg := testConfig(t, server.URL+"/v1")
	_, audit, err := NewEngine().Align(context.Background(), bundle, cfg)
	if err != nil {
		t.Fatalf("Align() error = %v", err)
	}
	if len(audit.Rejected) != 1 || audit.Rejected[0].Reason != "group_span_over_30s" {
		t.Fatalf("rejections = %+v", audit.Rejected)
	}
}

func TestAlignDuplicateAcrossRowsQuarantinesBoth(t *testing.T) {
	en, zh := testCues(2, 1000)
	bundle := testBundle(t, en, zh)
	content := `{"alignments":[
{"en_ids":[1],"zh_ids":[1],"layer":"bottom"},
{"en_ids":[1],"zh_ids":[2],"layer":"bottom"}
]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "m", "choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": content}}},
		})
	}))
	defer server.Close()
	cfg := testConfig(t, server.URL+"/v1")
	mapping, audit, err := NewEngine().Align(context.Background(), bundle, cfg)
	if err != nil {
		t.Fatalf("Align() error = %v", err)
	}
	if len(audit.Rejected) != 2 {
		t.Fatalf("rejected = %+v", audit.Rejected)
	}
	for _, group := range mapping.Groups {
		if group.Origin != OriginRestored {
			t.Fatalf("expected only restored groups, got %+v", group)
		}
	}
	assertCoverage(t, mapping, bundle)
}

func TestAlignTopLevelStructureErrorRetries(t *testing.T) {
	en, zh := testCues(1, 1000)
	bundle := testBundle(t, en, zh)
	var calls int32
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		current := calls
		mu.Unlock()
		content := `{"alignments":[]}`
		if current == 1 {
			content = "```json\n{\"alignments\":[]}\n```"
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "m", "choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": content}}},
		})
	}))
	defer server.Close()
	cfg := testConfig(t, server.URL+"/v1")
	if _, _, err := NewEngine().Align(context.Background(), bundle, cfg); err != nil {
		t.Fatalf("Align() error = %v", err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
}

func TestAlignContextConflictQuarantinesGroups(t *testing.T) {
	// Cue 1 straddles the core boundary, so both windows see it. The windows
	// disagree on its group membership; every involved owner group must be
	// quarantined instead of using first-writer-wins.
	en := []Cue{
		{ID: 1, Start: 1000, End: 2000, Style: "Default", Changes: []string{}},
		{ID: 2, Start: 2500, End: 3000, Style: "Default", Changes: []string{}},
	}
	zh := []Cue{
		{ID: 1, Start: 1000, End: 3000, Style: "Default", Changes: []string{}},
	}
	bundle := testBundle(t, en, zh)
	var calls int32
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		current := calls
		mu.Unlock()
		content := `{"alignments":[{"en_ids":[1],"zh_ids":[1],"layer":"bottom"}]}`
		if current == 2 {
			content = `{"alignments":[{"en_ids":[1],"zh_ids":[],"layer":"bottom"},{"en_ids":[2],"zh_ids":[1],"layer":"bottom"}]}`
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "m", "choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": content}}},
		})
	}))
	defer server.Close()
	cfg := testConfig(t, server.URL+"/v1")
	cfg.WindowSeconds = 2
	cfg.OverlapSeconds = 1
	cfg.Workers = 1
	mapping, audit, err := NewEngine().Align(context.Background(), bundle, cfg)
	if err != nil {
		t.Fatalf("Align() error = %v", err)
	}
	if len(audit.Conflicts) == 0 {
		t.Fatalf("expected conflicts, audit = %+v", audit)
	}
	for _, group := range mapping.Groups {
		if group.Origin == OriginModel {
			t.Fatalf("conflicting group accepted: %+v", group)
		}
	}
	assertCoverage(t, mapping, bundle)
}

func TestAlignContextDuplicateIsDeduplicated(t *testing.T) {
	en := []Cue{
		{ID: 1, Start: 1000, End: 2000, Style: "Default", Changes: []string{}},
		{ID: 2, Start: 2200, End: 2800, Style: "Default", Changes: []string{}},
	}
	zh := []Cue{
		{ID: 1, Start: 1000, End: 2000, Style: "Default", Changes: []string{}},
		{ID: 2, Start: 2200, End: 2800, Style: "Default", Changes: []string{}},
	}
	bundle := testBundle(t, en, zh)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		content := `{"alignments":[{"en_ids":[1],"zh_ids":[1],"layer":"bottom"},{"en_ids":[2],"zh_ids":[2],"layer":"bottom"}]}`
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "m", "choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": content}}},
		})
	}))
	defer server.Close()
	cfg := testConfig(t, server.URL+"/v1")
	cfg.WindowSeconds = 2
	cfg.OverlapSeconds = 1
	cfg.Workers = 1
	mapping, audit, err := NewEngine().Align(context.Background(), bundle, cfg)
	if err != nil {
		t.Fatalf("Align() error = %v", err)
	}
	modelGroups := 0
	for _, group := range mapping.Groups {
		if group.Origin == OriginModel {
			modelGroups++
		}
	}
	if modelGroups != 2 {
		t.Fatalf("model groups = %d, groups = %+v", modelGroups, mapping.Groups)
	}
	if audit.ContextDuplicates < 1 {
		t.Fatalf("context duplicates = %d", audit.ContextDuplicates)
	}
}

func TestAlignRecoversCorruptCacheAsMiss(t *testing.T) {
	en, zh := testCues(1, 1000)
	bundle := testBundle(t, en, zh)
	var calls int32
	var mu sync.Mutex
	server := httptest.NewServer(alignmentHandler(t, &calls, &mu))
	defer server.Close()
	cfg := testConfig(t, server.URL+"/v1")
	engine := NewEngine()
	if _, _, err := engine.Align(context.Background(), bundle, cfg); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}

	entries, err := os.ReadDir(filepath.Join(cfg.CacheDir, cacheNamespace))
	if err != nil || len(entries) != 1 {
		t.Fatalf("cache entries = %v, err = %v", entries, err)
	}
	cachePath := filepath.Join(cfg.CacheDir, cacheNamespace, entries[0].Name())
	writeFile(t, cachePath, "{not json")

	mapping, audit, err := engine.Align(context.Background(), bundle, cfg)
	if err != nil {
		t.Fatalf("corrupt cache was not recovered: %v", err)
	}
	if calls != 2 {
		t.Fatalf("corrupt cache was not re-requested: calls = %d", calls)
	}
	if len(audit.CacheCorrupted) != 1 || audit.CacheCorrupted[0].Window != 0 {
		t.Fatalf("cache corruption audit = %+v", audit.CacheCorrupted)
	}
	if audit.CacheCorrupted[0].Path != cachePath {
		t.Fatalf("corruption path = %q", audit.CacheCorrupted[0].Path)
	}
	if !strings.Contains(audit.CacheCorrupted[0].Reason, "cache") {
		t.Fatalf("corruption reason = %q", audit.CacheCorrupted[0].Reason)
	}
	if audit.CacheHits != 0 {
		t.Fatalf("cache hits = %d, want 0", audit.CacheHits)
	}
	assertCoverage(t, mapping, bundle)
	if _, err := os.Stat(cachePath); err != nil {
		t.Fatalf("recovered cache was not rewritten: %v", err)
	}
}

func TestAlignForeignCacheEntryIsMiss(t *testing.T) {
	en, zh := testCues(1, 1000)
	bundle := testBundle(t, en, zh)
	var calls int32
	var mu sync.Mutex
	server := httptest.NewServer(alignmentHandler(t, &calls, &mu))
	defer server.Close()
	cfg := testConfig(t, server.URL+"/v1")
	engine := NewEngine()
	if _, _, err := engine.Align(context.Background(), bundle, cfg); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(cfg.CacheDir, cacheNamespace))
	if err != nil || len(entries) != 1 {
		t.Fatal(err)
	}
	cachePath := filepath.Join(cfg.CacheDir, cacheNamespace, entries[0].Name())
	var record map[string]json.RawMessage
	data, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	record["request_hash"] = json.RawMessage(`"different-request"`)
	forged, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, cachePath, string(forged))

	_, audit, err := engine.Align(context.Background(), bundle, cfg)
	if err != nil {
		t.Fatalf("foreign cache was not recovered: %v", err)
	}
	if calls != 2 {
		t.Fatalf("foreign cache was not re-requested: calls = %d", calls)
	}
	if len(audit.CacheCorrupted) != 1 {
		t.Fatalf("cache corruption audit = %+v", audit.CacheCorrupted)
	}
}

func TestAlignCachePermissionErrorFailsLoudly(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses file permissions")
	}
	en, zh := testCues(1, 1000)
	bundle := testBundle(t, en, zh)
	var calls int32
	var mu sync.Mutex
	server := httptest.NewServer(alignmentHandler(t, &calls, &mu))
	defer server.Close()
	cfg := testConfig(t, server.URL+"/v1")
	engine := NewEngine()
	if _, _, err := engine.Align(context.Background(), bundle, cfg); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(cfg.CacheDir, cacheNamespace))
	if err != nil || len(entries) != 1 {
		t.Fatal(err)
	}
	cachePath := filepath.Join(cfg.CacheDir, cacheNamespace, entries[0].Name())
	if err := os.Chmod(cachePath, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(cachePath, 0o644) //nolint:errcheck

	_, audit, err := engine.Align(context.Background(), bundle, cfg)
	if err == nil {
		t.Fatal("permission failure was silently treated as a cache miss")
	}
	if !errors.Is(err, os.ErrPermission) {
		t.Fatalf("error = %v, want permission error", err)
	}
	if calls != 1 {
		t.Fatalf("permission failure triggered a re-request: calls = %d", calls)
	}
	if len(audit.CacheCorrupted) != 0 {
		t.Fatalf("permission failure reported as corruption: %+v", audit.CacheCorrupted)
	}
}

func TestAlignMalformedJSONRetriesCurrentRequest(t *testing.T) {
	en, zh := testCues(1, 1000)
	bundle := testBundle(t, en, zh)
	var mu sync.Mutex
	var bodies []string
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
			return
		}
		mu.Lock()
		calls++
		bodies = append(bodies, string(body))
		current := calls
		mu.Unlock()
		content := "not valid json \u0000"
		if current == 2 {
			content = `{"alignments":[{"en_ids":[1],"zh_ids":[1],"layer":"bottom"}]}`
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "m", "choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": content}}},
		})
	}))
	defer server.Close()
	cfg := testConfig(t, server.URL+"/v1")
	cfg.Retries = 1
	mapping, audit, err := NewEngine().Align(context.Background(), bundle, cfg)
	if err != nil {
		t.Fatalf("Align() error = %v", err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
	if len(bodies) != 2 || bodies[0] != bodies[1] {
		t.Fatalf("retry did not send the current request body")
	}
	if len(audit.RequestRetries) != 1 || audit.RequestRetries[0].Window != 0 {
		t.Fatalf("request retry audit = %+v", audit.RequestRetries)
	}
	if !strings.Contains(audit.RequestRetries[0].Reason, "invalid alignment JSON") {
		t.Fatalf("retry reason = %q", audit.RequestRetries[0].Reason)
	}
	assertCoverage(t, mapping, bundle)
}

func TestAlignFailureSurfacesSanitizedReason(t *testing.T) {
	en, zh := testCues(1, 1000)
	bundle := testBundle(t, en, zh)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "secret-key invalid reasoning_effort", http.StatusBadRequest)
	}))
	defer server.Close()
	cfg := testConfig(t, server.URL+"/v1")
	cfg.APIKey = "secret-key"
	cfg.Retries = 0
	_, audit, err := NewEngine().Align(context.Background(), bundle, cfg)
	if err == nil {
		t.Fatal("expected failure")
	}
	message := err.Error()
	if !strings.Contains(message, "window 0") || !strings.Contains(message, "HTTP 400") {
		t.Fatalf("error lacks window/status: %v", err)
	}
	for _, want := range []string{"reasoning_effort", "response_format", "model", "auth"} {
		if !strings.Contains(message, want) {
			t.Fatalf("guidance lacks %q: %v", want, err)
		}
	}
	if strings.Contains(message, "secret-key") || strings.Contains(message, "invalid reasoning_effort") {
		t.Fatalf("error leaked body or key: %v", err)
	}
	if len(audit.Rejected) == 0 {
		t.Fatal("failed window should still be auditable")
	}
}

func TestAlignRequiresConfiguredAPIKeyEnv(t *testing.T) {
	en, zh := testCues(1, 1000)
	bundle := testBundle(t, en, zh)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("request sent without required API key")
	}))
	defer server.Close()
	t.Setenv("MISSING_LLM_KEY", "")
	cfg := testConfig(t, server.URL+"/v1")
	cfg.APIKeyEnv = "MISSING_LLM_KEY"
	cfg.APIKey = "literal-fallback-must-not-be-used"
	_, _, err := NewEngine().Align(context.Background(), bundle, cfg)
	if err == nil || !strings.Contains(err.Error(), "MISSING_LLM_KEY") || !strings.Contains(err.Error(), "not set") {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(err.Error(), "literal-fallback-must-not-be-used") {
		t.Fatal("error leaked the literal key")
	}
}

func TestAlignRejectsBaseURLCredentialsAndQuery(t *testing.T) {
	en, zh := testCues(1, 1000)
	bundle := testBundle(t, en, zh)
	for _, baseURL := range []string{
		"http://user:pass@127.0.0.1:1/v1",
		"http://127.0.0.1:1/v1?key=secret",
		"http://127.0.0.1:1/v1#fragment",
	} {
		t.Run(baseURL, func(t *testing.T) {
			cfg := testConfig(t, baseURL)
			_, _, err := NewEngine().Align(context.Background(), bundle, cfg)
			if err == nil || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "pass") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestAlignRejectsDuplicateJSONKeys(t *testing.T) {
	en, zh := testCues(1, 1000)
	bundle := testBundle(t, en, zh)
	content := `{"alignments":[{"en_ids":[1],"zh_ids":[1],"layer":"bottom","layer":"top"}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "m", "choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": content}}},
		})
	}))
	defer server.Close()
	cfg := testConfig(t, server.URL+"/v1")
	cfg.Retries = 0
	_, _, err := NewEngine().Align(context.Background(), bundle, cfg)
	if err == nil || !strings.Contains(err.Error(), "duplicate object key") {
		t.Fatalf("error = %v", err)
	}
}

func TestAlignValidatesConfig(t *testing.T) {
	en, zh := testCues(1, 1000)
	bundle := testBundle(t, en, zh)
	cfg := testConfig(t, "http://127.0.0.1:1/v1")
	cfg.Workers = 0
	if _, _, err := NewEngine().Align(context.Background(), bundle, cfg); err == nil {
		t.Fatal("expected workers validation error")
	}
	cfg = testConfig(t, "http://127.0.0.1:1/v1")
	cfg.CacheEnabled = true
	cfg.CacheDir = ""
	if _, _, err := NewEngine().Align(context.Background(), bundle, cfg); err == nil {
		t.Fatal("expected cache_dir validation error")
	}
}

func assertCoverage(t *testing.T, mapping Mapping, bundle Bundle) {
	t.Helper()
	uses := map[string]map[int]int{"en": {}, "zh": {}}
	for _, group := range mapping.Groups {
		for _, id := range group.ENIDs {
			uses["en"][id]++
		}
		for _, id := range group.ZHIDs {
			uses["zh"][id]++
		}
	}
	for _, cue := range bundle.EN {
		if uses["en"][cue.ID] != 1 {
			t.Fatalf("en cue %d used %d times", cue.ID, uses["en"][cue.ID])
		}
	}
	for _, cue := range bundle.ZH {
		if uses["zh"][cue.ID] != 1 {
			t.Fatalf("zh cue %d used %d times", cue.ID, uses["zh"][cue.ID])
		}
	}
}
