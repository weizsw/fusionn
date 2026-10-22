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
	"testing"

	"github.com/fusionn/internal/config"
)

// TestAlign_CrossLanguageNativePositionDifferenceAccepted verifies that a semantic
// pairing between cues with different native screen positions (e.g., EN bottom,
// ZH top, or vice versa) is accepted by row validation and Align rather than
// rejected with native_position_conflict.
func TestAlign_CrossLanguageNativePositionDifferenceAccepted(t *testing.T) {
	tests := []struct {
		name    string
		enTop   bool
		enAlign int
		zhTop   bool
		zhAlign int
		layer   string
	}{
		{
			name:    "en_bottom_zh_top",
			enTop:   false,
			enAlign: 2,
			zhTop:   true,
			zhAlign: 8,
			layer:   LayerTop,
		},
		{
			name:    "en_top_zh_bottom",
			enTop:   true,
			enAlign: 8,
			zhTop:   false,
			zhAlign: 2,
			layer:   LayerBottom,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			en := []Cue{
				{
					ID:        1,
					Start:     5000,
					End:       8000,
					Plain:     "Synthetic speech content.",
					Display:   "Synthetic speech content.",
					Style:     "Default",
					Alignment: tc.enAlign,
					Top:       tc.enTop,
					Changes:   []string{},
				},
			}
			zh := []Cue{
				{
					ID:        1,
					Start:     5100,
					End:       7900,
					Plain:     "合成对白测试文本。",
					Display:   "合成对白测试文本。",
					Style:     "Default",
					Alignment: tc.zhAlign,
					Top:       tc.zhTop,
					Changes:   []string{},
				},
			}
			bundle := testBundle(t, en, zh)

			// Model proposes pairing 1 and 1 into one group.
			content := `{"alignments":[{"en_ids":[1],"zh_ids":[1],"layer":"` + tc.layer + `"}]}`
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"model": "test-model",
					"choices": []any{
						map[string]any{"finish_reason": "stop", "message": map[string]any{"content": content}},
					},
				})
			}))
			defer server.Close()

			cfg := testConfig(t, server.URL+"/v1")
			mapping, audit, err := NewEngine().Align(context.Background(), bundle, cfg)
			if err != nil {
				t.Fatalf("Align() error = %v", err)
			}

			// Must not produce native_position_conflict
			for _, rej := range audit.Rejected {
				if rej.Reason == "native_position_conflict" {
					t.Fatalf("unexpected native_position_conflict rejection: %+v", rej)
				}
			}
			if len(audit.Rejected) != 0 {
				t.Fatalf("expected 0 rejected rows, got %d: %+v", len(audit.Rejected), audit.Rejected)
			}

			// Candidate must be accepted with both EN and ZH in the same group
			if len(mapping.Groups) != 1 {
				t.Fatalf("expected 1 accepted group, got %d: %+v", len(mapping.Groups), mapping.Groups)
			}
			g := mapping.Groups[0]
			if len(g.ENIDs) != 1 || g.ENIDs[0] != 1 || len(g.ZHIDs) != 1 || g.ZHIDs[0] != 1 {
				t.Fatalf("group does not retain paired cues: en=%v zh=%v", g.ENIDs, g.ZHIDs)
			}
			if g.Origin != OriginModel {
				t.Fatalf("expected origin model, got %s", g.Origin)
			}

			// Source cue position fields must remain intact
			if bundle.EN[0].Top != tc.enTop || bundle.EN[0].Alignment != tc.enAlign {
				t.Fatalf("source EN position mutated: top=%v align=%d", bundle.EN[0].Top, bundle.EN[0].Alignment)
			}
			if bundle.ZH[0].Top != tc.zhTop || bundle.ZH[0].Alignment != tc.zhAlign {
				t.Fatalf("source ZH position mutated: top=%v align=%d", bundle.ZH[0].Top, bundle.ZH[0].Alignment)
			}
		})
	}
}

// TestAlign_SameLanguageNativePositionTransitionAccepted verifies that when cues within
// the same language transition between bottom and top in an otherwise content-aligned group,
// the group is accepted rather than vetoed by native position conflict.
func TestAlign_SameLanguageNativePositionTransitionAccepted(t *testing.T) {
	// EN has 2 cues: cue 1 is bottom, cue 2 is top. ZH has 1 cue: cue 1 is top.
	en := []Cue{
		{
			ID:        1,
			Start:     1000,
			End:       2500,
			Plain:     "First part at bottom.",
			Display:   "First part at bottom.",
			Style:     "Default",
			Alignment: 2,
			Top:       false,
			Changes:   []string{},
		},
		{
			ID:        2,
			Start:     2600,
			End:       4000,
			Plain:     "Second part at top.",
			Display:   "Second part at top.",
			Style:     "Default",
			Alignment: 8,
			Top:       true,
			Changes:   []string{},
		},
	}
	zh := []Cue{
		{
			ID:        1,
			Start:     1000,
			End:       3900,
			Plain:     "完整对应的顶部中文。",
			Display:   "完整对应的顶部中文。",
			Style:     "Default",
			Alignment: 8,
			Top:       true,
			Changes:   []string{},
		},
	}
	bundle := testBundle(t, en, zh)

	content := `{"alignments":[{"en_ids":[1,2],"zh_ids":[1],"layer":"top"}]}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "test-model",
			"choices": []any{
				map[string]any{"finish_reason": "stop", "message": map[string]any{"content": content}},
			},
		})
	}))
	defer server.Close()

	cfg := testConfig(t, server.URL+"/v1")
	mapping, audit, err := NewEngine().Align(context.Background(), bundle, cfg)
	if err != nil {
		t.Fatalf("Align() error = %v", err)
	}

	for _, rej := range audit.Rejected {
		if rej.Reason == "native_position_conflict" {
			t.Fatalf("unexpected native_position_conflict rejection: %+v", rej)
		}
	}
	if len(audit.Rejected) != 0 {
		t.Fatalf("expected 0 rejected rows, got %d: %+v", len(audit.Rejected), audit.Rejected)
	}

	if len(mapping.Groups) != 1 {
		t.Fatalf("expected 1 accepted group, got %d: %+v", len(mapping.Groups), mapping.Groups)
	}
	g := mapping.Groups[0]
	if len(g.ENIDs) != 2 || g.ENIDs[0] != 1 || g.ENIDs[1] != 2 || len(g.ZHIDs) != 1 || g.ZHIDs[0] != 1 {
		t.Fatalf("pairing split unexpectedly: en=%v zh=%v", g.ENIDs, g.ZHIDs)
	}

	// Verify source positions preserved
	if bundle.EN[0].Top != false || bundle.EN[1].Top != true || bundle.ZH[0].Top != true {
		t.Fatalf("source positions mutated: en0=%v en1=%v zh0=%v", bundle.EN[0].Top, bundle.EN[1].Top, bundle.ZH[0].Top)
	}
}

// TestAlign_MaxDistanceToleranceBoundary verifies that validateResponse respects
// custom maxDistance thresholds on cue distance checks.
func TestAlign_MaxDistanceToleranceBoundary(t *testing.T) {
	w := window{
		Index:     0,
		CoreStart: 0,
		CoreEnd:   60000,
		EN: []Cue{
			{ID: 1, Start: 1000, End: 2000, Plain: "Cue 1", Display: "Cue 1"},
		},
		ZH: []Cue{
			{ID: 1, Start: 5000, End: 6000, Plain: "ZH 1", Display: "ZH 1"},
		},
	}
	content := json.RawMessage(`{"alignments":[{"en_ids":[1],"zh_ids":[1],"layer":"bottom"}]}`)

	// With tolerance 3000ms: distance is 3000ms (1000 to 5000 start difference is 4000ms).
	// Distance between start times is 4000ms.
	// When maxDistance is 2000, it should be rejected with "too_far".
	valid, rejections, err := validateResponse(content, w, 2000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(valid) != 0 || len(rejections) != 1 || rejections[0].Reason != "too_far" {
		t.Fatalf("expected too_far rejection with tight maxDistance=2000, got valid=%v rejections=%v", valid, rejections)
	}

	// When maxDistance is 10000, it should be accepted.
	valid, rejections, err = validateResponse(content, w, 10000)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(valid) != 1 || len(rejections) != 0 {
		t.Fatalf("expected accepted pair with relaxed maxDistance=10000, got valid=%v rejections=%v", valid, rejections)
	}
}
func TestAlign_NegativeSafeguardsPreserved(t *testing.T) {
	en := []Cue{
		{ID: 1, Start: 1000, End: 2000, Plain: "Cue 1", Display: "Cue 1", Top: false, Alignment: 2, Changes: []string{}},
		{ID: 2, Start: 2100, End: 3000, Plain: "Cue 2", Display: "Cue 2", Top: true, Alignment: 8, Changes: []string{}},
		{ID: 3, Start: 40000, End: 41000, Plain: "Far Cue 3", Display: "Far Cue 3", Top: false, Alignment: 2, Changes: []string{}},
		{ID: 4, Start: 1000, End: 35000, Plain: "Long Cue 4", Display: "Long Cue 4", Top: false, Alignment: 2, Changes: []string{}},
	}
	zh := []Cue{
		{ID: 10, Start: 1000, End: 2000, Plain: "ZH 10", Display: "ZH 10", Top: true, Alignment: 8, Changes: []string{}},
		{ID: 20, Start: 2100, End: 3000, Plain: "ZH 20", Display: "ZH 20", Top: false, Alignment: 2, Changes: []string{}},
		{ID: 30, Start: 40000, End: 41000, Plain: "ZH 30", Display: "ZH 30", Top: false, Alignment: 2, Changes: []string{}},
	}
	tracks := map[string]map[int]Cue{
		"en": {1: en[0], 2: en[1], 3: en[2], 4: en[3]},
		"zh": {10: zh[0], 20: zh[1], 30: zh[2]},
	}

	// 1. Unknown ID outside window
	if reason := validateRowValues(Group{ENIDs: []int{999}, ZHIDs: []int{10}}, tracks, MaxDistanceMS); reason != "id_outside_window" {
		t.Errorf("unknown EN ID: got reason %q, want id_outside_window", reason)
	}
	if reason := validateRowValues(Group{ENIDs: []int{1}, ZHIDs: []int{888}}, tracks, MaxDistanceMS); reason != "id_outside_window" {
		t.Errorf("unknown ZH ID: got reason %q, want id_outside_window", reason)
	}

	// 2. Duplicate ID in group
	if reason := validateRowValues(Group{ENIDs: []int{1, 1}, ZHIDs: []int{10}}, tracks, MaxDistanceMS); reason != "duplicate_id_in_group" {
		t.Errorf("duplicate EN ID in group: got reason %q, want duplicate_id_in_group", reason)
	}
	if reason := validateRowValues(Group{ENIDs: []int{1}, ZHIDs: []int{10, 10}}, tracks, MaxDistanceMS); reason != "duplicate_id_in_group" {
		t.Errorf("duplicate ZH ID in group: got reason %q, want duplicate_id_in_group", reason)
	}

	// 3. Excessive distance (outlier far apart)
	if reason := validateRowValues(Group{ENIDs: []int{1}, ZHIDs: []int{30}}, tracks, MaxDistanceMS); reason != "too_far" {
		t.Errorf("excessive distance: got reason %q, want too_far", reason)
	}

	// 4. Group span over 30s
	if reason := validateRowValues(Group{ENIDs: []int{1, 4}, ZHIDs: []int{10}}, tracks, MaxDistanceMS); reason != "group_span_over_30s" {
		t.Errorf("group span over 30s: got reason %q, want group_span_over_30s", reason)
	}
}

const (
	positionRecordDirEnv      = "FUSIONN_POSITION_RECORD_DIR"
	positionRecordManifestEnv = "FUSIONN_POSITION_RECORD_MANIFEST"
	positionSourcesEnv        = "FUSIONN_POSITION_SOURCES"
	positionStyleEnv          = "FUSIONN_POSITION_STYLE"
	positionWordsEnv          = "FUSIONN_POSITION_WORDS"
	positionBaselineEnv       = "FUSIONN_POSITION_BASELINE_MAPPING"
	positionAssEnv            = "FUSIONN_POSITION_ASS"
)

type recordManifestEntry struct {
	Index  int    `json:"index"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
}

// TestReplayRecordedResponsesWithPositionFix performs an offline replay of recorded
// LLM responses through production decode, validation, and reconcile with the position fix.
//
// This test is explicit opt-in: it requires FUSIONN_POSITION_RECORD_DIR or
// FUSIONN_POSITION_RECORD_MANIFEST to be set. When omitted, it is skipped cleanly so
// standard repository tests remain completely portable and free of filesystem dependencies.
// When enabled, all inputs must exist and be valid or the test fails loudly.
func TestReplayRecordedResponsesWithPositionFix(t *testing.T) {
	dir := os.Getenv(positionRecordDirEnv)
	manifestPath := os.Getenv(positionRecordManifestEnv)
	if dir == "" && manifestPath == "" {
		t.Skipf("skipping recorded response replay: set %s or %s to run", positionRecordDirEnv, positionRecordManifestEnv)
	}

	if manifestPath == "" && dir != "" {
		cand := filepath.Join(dir, "recorded-responses", "manifest.json")
		if _, err := os.Stat(cand); err == nil {
			manifestPath = cand
		} else {
			cand2 := filepath.Join(dir, "manifest.json")
			if _, err2 := os.Stat(cand2); err2 == nil {
				manifestPath = cand2
			}
		}
	}
	if manifestPath == "" {
		t.Fatalf("%s or manifest file not found in %s", positionRecordManifestEnv, dir)
	}

	sourcesPath := os.Getenv(positionSourcesEnv)
	if sourcesPath == "" && dir != "" {
		sourcesPath = filepath.Join(dir, "sources.json")
	}
	if sourcesPath == "" {
		t.Fatalf("sources.json path required: set %s or %s", positionSourcesEnv, positionRecordDirEnv)
	}

	stylePath := os.Getenv(positionStyleEnv)
	if stylePath == "" && dir != "" {
		stylePath = filepath.Join(dir, "delivered-style.json")
	}
	if stylePath == "" {
		t.Fatalf("delivered-style.json path required: set %s or %s", positionStyleEnv, positionRecordDirEnv)
	}

	wordsPath := os.Getenv(positionWordsEnv)
	if wordsPath == "" && dir != "" {
		wordsPath = filepath.Join(dir, "word-timings.json")
	}

	baselinePath := os.Getenv(positionBaselineEnv)
	if baselinePath == "" && dir != "" {
		for _, cand := range []string{
			filepath.Join(dir, "baseline", "v12-mapping.json"),
			filepath.Join(dir, "baseline", "mapping.json"),
			filepath.Join(dir, "v12-mapping.json"),
			filepath.Join(dir, "baseline-mapping.json"),
		} {
			if _, err := os.Stat(cand); err == nil {
				baselinePath = cand
				break
			}
		}
	}

	assPath := os.Getenv(positionAssEnv)
	if assPath == "" && dir != "" {
		assPath = filepath.Join(dir, "E01.zh20-bilingual-layout.ass")
	}

	rawBundle, err := os.ReadFile(sourcesPath)
	if err != nil {
		t.Fatalf("read sources from %s: %v", sourcesPath, err)
	}
	var bundle Bundle
	if err := json.Unmarshal(rawBundle, &bundle); err != nil {
		t.Fatalf("unmarshal bundle from %s: %v", sourcesPath, err)
	}

	rawManifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest from %s: %v", manifestPath, err)
	}
	var manifestEntries []recordManifestEntry
	if err := json.Unmarshal(rawManifest, &manifestEntries); err != nil {
		t.Fatalf("unmarshal manifest from %s: %v", manifestPath, err)
	}

	windows := buildWindows(bundle, 210000, 15000)
	if len(windows) != len(manifestEntries) {
		t.Fatalf("windows count %d != manifest entries count %d", len(windows), len(manifestEntries))
	}

	batches := make([]batch, 0, len(windows))
	manifestDir := filepath.Dir(manifestPath)
	audit := Audit{
		Rejected:       []RejectedRow{},
		Conflicts:      []Group{},
		Restored:       map[string][]int{"en": {}, "zh": {}},
		WindowMissing:  []WindowMissing{},
		CacheCorrupted: []CacheCorruption{},
		Coverage:       Coverage{EN: len(bundle.EN), ZH: len(bundle.ZH)},
		CacheHits:      len(windows),
		ResolvedModels: []string{"qfmodel"},
		Provenance: &Provenance{
			Source:            "native",
			ConfiguredModel:   "group/auto-qfmodel",
			SourceFingerprint: bundle.Fingerprint,
		},
	}

	for i, w := range windows {
		entry := manifestEntries[i]
		if entry.Index != w.Index {
			t.Fatalf("manifest entry index %d != window index %d", entry.Index, w.Index)
		}

		recPath := entry.Path
		if !filepath.IsAbs(recPath) {
			recPath = filepath.Join(manifestDir, recPath)
		}
		rawCache, err := os.ReadFile(recPath)
		if err != nil {
			t.Fatalf("read record %s: %v", recPath, err)
		}

		hash := fmt.Sprintf("%x", sha256.Sum256(rawCache))
		if hash != entry.SHA256 {
			t.Fatalf("record %s sha256 changed: got %s, want %s", filepath.Base(recPath), hash, entry.SHA256)
		}

		var rec cacheRecord
		if err := json.Unmarshal(rawCache, &rec); err != nil {
			t.Fatalf("decode record %s: %v", filepath.Base(recPath), err)
		}

		// Validate that the recorded request strictly matches window Index and cue IDs.
		var reqPayload struct {
			Messages []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.Unmarshal(rec.Request, &reqPayload); err != nil {
			t.Fatalf("window %d unmarshal request payload: %v", i, err)
		}
		var userContent string
		for _, m := range reqPayload.Messages {
			if m.Role == "user" {
				userContent = m.Content
				break
			}
		}
		if userContent == "" {
			t.Fatalf("window %d missing user prompt in recorded request", i)
		}
		var reqView windowView
		if err := json.Unmarshal([]byte(userContent), &reqView); err != nil {
			t.Fatalf("window %d unmarshal user view: %v", i, err)
		}
		if len(reqView.CoreMS) != 2 || reqView.CoreMS[0] != w.CoreStart || reqView.CoreMS[1] != w.CoreEnd {
			t.Fatalf("window %d core_ms mismatch: got %v, want [%d, %d]", i, reqView.CoreMS, w.CoreStart, w.CoreEnd)
		}
		if len(reqView.EN) != len(w.EN) {
			t.Fatalf("window %d EN cue count mismatch: got %d, want %d", i, len(reqView.EN), len(w.EN))
		}
		for ci, cue := range w.EN {
			if reqView.EN[ci].ID != cue.ID {
				t.Fatalf("window %d EN cue %d ID mismatch: got %d, want %d", i, ci, reqView.EN[ci].ID, cue.ID)
			}
		}
		if len(reqView.ZH) != len(w.ZH) {
			t.Fatalf("window %d ZH cue count mismatch: got %d, want %d", i, len(reqView.ZH), len(w.ZH))
		}
		for ci, cue := range w.ZH {
			if reqView.ZH[ci].ID != cue.ID {
				t.Fatalf("window %d ZH cue %d ID mismatch: got %d, want %d", i, ci, reqView.ZH[ci].ID, cue.ID)
			}
		}

		valid, rejected, err := validateResponse(rec.Response, w, MaxDistanceMS)
		if err != nil {
			t.Fatalf("window %d validateResponse failed: %v", i, err)
		}
		batches = append(batches, batch{Index: w.Index, Valid: valid, Rejected: rejected, Missing: missingWindow(w, valid)})
	}

	for _, b := range batches {
		audit.Rejected = append(audit.Rejected, b.Rejected...)
		audit.WindowMissing = append(audit.WindowMissing, b.Missing)
	}

	groups, reconcileAudit := reconcile(bundle, batches, 210000)
	audit.Conflicts = reconcileAudit.Conflicts
	audit.ContextDuplicates = reconcileAudit.ContextDuplicates
	audit.Restored = reconcileAudit.Restored

	mapping := Mapping{
		Version:           Version,
		SourceFingerprint: bundle.Fingerprint,
		Groups:            groups,
	}
	fingerprint, err := mappingDigest(mapping)
	if err != nil {
		t.Fatalf("mappingDigest failed: %v", err)
	}
	mapping.Fingerprint = fingerprint

	// Assertions on the fix:
	// 1. Rejected rows must contain ZERO native_position_conflict
	for _, r := range audit.Rejected {
		if r.Reason == "native_position_conflict" {
			t.Fatalf("native_position_conflict still present: %+v", r)
		}
	}
	// Exactly 1 non-position rejection remains (group_span_over_30s in window 1)
	if len(audit.Rejected) != 1 {
		t.Fatalf("expected exactly 1 rejected row, got %d: %+v", len(audit.Rejected), audit.Rejected)
	}
	if audit.Rejected[0].Reason != "group_span_over_30s" {
		t.Fatalf("expected group_span_over_30s, got %s", audit.Rejected[0].Reason)
	}

	// 2. Source cue layout position metadata preserved intact on all cues
	var freshBundle Bundle
	if err := json.Unmarshal(rawBundle, &freshBundle); err != nil {
		t.Fatal(err)
	}
	for idx, c := range bundle.EN {
		orig := freshBundle.EN[idx]
		if c.Top != orig.Top || c.Alignment != orig.Alignment {
			t.Fatalf("EN cue %d position metadata mutated: top %v->%v, align %d->%d", c.ID, orig.Top, c.Top, orig.Alignment, c.Alignment)
		}
	}
	for idx, c := range bundle.ZH {
		orig := freshBundle.ZH[idx]
		if c.Top != orig.Top || c.Alignment != orig.Alignment {
			t.Fatalf("ZH cue %d position metadata mutated: top %v->%v, align %d->%d", c.ID, orig.Top, c.Top, orig.Alignment, c.Alignment)
		}
	}

	// 3. Every source ID must appear exactly once in accepted mapping groups
	enCounts := map[int]int{}
	zhCounts := map[int]int{}
	for _, g := range mapping.Groups {
		for _, id := range g.ENIDs {
			enCounts[id]++
		}
		for _, id := range g.ZHIDs {
			zhCounts[id]++
		}
	}
	for _, c := range bundle.EN {
		if count := enCounts[c.ID]; count != 1 {
			t.Fatalf("source EN cue %d appears %d times in mapping.Groups (want 1)", c.ID, count)
		}
	}
	for _, c := range bundle.ZH {
		if count := zhCounts[c.ID]; count != 1 {
			t.Fatalf("source ZH cue %d appears %d times in mapping.Groups (want 1)", c.ID, count)
		}
	}

	// 4. All 7 candidate groups must be successfully accepted into mapping.Groups:
	expectedPairs := []struct {
		en []int
		zh []int
	}{
		{en: []int{25, 26}, zh: []int{26}},
		{en: []int{112, 113}, zh: []int{91}},
		{en: []int{116, 117}, zh: []int{94}},
		{en: []int{133, 134}, zh: []int{108}},
		{en: []int{137}, zh: []int{113}},
		{en: []int{138}, zh: []int{114}},
		{en: []int{194}, zh: []int{168}},
	}

	groupKey := func(en, zh []int) string {
		return fmt.Sprintf("en:%v_zh:%v", en, zh)
	}
	groupMap := map[string]Group{}
	for _, g := range mapping.Groups {
		groupMap[groupKey(g.ENIDs, g.ZHIDs)] = g
	}

	for _, ep := range expectedPairs {
		key := groupKey(ep.en, ep.zh)
		g, ok := groupMap[key]
		if !ok {
			t.Errorf("candidate group %s not found in accepted mapping groups", key)
			continue
		}
		if g.Origin != OriginModel {
			t.Errorf("group %s origin = %s, want model", key, g.Origin)
		}
	}

	// 5. None of the 18 cues should be in restored single-sided list
	restoredEN := map[int]bool{}
	for _, id := range audit.Restored["en"] {
		restoredEN[id] = true
	}
	restoredZH := map[int]bool{}
	for _, id := range audit.Restored["zh"] {
		restoredZH[id] = true
	}
	for _, ep := range expectedPairs {
		for _, id := range ep.en {
			if restoredEN[id] {
				t.Errorf("EN cue %d was unexpectedly restored as single-sided", id)
			}
		}
		for _, id := range ep.zh {
			if restoredZH[id] {
				t.Errorf("ZH cue %d was unexpectedly restored as single-sided", id)
			}
		}
	}

	// 6. Proving by calculation: 7 groups recovered and all remaining groups unchanged against baseline
	if baselinePath != "" {
		rawBaseline, err := os.ReadFile(baselinePath)
		if err != nil {
			t.Fatalf("read baseline mapping from %s: %v", baselinePath, err)
		}
		var baselineMapping Mapping
		if err := json.Unmarshal(rawBaseline, &baselineMapping); err != nil {
			t.Fatalf("unmarshal baseline mapping from %s: %v", baselinePath, err)
		}

		removedRestoredKeys := map[string]bool{}
		for _, ep := range expectedPairs {
			for _, id := range ep.en {
				removedRestoredKeys[groupKey([]int{id}, []int{})] = true
			}
			for _, id := range ep.zh {
				removedRestoredKeys[groupKey([]int{}, []int{id})] = true
			}
		}
		if len(removedRestoredKeys) != 18 {
			t.Fatalf("expected 18 single-sided keys to remove, got %d", len(removedRestoredKeys))
		}

		addedModelKeys := map[string]bool{}
		for _, ep := range expectedPairs {
			addedModelKeys[groupKey(ep.en, ep.zh)] = true
		}
		if len(addedModelKeys) != 7 {
			t.Fatalf("expected 7 model keys to add, got %d", len(addedModelKeys))
		}

		baselineMap := map[string]Group{}
		for _, g := range baselineMapping.Groups {
			baselineMap[groupKey(g.ENIDs, g.ZHIDs)] = g
		}

		normKind := func(k string) string {
			if k == KindUnknown {
				return ""
			}
			return k
		}
		for key, bg := range baselineMap {
			if removedRestoredKeys[key] {
				if _, exists := groupMap[key]; exists {
					t.Fatalf("baseline restored group %s still exists in new mapping", key)
				}
			} else {
				ng, exists := groupMap[key]
				if !exists {
					t.Fatalf("baseline group %s missing from new mapping", key)
				}
				if ng.Layer != bg.Layer || ng.Origin != bg.Origin || normKind(ng.Kind) != normKind(bg.Kind) {
					t.Fatalf("group %s modified: new=%+v baseline=%+v", key, ng, bg)
				}
			}
		}

		for key, ng := range groupMap {
			if addedModelKeys[key] {
				if _, exists := baselineMap[key]; exists {
					t.Fatalf("recovered model group %s was already in baseline mapping", key)
				}
			} else {
				bg, exists := baselineMap[key]
				if !exists {
					t.Fatalf("new group %s not found in baseline mapping", key)
				}
				if ng.Layer != bg.Layer || ng.Origin != bg.Origin || normKind(ng.Kind) != normKind(bg.Kind) {
					t.Fatalf("group %s modified: new=%+v baseline=%+v", key, ng, bg)
				}
			}
		}

		if len(mapping.Groups) != len(baselineMapping.Groups)-18+7 {
			t.Fatalf("group count mismatch: got %d, want baseline %d - 18 + 7 = %d",
				len(mapping.Groups), len(baselineMapping.Groups), len(baselineMapping.Groups)-11)
		}
	}

	// 7. Whole episode validation with rendered ASS and Verify()
	var words WordTimings
	if wordsPath != "" {
		if rawWords, err := os.ReadFile(wordsPath); err == nil {
			_ = json.Unmarshal(rawWords, &words)
		}
	}

	rawStyle, err := os.ReadFile(stylePath)
	if err != nil {
		t.Fatalf("read style from %s: %v", stylePath, err)
	}
	var style config.ASSStyleConfig
	if err := json.Unmarshal(rawStyle, &style); err != nil {
		t.Fatalf("unmarshal style: %v", err)
	}

	width, height := 3840, 1632
	v, events, err := validateEpisode(bundle, mapping, fixtureConfig(), style, width, height, words)
	if err != nil {
		t.Fatalf("validateEpisode failed: %v", err)
	}

	failures := v.Verify()
	if len(failures) != 0 {
		for _, f := range failures {
			t.Errorf("validation failure: %s", f)
		}
		t.Fatalf("%d validation failures in validateEpisode", len(failures))
	}

	if assPath != "" {
		if rawASS, err := os.ReadFile(assPath); err == nil {
			checkSerializedASS(t, string(rawASS), events)
			rerendered, err := RenderWithWordTimings(bundle, mapping, fixtureConfig(), style, width, height, words)
			if err != nil {
				t.Fatalf("RenderWithWordTimings failed: %v", err)
			}
			if string(rerendered) != string(rawASS) {
				t.Fatalf("re-rendered ASS does not match delivered ASS %s byte-for-byte", assPath)
			}
		}
	}

	t.Logf("Replayed mapping groups: %d, rejected: %d, conflicts: %d, restored EN: %d, restored ZH: %d",
		len(mapping.Groups), len(audit.Rejected), len(audit.Conflicts), len(audit.Restored["en"]), len(audit.Restored["zh"]))
}
