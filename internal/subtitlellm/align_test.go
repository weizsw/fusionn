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
	"reflect"
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
	if _, ok := captured["max_tokens"]; ok {
		t.Fatalf("max_tokens sent when unset: %v", captured["max_tokens"])
	}
}

func TestBuildPayloadSendsMaxTokensOnlyWhenSet(t *testing.T) {
	cfg := testConfig(t, "http://unused/v1")
	for _, tc := range []struct {
		maxTokens int
		want      bool
	}{{0, false}, {32000, true}} {
		cfg.MaxTokens = tc.maxTokens
		payload, value, err := buildPayload(window{}, cfg)
		if err != nil {
			t.Fatalf("buildPayload() error = %v", err)
		}
		body, err := json.Marshal(payload)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var sent map[string]any
		if err := json.Unmarshal(body, &sent); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		_, inBody := sent["max_tokens"]
		_, inKey := value["max_tokens"]
		if inBody != tc.want || inKey != tc.want {
			t.Fatalf("max_tokens=%d: in body %v, in cache key %v, want %v", tc.maxTokens, inBody, inKey, tc.want)
		}
		if tc.want && sent["max_tokens"].(float64) != float64(tc.maxTokens) {
			t.Fatalf("max_tokens = %v, want %d", sent["max_tokens"], tc.maxTokens)
		}
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

func TestAlignFailureFallsBackWithAuditedWindows(t *testing.T) {
	en, zh := testCues(1, 1000)
	bundle := testBundle(t, en, zh)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "secret-key should never be echoed", http.StatusInternalServerError)
	}))
	defer server.Close()
	cfg := testConfig(t, server.URL+"/v1")
	cfg.APIKey = "secret-key"
	mapping, audit, err := NewEngine().Align(context.Background(), bundle, cfg)
	if err != nil {
		t.Fatalf("expected fallback with nil error, got: %v", err)
	}
	if len(mapping.Groups) == 0 {
		t.Fatal("fallback mapping should retain restored groups")
	}
	if len(audit.FailedWindows) != 1 {
		t.Fatalf("failed windows = %v", audit.FailedWindows)
	}
	if len(audit.WindowFailures) != 1 {
		t.Fatalf("window failures = %+v, want 1 entry", audit.WindowFailures)
	}
	combined := audit.WindowFailures[0].Reason
	for _, value := range audit.Rejected {
		combined += " " + string(value.Value)
	}
	if strings.Contains(combined, "secret-key") {
		t.Fatal("credential leaked into audit reason")
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
	_, audit, err := NewEngine().Align(context.Background(), bundle, cfg)
	if err != nil {
		t.Fatalf("expected fallback with nil error on client timeout, got: %v", err)
	}
	if len(audit.FailedWindows) != 1 {
		t.Fatalf("failed windows = %v", audit.FailedWindows)
	}
	if len(audit.WindowFailures) != 1 {
		t.Fatalf("window failures = %+v, want 1 entry", audit.WindowFailures)
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
	// One accepted pair. The other cues are restored, then paired by timing
	// because each EN cue overlaps its ZH cue exactly.
	if len(audit.Restored["en"]) != 3 || len(audit.Restored["zh"]) != 3 {
		t.Fatalf("restored = %v, want 3 EN and 3 ZH", audit.Restored)
	}
	if len(mapping.Groups) != 4 {
		t.Fatalf("groups = %d, want 4", len(mapping.Groups))
	}
	for _, group := range mapping.Groups[1:] {
		if group.Origin != OriginTiming {
			t.Fatalf("group %+v, want timing origin", group)
		}
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

// TestAlignTooFarRejection verifies distance checks reject remote cue pairings.
// Native position differences do not veto pairing; excessive distance continues to be rejected.
func TestAlignTooFarRejection(t *testing.T) {
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
	// Both rows are quarantined; the restored cues fall back to timing pairs.
	if len(audit.Restored["en"]) != 2 || len(audit.Restored["zh"]) != 2 {
		t.Fatalf("restored = %v, want 2 EN and 2 ZH", audit.Restored)
	}
	for _, group := range mapping.Groups {
		if group.Origin == OriginModel {
			t.Fatalf("expected no model groups, got %+v", group)
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
	if err != nil {
		t.Fatalf("expected fallback with nil error, got: %v", err)
	}
	if len(audit.FailedWindows) != 1 {
		t.Fatalf("failed windows = %v, want 1", audit.FailedWindows)
	}
	if len(audit.WindowFailures) != 1 {
		t.Fatalf("window failures = %+v, want 1 entry", audit.WindowFailures)
	}
	reason := audit.WindowFailures[0].Reason
	if !strings.Contains(reason, "HTTP 400") {
		t.Fatalf("reason lacks status: %s", reason)
	}
	if strings.Contains(reason, "secret-key") || strings.Contains(reason, "invalid reasoning_effort") {
		t.Fatalf("reason leaked body or key: %s", reason)
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
	_, audit, err := NewEngine().Align(context.Background(), bundle, cfg)
	if err != nil {
		t.Fatalf("expected fallback with nil error, got: %v", err)
	}
	if len(audit.FailedWindows) != 1 {
		t.Fatalf("failed windows = %v, want 1", audit.FailedWindows)
	}
	if len(audit.WindowFailures) != 1 {
		t.Fatalf("window failures = %+v, want 1 entry", audit.WindowFailures)
	}
	if !strings.Contains(audit.WindowFailures[0].Reason, "duplicate object key") {
		t.Fatalf("reason = %s, want duplicate object key", audit.WindowFailures[0].Reason)
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

func TestAlignMixedMultiWindowPreservesSuccessfulEdgesAndRestoresFailedCues(t *testing.T) {
	// Window 0: 0s - 10s (cues 1..3)
	// Window 1: 10s - 20s (cues 4..5)
	en := []Cue{
		{ID: 1, Start: 1000, End: 3000, OriginalStart: 1000, OriginalEnd: 3000, Raw: "en1", Plain: "en1", Display: "en1", Style: "Default", Alignment: 2},
		{ID: 2, Start: 3500, End: 5000, OriginalStart: 3500, OriginalEnd: 5000, Raw: "en2", Plain: "en2", Display: "en2", Style: "Default", Alignment: 2},
		{ID: 3, Start: 5500, End: 7500, OriginalStart: 5500, OriginalEnd: 7500, Raw: "en3", Plain: "en3", Display: "en3", Style: "Default", Alignment: 2},
		{ID: 4, Start: 12000, End: 14000, OriginalStart: 12000, OriginalEnd: 14000, Raw: "en4", Plain: "en4", Display: "en4", Style: "Default", Alignment: 2},
		{ID: 5, Start: 15000, End: 17000, OriginalStart: 15000, OriginalEnd: 17000, Raw: "en5", Plain: "en5", Display: "en5", Style: "Default", Alignment: 2},
	}
	zh := []Cue{
		{ID: 1, Start: 1000, End: 5000, OriginalStart: 1000, OriginalEnd: 5000, Raw: "zh1", Plain: "zh1", Display: "zh1", Style: "Default", Alignment: 2},
		{ID: 2, Start: 5500, End: 6500, OriginalStart: 5500, OriginalEnd: 6500, Raw: "zh2", Plain: "zh2", Display: "zh2", Style: "Default", Alignment: 2},
		{ID: 3, Start: 6500, End: 7500, OriginalStart: 6500, OriginalEnd: 7500, Raw: "zh3", Plain: "zh3", Display: "zh3", Style: "Default", Alignment: 2},
		{ID: 4, Start: 12500, End: 14200, OriginalStart: 12500, OriginalEnd: 14200, Raw: "zh4", Plain: "zh4", Display: "zh4", Style: "Default", Alignment: 2},
		{ID: 5, Start: 15200, End: 17100, OriginalStart: 15200, OriginalEnd: 17100, Raw: "zh5", Plain: "zh5", Display: "zh5", Style: "Default", Alignment: 2},
	}
	bundle := testBundle(t, en, zh)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		promptText := ""
		if len(req.Messages) > 1 {
			promptText = req.Messages[1].Content
		}
		// Window 0 contains core [0, 10000]
		if strings.Contains(promptText, `"core_ms":[0,10000]`) || strings.Contains(promptText, `"core_ms": [0, 10000]`) {
			// Return non-1:1 model alignment: EN [1, 2] -> ZH [1], and EN [3] -> ZH [2, 3]
			content := `{"alignments":[{"en_ids":[1,2],"zh_ids":[1],"layer":"bottom","kind":"dialogue"},{"en_ids":[3],"zh_ids":[2,3],"layer":"bottom","kind":"dialogue"}]}`
			_ = json.NewEncoder(w).Encode(map[string]any{
				"model": "test-model", "choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": content}}},
			})
			return
		}
		// Window 1 fails with 503
		http.Error(w, "upstream unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	cfg := testConfig(t, server.URL+"/v1")
	cfg.WindowSeconds = 10
	cfg.OverlapSeconds = 2
	cfg.Retries = 0

	mapping, audit, err := NewEngine().Align(context.Background(), bundle, cfg)
	if err != nil {
		t.Fatalf("Align() error = %v, want fallback with nil error", err)
	}
	if len(audit.FailedWindows) != 1 || audit.FailedWindows[0] != 1 {
		t.Fatalf("failed windows = %v, want [1]", audit.FailedWindows)
	}

	// Verify Window 0 preserved its non-1:1 model edges
	var found2to1, found1to2 bool
	for _, g := range mapping.Groups {
		if reflect.DeepEqual(g.ENIDs, []int{1, 2}) && reflect.DeepEqual(g.ZHIDs, []int{1}) {
			found2to1 = true
			if g.Origin == OriginRestored {
				t.Error("2:1 group should be model-aligned, not restored")
			}
		}
		if reflect.DeepEqual(g.ENIDs, []int{3}) && reflect.DeepEqual(g.ZHIDs, []int{2, 3}) {
			found1to2 = true
			if g.Origin == OriginRestored {
				t.Error("1:2 group should be model-aligned, not restored")
			}
		}
	}
	if !found2to1 {
		t.Error("2:1 non-1:1 alignment edge was not preserved")
	}
	if !found1to2 {
		t.Error("1:2 non-1:1 alignment edge was not preserved")
	}

	// Verify Window 1 cues (4, 5) were restored, then paired by timing
	if !reflect.DeepEqual(audit.Restored["en"], []int{4, 5}) || !reflect.DeepEqual(audit.Restored["zh"], []int{4, 5}) {
		t.Errorf("restored = %v, want EN and ZH [4 5]", audit.Restored)
	}
	for _, id := range []int{4, 5} {
		paired := false
		for _, g := range mapping.Groups {
			if reflect.DeepEqual(g.ENIDs, []int{id}) && reflect.DeepEqual(g.ZHIDs, []int{id}) {
				paired = true
				if g.Origin != OriginTiming {
					t.Errorf("cue %d origin = %s, want %s", id, g.Origin, OriginTiming)
				}
			}
		}
		if !paired {
			t.Errorf("cue %d was not paired by timing", id)
		}
	}

	assertCoverage(t, mapping, bundle)
}

// TestAlignFailedWindowFallsBackToTimingPairs verifies a failed window pairs
// overlapping cues on English timing and keeps the rest on exact original
// timing and order.
func TestAlignFailedWindowFallsBackToTimingPairs(t *testing.T) {
	en := []Cue{
		{ID: 1, Start: 1000, End: 2500, OriginalStart: 1000, OriginalEnd: 2500, Raw: "en1", Plain: "en1", Display: "en1", Style: "Default", Alignment: 2},
		{ID: 2, Start: 3000, End: 4500, OriginalStart: 3000, OriginalEnd: 4500, Raw: "en2", Plain: "en2", Display: "en2", Style: "Default", Alignment: 2},
		{ID: 3, Start: 5000, End: 6500, OriginalStart: 5000, OriginalEnd: 6500, Raw: "en3", Plain: "en3", Display: "en3", Style: "Default", Alignment: 2},
	}
	zh := []Cue{
		{ID: 1, Start: 1100, End: 2400, OriginalStart: 1100, OriginalEnd: 2400, Raw: "zh1", Plain: "zh1", Display: "zh1", Style: "Default", Alignment: 2},
		{ID: 2, Start: 3100, End: 4600, OriginalStart: 3100, OriginalEnd: 4600, Raw: "zh2", Plain: "zh2", Display: "zh2", Style: "Default", Alignment: 2},
		{ID: 3, Start: 7000, End: 8200, OriginalStart: 7000, OriginalEnd: 8200, Raw: "zh3", Plain: "zh3", Display: "zh3", Style: "Default", Alignment: 2},
	}
	bundle := testBundle(t, en, zh)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream failed", http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := testConfig(t, server.URL+"/v1")
	cfg.Retries = 0
	cfg.EnChars = 65
	cfg.ZhChars = 25
	cfg.EnGapMS = 400
	cfg.ZhGapMS = 500
	cfg.SpanMS = 5500
	cfg.FillGapMS = 500

	mapping, audit, err := NewEngine().Align(context.Background(), bundle, cfg)
	if err != nil {
		t.Fatalf("Align() error = %v, want fallback", err)
	}
	if len(audit.FailedWindows) != 1 {
		t.Fatalf("failed windows = %v, want 1", audit.FailedWindows)
	}

	// Verify exact timing and non-decreasing chronological order in rendered ASS
	style := config.ASSStyleConfig{
		Enabled:         true,
		PlayResX:        1920,
		PlayResY:        1080,
		PrimaryFont:     "PingFang SC",
		PrimarySize:     52,
		PrimaryColor:    "&H08EFECEA",
		SecondaryFont:   "Helvetica Neue",
		SecondarySize:   36,
		SecondaryColor:  "&H087BC0E5",
		Outline:         1.4,
		OutlineColor:    "&H30141414",
		Shadow:          1.5,
		BackColor:       "&H80000000",
		MarginV:         32,
		MarginLeft:      80,
		MarginRight:     80,
		Alignment:       2,
		BorderStyle:     1,
		WrapStyle:       "0",
		ScaleX:          100,
		LineHeightRatio: 1.23,
		BlockPadding:    16,
		CollisionGap:    12,
	}
	rendered, renderErr := Render(bundle, mapping, cfg, style, 1920, 1080)
	if renderErr != nil {
		t.Fatalf("Render() error = %v", renderErr)
	}

	dialogues := parseDialogues(t, string(rendered))
	if len(dialogues) != 4 {
		t.Fatalf("dialogues count = %d, want 4 (2 timing pairs + EN 3 + ZH 3)", len(dialogues))
	}

	expectedTimings := []struct {
		start int
		end   int
		texts []string
	}{
		{start: 1000, end: 2500, texts: []string{"zh1", "en1"}},
		{start: 3000, end: 4500, texts: []string{"zh2", "en2"}},
		{start: 5000, end: 6500, texts: []string{"en3"}},
		{start: 7000, end: 8200, texts: []string{"zh3"}},
	}

	for i, exp := range expectedTimings {
		d := dialogues[i]
		if d.start != exp.start || d.end != exp.end {
			t.Errorf("dialogue [%d] timing = %d-%d, want %d-%d", i, d.start, d.end, exp.start, exp.end)
		}
		for _, text := range exp.texts {
			if !strings.Contains(d.text, text) {
				t.Errorf("dialogue [%d] text = %q, want text containing %q", i, d.text, text)
			}
		}
	}

	// Verify non-decreasing chronological start times
	for i := 1; i < len(dialogues); i++ {
		if dialogues[i].start < dialogues[i-1].start {
			t.Errorf("dialogue [%d] start (%d) < dialogue [%d] start (%d)", i, dialogues[i].start, i-1, dialogues[i-1].start)
		}
	}
	assertCoverage(t, mapping, bundle)
}

func TestAlignProtocolExhaustionAndRequestCount(t *testing.T) {
	en, zh := testCues(1, 1000)
	bundle := testBundle(t, en, zh)

	var requestCount int
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestCount++
		mu.Unlock()
		http.Error(w, "bad gateway", http.StatusBadGateway)
	}))
	defer server.Close()

	cfg := testConfig(t, server.URL+"/v1")
	cfg.Retries = 2 // 1 initial + 2 retries = 3 total requests

	mapping, audit, err := NewEngine().Align(context.Background(), bundle, cfg)
	if err != nil {
		t.Fatalf("Align() error = %v, want fallback on retry exhaustion", err)
	}
	mu.Lock()
	totalRequests := requestCount
	mu.Unlock()
	if totalRequests != 3 {
		t.Fatalf("request count = %d, want exactly 3 (1 initial + 2 retries)", totalRequests)
	}
	if len(audit.FailedWindows) != 1 {
		t.Fatalf("failed windows = %v, want 1", audit.FailedWindows)
	}
	if len(audit.WindowFailures) != 1 {
		t.Fatalf("window failures = %+v, want 1 entry", audit.WindowFailures)
	}
	if !strings.Contains(audit.WindowFailures[0].Reason, "HTTP 502") {
		t.Fatalf("reason = %s, want HTTP 502", audit.WindowFailures[0].Reason)
	}
	assertCoverage(t, mapping, bundle)
}

func TestAlignSuccessfulRetryDoesNotDegrade(t *testing.T) {
	en, zh := testCues(1, 1000)
	bundle := testBundle(t, en, zh)

	var attempt int
	var mu sync.Mutex
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		attempt++
		current := attempt
		mu.Unlock()
		if current == 1 {
			http.Error(w, "transient error", http.StatusInternalServerError)
			return
		}
		content := `{"alignments":[{"en_ids":[1],"zh_ids":[1],"layer":"bottom","kind":"dialogue"}]}`
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "test-model", "choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": content}}},
		})
	}))
	defer server.Close()

	cfg := testConfig(t, server.URL+"/v1")
	cfg.Retries = 1

	mapping, audit, err := NewEngine().Align(context.Background(), bundle, cfg)
	if err != nil {
		t.Fatalf("Align() error = %v, want successful retry", err)
	}
	if len(audit.FailedWindows) != 0 {
		t.Fatalf("failed windows = %v, want 0 on successful retry", audit.FailedWindows)
	}
	if len(audit.WindowFailures) != 0 {
		t.Fatalf("window failures = %+v, want 0 on successful retry", audit.WindowFailures)
	}
	if len(mapping.Groups) != 1 || mapping.Groups[0].Origin == OriginRestored {
		t.Fatalf("groups = %+v, expected model-aligned group", mapping.Groups)
	}
	assertCoverage(t, mapping, bundle)
}

func TestAlignNonPermissionCacheIOErrorIsHardError(t *testing.T) {
	en, zh := testCues(1, 1000)
	bundle := testBundle(t, en, zh)

	dir := t.TempDir()
	// Create a file where the cache directory is supposed to be, causing ENOTDIR
	blockedPath := filepath.Join(dir, "blocked-cache-dir")
	if err := os.WriteFile(blockedPath, []byte("plain file"), 0644); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server should not be contacted if cache dir is invalid", http.StatusInternalServerError)
	}))
	defer server.Close()

	cfg := testConfig(t, server.URL+"/v1")
	cfg.CacheEnabled = true
	cfg.CacheDir = blockedPath

	_, _, err := NewEngine().Align(context.Background(), bundle, cfg)
	if err == nil {
		t.Fatal("expected hard error on cache ENOTDIR, got nil")
	}
}
