package subtitlellm

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

// makeSyntheticReplayFixture creates an in-memory/tempdir fixture that provides
// valid sources, style, word timings, manifest, recorded responses, and ASS.
// It allows negative tests to tamper with exactly one field and verify that
// replayRecordedResponses fails loudly without touching any external directories.
func makeSyntheticReplayFixture(t *testing.T) (string, replayConfig) {
	t.Helper()
	dir := t.TempDir()

	enCues := []Cue{
		{ID: 1, Start: 1000, End: 2000, OriginalStart: 1000, OriginalEnd: 2000, Raw: "Hello", Plain: "Hello", Display: "Hello", Alignment: 2, Style: "Default", Changes: []string{}},
	}
	zhCues := []Cue{
		{ID: 1, Start: 1000, End: 2000, OriginalStart: 1000, OriginalEnd: 2000, Raw: "你好", Plain: "你好", Display: "你好", Alignment: 2, Style: "Default", Changes: []string{}},
	}
	bundle := testBundle(t, enCues, zhCues)
	sourcesJSON, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	sourcesPath := filepath.Join(dir, "sources.json")
	if err := os.WriteFile(sourcesPath, sourcesJSON, 0o600); err != nil {
		t.Fatal(err)
	}

	style := fixtureStyle()
	styleJSON, err := json.Marshal(style)
	if err != nil {
		t.Fatal(err)
	}
	stylePath := filepath.Join(dir, "delivered-style.json")
	if err := os.WriteFile(stylePath, styleJSON, 0o600); err != nil {
		t.Fatal(err)
	}

	words := WordTimings{}
	wordsJSON, err := json.Marshal(words)
	if err != nil {
		t.Fatal(err)
	}
	wordsPath := filepath.Join(dir, "word-timings.json")
	if err := os.WriteFile(wordsPath, wordsJSON, 0o600); err != nil {
		t.Fatal(err)
	}

	// Single window: [0, 210000)
	windows := buildWindows(bundle, 210000, 15000)
	if len(windows) != 1 {
		t.Fatalf("expected 1 window, got %d", len(windows))
	}
	w0 := windows[0]
	view := w0.view()
	userContent, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	reqPayload := struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}{
		Messages: []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{
			{Role: "system", Content: prompt},
			{Role: "user", Content: string(userContent)},
		},
	}
	reqJSON, err := json.Marshal(reqPayload)
	if err != nil {
		t.Fatal(err)
	}

	respJSON := []byte(`{"alignments":[{"en_ids":[1],"zh_ids":[1],"layer":"bottom","kind":"dialogue"}]}`)
	rec := cacheRecord{
		RequestHash: "synth-req-hash",
		Index:       0,
		Request:     reqJSON,
		Response:    respJSON,
	}
	recJSON, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	recPath := filepath.Join(dir, "000-synth.json")
	if err := os.WriteFile(recPath, recJSON, 0o600); err != nil {
		t.Fatal(err)
	}
	recHash := fmt.Sprintf("%x", sha256.Sum256(recJSON))

	manifest := []recordManifestEntry{
		{
			Index:  0,
			Path:   "000-synth.json",
			SHA256: recHash,
		},
	}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(manifestPath, manifestJSON, 0o600); err != nil {
		t.Fatal(err)
	}

	mapping := Mapping{
		Version:           Version,
		SourceFingerprint: bundle.Fingerprint,
		Groups: []Group{
			{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
		},
	}
	assBytes, err := RenderWithWordTimings(bundle, mapping, fixtureConfig(), style, 3840, 1632, words)
	if err != nil {
		t.Fatal(err)
	}
	assPath := filepath.Join(dir, "delivered.ass")
	if err := os.WriteFile(assPath, assBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := replayConfig{
		Dir:          dir,
		ManifestPath: manifestPath,
		SourcesPath:  sourcesPath,
		StylePath:    stylePath,
		WordsPath:    wordsPath,
		ASSPath:      assPath,
	}
	return dir, cfg
}

func TestReplayRecorded_ValidSyntheticBaselinePasses(t *testing.T) {
	_, cfg := makeSyntheticReplayFixture(t)
	res, err := replayRecordedResponses(cfg)
	if err != nil {
		t.Fatalf("expected clean replay on synthetic fixture, got error: %v", err)
	}
	if len(res.Mapping.Groups) != 1 {
		t.Fatalf("expected 1 mapping group, got %d", len(res.Mapping.Groups))
	}
	if res.RecordedCount != 1 {
		t.Fatalf("expected 1 recorded response, got %d", res.RecordedCount)
	}
	if res.Audit.CacheHits != 0 {
		t.Fatalf("expected 0 cache hits, got %d", res.Audit.CacheHits)
	}
}

func TestReplayRecorded_MissingASSFails(t *testing.T) {
	dir, cfg := makeSyntheticReplayFixture(t)
	cfg.ASSPath = filepath.Join(dir, "does-not-exist.ass")
	_, err := replayRecordedResponses(cfg)
	if err == nil {
		t.Fatal("expected failure when declared ASS path does not exist, got nil")
	}
	if !strings.Contains(err.Error(), "read delivered ASS") {
		t.Fatalf("expected error mentioning 'read delivered ASS', got: %v", err)
	}
}

func TestReplayRecorded_CorruptWordTimingsFails(t *testing.T) {
	_, cfg := makeSyntheticReplayFixture(t)
	if err := os.WriteFile(cfg.WordsPath, []byte("NOT_VALID_JSON{"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := replayRecordedResponses(cfg)
	if err == nil {
		t.Fatal("expected failure when word-timings JSON is corrupted, got nil")
	}
	if !strings.Contains(err.Error(), "decode word timings") {
		t.Fatalf("expected error mentioning 'decode word timings', got: %v", err)
	}
}

func TestReplayRecorded_ManifestIndexMismatchFails(t *testing.T) {
	dir, cfg := makeSyntheticReplayFixture(t)
	rawRecord, err := os.ReadFile(filepath.Join(dir, "000-synth.json"))
	if err != nil {
		t.Fatal(err)
	}
	recHash := fmt.Sprintf("%x", sha256.Sum256(rawRecord))
	tamperedManifest := []recordManifestEntry{
		{
			Index:  99, // Mismatched index vs window 0
			Path:   "000-synth.json",
			SHA256: recHash,
		},
	}
	tamperedJSON, err := json.Marshal(tamperedManifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.ManifestPath, tamperedJSON, 0o600); err != nil {
		t.Fatal(err)
	}

	_, err = replayRecordedResponses(cfg)
	if err == nil {
		t.Fatal("expected failure when manifest index does not match window index, got nil")
	}
	if !strings.Contains(err.Error(), "want window index 0") {
		t.Fatalf("expected error mentioning 'want window index 0', got: %v", err)
	}
}

func TestReplayRecorded_RecordIndexMismatchFails(t *testing.T) {
	dir, cfg := makeSyntheticReplayFixture(t)
	recPath := filepath.Join(dir, "000-synth.json")
	rawRecord, err := os.ReadFile(recPath)
	if err != nil {
		t.Fatal(err)
	}
	var rec cacheRecord
	if err := json.Unmarshal(rawRecord, &rec); err != nil {
		t.Fatal(err)
	}
	rec.Index = 99 // Mismatched index vs window 0
	newRecJSON, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(recPath, newRecJSON, 0o600); err != nil {
		t.Fatal(err)
	}
	// Also update manifest SHA256 so hash check passes and we test index mismatch:
	newHash := fmt.Sprintf("%x", sha256.Sum256(newRecJSON))
	manifest := []recordManifestEntry{
		{Index: 0, Path: "000-synth.json", SHA256: newHash},
	}
	manifestJSON, _ := json.Marshal(manifest)
	_ = os.WriteFile(cfg.ManifestPath, manifestJSON, 0o600)

	_, err = replayRecordedResponses(cfg)
	if err == nil {
		t.Fatal("expected failure when cache record Index does not match window index, got nil")
	}
	if !strings.Contains(err.Error(), "Index 99 != window index 0") {
		t.Fatalf("expected error mentioning 'Index 99 != window index 0', got: %v", err)
	}
}

func TestReplayRecorded_RequestCueMismatchFails(t *testing.T) {
	dir, cfg := makeSyntheticReplayFixture(t)
	recPath := filepath.Join(dir, "000-synth.json")
	rawRecord, err := os.ReadFile(recPath)
	if err != nil {
		t.Fatal(err)
	}
	var rec cacheRecord
	if err := json.Unmarshal(rawRecord, &rec); err != nil {
		t.Fatal(err)
	}

	// Change request payload user view cue ID to 999 (mismatch with window EN cue 1)
	mismatchedPayload := struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}{
		Messages: []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}{
			{Role: "system", Content: prompt},
			{Role: "user", Content: `{"core_ms":[0,210000],"en":[{"id":999,"start":1000,"end":2000,"text":"Hello"}],"zh":[{"id":1,"start":1000,"end":2000,"text":"你好"}]}`},
		},
	}
	reqJSON, err := json.Marshal(mismatchedPayload)
	if err != nil {
		t.Fatal(err)
	}
	rec.Request = reqJSON

	newRecJSON, err := json.Marshal(rec)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(recPath, newRecJSON, 0o600); err != nil {
		t.Fatal(err)
	}
	newHash := fmt.Sprintf("%x", sha256.Sum256(newRecJSON))
	manifest := []recordManifestEntry{
		{Index: 0, Path: "000-synth.json", SHA256: newHash},
	}
	manifestJSON, _ := json.Marshal(manifest)
	_ = os.WriteFile(cfg.ManifestPath, manifestJSON, 0o600)

	_, err = replayRecordedResponses(cfg)
	if err == nil {
		t.Fatal("expected failure when request cue ID does not match window, got nil")
	}
	if !strings.Contains(err.Error(), "EN cue 0 ID mismatch") {
		t.Fatalf("expected error mentioning 'EN cue 0 ID mismatch', got: %v", err)
	}
}

func TestReplayRecorded_Sha256MismatchFails(t *testing.T) {
	_, cfg := makeSyntheticReplayFixture(t)
	tamperedManifest := []recordManifestEntry{
		{
			Index:  0,
			Path:   "000-synth.json",
			SHA256: "0000000000000000000000000000000000000000000000000000000000000000",
		},
	}
	tamperedJSON, err := json.Marshal(tamperedManifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.ManifestPath, tamperedJSON, 0o600); err != nil {
		t.Fatal(err)
	}

	_, err = replayRecordedResponses(cfg)
	if err == nil {
		t.Fatal("expected failure when manifest SHA256 does not match record hash, got nil")
	}
	if !strings.Contains(err.Error(), "sha256 changed") {
		t.Fatalf("expected error mentioning 'sha256 changed', got: %v", err)
	}
}

// TestReconcile_GenuinelyDifferentTranslationStillRejected proves that arbitration
// refuses conflicting translations across windows and preserves single-sided fallback.
func TestReconcile_GenuinelyDifferentTranslationStillRejected(t *testing.T) {
	coreMS := 10000
	en := makeTestCues("en", []int{10}, []int{10500}, []int{11500})
	zh := makeTestCues("zh", []int{20, 21}, []int{10500, 10500}, []int{11500, 11500})
	bundle := testBundle(t, en, zh)

	// Window 0 proposes EN[10] <-> ZH[20]
	// Window 1 proposes EN[10] <-> ZH[21] (conflicting translation)
	batches := []batch{
		{
			Index:   0,
			ENInput: []int{10},
			ZHInput: []int{20, 21},
			Valid: []Group{
				{ENIDs: []int{10}, ZHIDs: []int{20}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
			},
		},
		{
			Index:   1,
			ENInput: []int{10},
			ZHInput: []int{20, 21},
			Valid: []Group{
				{ENIDs: []int{10}, ZHIDs: []int{21}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
			},
		},
	}

	accepted, audit := reconcile(bundle, batches, coreMS)
	if len(audit.Conflicts) == 0 {
		t.Fatalf("expected conflicting translations to trigger conflict, got 0 conflicts: %+v", accepted)
	}
	// Neither contradictory model group should be accepted
	for _, g := range accepted {
		if g.Origin == OriginModel {
			t.Fatalf("contradictory translation was unexpectedly accepted: %+v", g)
		}
	}
	// Fallback to single-sided restored cues
	if len(audit.Restored["en"]) != 1 || audit.Restored["en"][0] != 10 {
		t.Fatalf("expected EN 10 to be restored single-sided, got %+v", audit.Restored["en"])
	}
}

// TestEngine_TruncatedContextArbitrationUsesActualWindowInput incorporates the parent
// engine probe (previously /tmp/fusionn-review-ea67136-engine-probe_test.go) to verify
// that Engine.Align populates actual window input on cold start and cache replay.
func TestEngine_TruncatedContextArbitrationUsesActualWindowInput(t *testing.T) {
	cue := func(id, start, end int, text string) Cue {
		return Cue{
			ID:            id,
			Start:         start,
			End:           end,
			OriginalStart: start,
			OriginalEnd:   end,
			Raw:           text,
			Plain:         text,
			Display:       text,
			Alignment:     2,
			Style:         "Default",
			Changes:       []string{},
		}
	}
	bundle := testBundle(t,
		[]Cue{cue(1, 100, 500, "An opening."), cue(2, 2200, 2450, "A longer"), cue(3, 2800, 3200, "thought.")},
		[]Cue{cue(1, 100, 500, "开头。"), cue(2, 2200, 2450, "一个"), cue(3, 2800, 3200, "长句。")},
	)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var p requestPayload
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			t.Errorf("decode: %v", err)
			w.WriteHeader(500)
			return
		}
		var view struct {
			EN []struct {
				ID int
			} `json:"en"`
		}
		if len(p.Messages) < 2 {
			t.Error("no source view")
			w.WriteHeader(500)
			return
		}
		if err := json.Unmarshal([]byte(p.Messages[1].Content), &view); err != nil {
			t.Errorf("source view: %v", err)
			w.WriteHeader(500)
			return
		}
		if len(view.EN) != 2 {
			t.Errorf("unexpected source IDs: %+v", view.EN)
			w.WriteHeader(500)
			return
		}
		content := `{"alignments":[{"en_ids":[1],"zh_ids":[1],"layer":"bottom","kind":"dialogue"},{"en_ids":[2],"zh_ids":[2],"layer":"bottom","kind":"dialogue"}]}`
		if view.EN[0].ID == 2 {
			content = `{"alignments":[{"en_ids":[2,3],"zh_ids":[2,3],"layer":"bottom","kind":"dialogue"}]}`
		}
		if err := json.NewEncoder(w).Encode(map[string]any{
			"model": "mock",
			"choices": []any{
				map[string]any{"finish_reason": "stop", "message": map[string]any{"content": content}},
			},
		}); err != nil {
			t.Errorf("response: %v", err)
		}
	}))
	defer server.Close()

	cfg := testConfig(t, server.URL+"/v1")
	cfg.WindowSeconds = 2
	cfg.OverlapSeconds = 1
	cfg.Retries = 0
	engine := NewEngine()

	for pass := 0; pass < 2; pass++ {
		mapping, audit, err := engine.Align(context.Background(), bundle, cfg)
		if err != nil {
			t.Fatal(err)
		}
		if len(audit.Conflicts) != 0 || len(audit.Restored["en"])+len(audit.Restored["zh"]) != 0 {
			t.Fatalf("window membership not used in arbitration: %+v", audit)
		}
		if len(mapping.Groups) != 2 {
			t.Fatalf("expected opening plus complete owner group, got %+v", mapping.Groups)
		}
		found := false
		for _, g := range mapping.Groups {
			if reflect.DeepEqual(g.ENIDs, []int{2, 3}) && reflect.DeepEqual(g.ZHIDs, []int{2, 3}) && g.OwnerWindow == 1 {
				found = true
			}
		}
		if !found {
			t.Fatalf("complete bilingual owner missing: %+v", mapping.Groups)
		}
		if calls.Load() != 2 {
			t.Fatalf("unexpected network attempts %d", calls.Load())
		}
		if pass == 1 && audit.CacheHits != 2 {
			t.Fatalf("replay cache hits %d", audit.CacheHits)
		}
	}
}
