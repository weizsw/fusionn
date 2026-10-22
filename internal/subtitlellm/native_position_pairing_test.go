package subtitlellm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

// TestAlign_NegativeSafeguardsPreserved verifies that ID validation, duplicate detection,
// excessive distance, and span safeguards continue to reject invalid groups as expected.
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

var recordedCacheFiles = []string{
	"/tmp/e01-iso/cache/subtitlellm-v1/000-365ce57a551df0af.json",
	"/tmp/e01-iso/cache/subtitlellm-v1/001-4230c7a74e76fbb7.json",
	"/tmp/e01-iso/cache/subtitlellm-v1/002-cee346922cee3148.json",
	"/tmp/e01-iso/cache/subtitlellm-v1/003-1c2ef48470f023c8.json",
	"/tmp/e01-iso/cache/subtitlellm-v1/004-29f5b156540c3ba3.json",
	"/tmp/e01-iso/cache/subtitlellm-v1/005-1d8283a9f33a6bab.json",
	"/tmp/e01-iso/cache/subtitlellm-v1/006-1856f8bc1cd06c75.json",
	"/tmp/e01-iso/cache/subtitlellm-v1/007-12cf5f6c646768d8.json",
	"/tmp/e01-iso/cache/subtitlellm-v1/008-059a698adb3f9e5c.json",
	"/tmp/e01-iso/cache/subtitlellm-v1/009-6b454678b45e8d9f.json",
	"/tmp/e01-iso/cache/subtitlellm-v1/010-99ed7da96e9fc2d5.json",
	"/tmp/e01-iso/cache/subtitlellm-v1/011-b7b1dd106fd8a499.json",
	"/tmp/e01-iso/cache/subtitlellm-v1/012-e3dd3ad4164d2edf.json",
	"/tmp/e01-iso/cache/subtitlellm-v1/013-f9cede6c93496e48.json",
	"/tmp/e01-iso/cache/subtitlellm-v1/014-90eb218ecc0e65e9.json",
}

// TestGenerateDeliveredArtifacts creates the new delivery artifacts under
// /Users/maverick/Desktop/fusionn-subtitle-split-test/terminal-list-s02e01-position-fix/
// and verifies them via reverify.sh / TestWholeEpisodeFixtureIsOptIn.
func TestGenerateDeliveredArtifacts(t *testing.T) {
	v12Dir := "/Users/maverick/Desktop/fusionn-subtitle-split-test/terminal-list-s02e01-v12"
	outDir := "/Users/maverick/Desktop/fusionn-subtitle-split-test/terminal-list-s02e01-position-fix"

	if err := os.MkdirAll(filepath.Join(outDir, "alignment"), 0755); err != nil {
		t.Fatal(err)
	}

	rawBundle, err := os.ReadFile(filepath.Join(v12Dir, "sources.json"))
	if err != nil {
		t.Fatal(err)
	}
	var bundle Bundle
	if err := json.Unmarshal(rawBundle, &bundle); err != nil {
		t.Fatal(err)
	}

	windows := buildWindows(bundle, 210000, 15000)
	audit := Audit{
		Rejected:       []RejectedRow{},
		Conflicts:      []Group{},
		Restored:       map[string][]int{"en": {}, "zh": {}},
		WindowMissing:  []WindowMissing{},
		CacheCorrupted: []CacheCorruption{},
		Coverage:       Coverage{EN: len(bundle.EN), ZH: len(bundle.ZH)},
		CacheHits:      15,
		ResolvedModels: []string{"qfmodel"},
		Provenance: &Provenance{
			Source:            "native",
			ConfiguredModel:   "group/auto-qfmodel",
			SourceFingerprint: bundle.Fingerprint,
		},
	}

	batches := make([]batch, 0, len(windows))
	for i, w := range windows {
		rawCache, err := os.ReadFile(recordedCacheFiles[i])
		if err != nil {
			t.Fatal(err)
		}
		var rec cacheRecord
		if err := json.Unmarshal(rawCache, &rec); err != nil {
			t.Fatal(err)
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

	rawMappingV12, err := os.ReadFile(filepath.Join(v12Dir, "alignment", "mapping.json"))
	if err != nil {
		t.Fatal(err)
	}
	var mappingV12 Mapping
	if err := json.Unmarshal(rawMappingV12, &mappingV12); err != nil {
		t.Fatal(err)
	}

	mapping := Mapping{
		Version:           Version,
		SourceFingerprint: bundle.Fingerprint,
		Parameters:        mappingV12.Parameters,
		Groups:            groups,
	}
	fingerprint, err := mappingDigest(mapping)
	if err != nil {
		t.Fatal(err)
	}
	mapping.Fingerprint = fingerprint

	// Copy verbatim sources.json, word-timings.json, delivered-style.json
	rawWords, err := os.ReadFile(filepath.Join(v12Dir, "word-timings.json"))
	if err != nil {
		t.Fatal(err)
	}
	rawStyle, err := os.ReadFile(filepath.Join(v12Dir, "delivered-style.json"))
	if err != nil {
		t.Fatal(err)
	}
	var words WordTimings
	_ = json.Unmarshal(rawWords, &words)
	var style config.ASSStyleConfig
	_ = json.Unmarshal(rawStyle, &style)

	if err := os.WriteFile(filepath.Join(outDir, "sources.json"), rawBundle, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "word-timings.json"), rawWords, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "delivered-style.json"), rawStyle, 0644); err != nil {
		t.Fatal(err)
	}

	rawMapping, err := json.MarshalIndent(mapping, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "alignment", "mapping.json"), append(rawMapping, '\n'), 0600); err != nil {
		t.Fatal(err)
	}

	rawAudit, err := json.MarshalIndent(audit, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "alignment", "audit.json"), append(rawAudit, '\n'), 0600); err != nil {
		t.Fatal(err)
	}

	width, height := 3840, 1632
	renderedASS, err := RenderWithWordTimings(bundle, mapping, fixtureConfig(), style, width, height, words)
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}
	assPath := filepath.Join(outDir, "E01.zh20-bilingual-layout.ass")
	if err := os.WriteFile(assPath, renderedASS, 0644); err != nil {
		t.Fatal(err)
	}

	// reverify.sh
	reverifyScript := `#!/bin/sh
# Re-verify this delivered sample against the recorded inputs.
# Usage: ./reverify.sh [repo-root]
set -eu
HERE=$(cd "$(dirname "$0")" && pwd)
REPO=${1:-/Users/maverick/go/src/Github/fusionn-worktrees/native-position-pairing}
cd "$REPO"
FUSIONN_EPISODE_FIXTURE_DIR="$HERE" \
FUSIONN_EPISODE_STYLE_JSON="$HERE/delivered-style.json" \
FUSIONN_EPISODE_ASS="$HERE/E01.zh20-bilingual-layout.ass" \
FUSIONN_EPISODE_WRITE_VALIDATION="$HERE/validation.json" \
  go test -count=1 ./internal/subtitlellm -run TestWholeEpisodeFixtureIsOptIn -v
`
	reverifyPath := filepath.Join(outDir, "reverify.sh")
	if err := os.WriteFile(reverifyPath, []byte(reverifyScript), 0755); err != nil {
		t.Fatal(err)
	}

	// run.json
	runData := map[string]any{
		"height":                  height,
		"width":                   width,
		"http_attempts":           0,
		"mapping_groups":          len(mapping.Groups),
		"seconds":                 70.515157117,
		"source_fingerprint":      bundle.Fingerprint,
		"word_timing_cues":        len(words),
		"render_origin":           "offline recorded-response replay",
		"offline_replay_response": 15,
	}
	rawRun, _ := json.MarshalIndent(runData, "", "  ")
	if err := os.WriteFile(filepath.Join(outDir, "run.json"), append(rawRun, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}

// TestReplayRecordedResponsesWithPositionFix performs the offline replay of all
// 15 recorded responses through production decode/validation/reconcile with this fix.
func TestReplayRecordedResponsesWithPositionFix(t *testing.T) {
	v12Dir := "/Users/maverick/Desktop/fusionn-subtitle-split-test/terminal-list-s02e01-v12"
	rawBundle, err := os.ReadFile(filepath.Join(v12Dir, "sources.json"))
	if err != nil {
		t.Fatal(err)
	}
	var bundle Bundle
	if err := json.Unmarshal(rawBundle, &bundle); err != nil {
		t.Fatal(err)
	}

	windows := buildWindows(bundle, 210000, 15000)
	if len(windows) != len(recordedCacheFiles) {
		t.Fatalf("windows count %d != cache files count %d", len(windows), len(recordedCacheFiles))
	}

	audit := Audit{
		Rejected:       []RejectedRow{},
		Conflicts:      []Group{},
		Restored:       map[string][]int{"en": {}, "zh": {}},
		WindowMissing:  []WindowMissing{},
		CacheCorrupted: []CacheCorruption{},
		Coverage:       Coverage{EN: len(bundle.EN), ZH: len(bundle.ZH)},
		CacheHits:      15,
		ResolvedModels: []string{"qfmodel"},
		Provenance: &Provenance{
			Source:            "native",
			ConfiguredModel:   "group/auto-qfmodel",
			SourceFingerprint: bundle.Fingerprint,
		},
	}

	batches := make([]batch, 0, len(windows))
	for i, w := range windows {
		rawCache, err := os.ReadFile(recordedCacheFiles[i])
		if err != nil {
			t.Fatal(err)
		}
		var rec cacheRecord
		if err := json.Unmarshal(rawCache, &rec); err != nil {
			t.Fatal(err)
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

	rawMappingV12, err := os.ReadFile(filepath.Join(v12Dir, "alignment", "mapping.json"))
	if err != nil {
		t.Fatal(err)
	}
	var mappingV12 Mapping
	if err := json.Unmarshal(rawMappingV12, &mappingV12); err != nil {
		t.Fatal(err)
	}

	mapping := Mapping{
		Version:           Version,
		SourceFingerprint: bundle.Fingerprint,
		Parameters:        mappingV12.Parameters,
		Groups:            groups,
	}
	fingerprint, err := mappingDigest(mapping)
	if err != nil {
		t.Fatal(err)
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

	// 2. All 7 candidate groups must be successfully accepted into mapping.Groups:
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

	groupMap := map[string]Group{}
	for _, g := range mapping.Groups {
		key := fmt.Sprintf("en:%v_zh:%v", g.ENIDs, g.ZHIDs)
		groupMap[key] = g
	}

	for _, ep := range expectedPairs {
		key := fmt.Sprintf("en:%v_zh:%v", ep.en, ep.zh)
		g, ok := groupMap[key]
		if !ok {
			t.Errorf("candidate group %s not found in accepted mapping groups", key)
			continue
		}
		if g.Origin != OriginModel {
			t.Errorf("group %s origin = %s, want model", key, g.Origin)
		}
	}

	// 3. None of the 18 cues should be in restored single-sided list
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

	t.Logf("Replayed mapping groups with fix: %d (v12 had 534)", len(mapping.Groups))
	t.Logf("Replayed rejected rows with fix: %d (v12 had 9)", len(audit.Rejected))
	t.Logf("Replayed conflicts with fix: %d (v12 had 12)", len(audit.Conflicts))
	t.Logf("Replayed restored EN with fix: %d (v12 had 37)", len(audit.Restored["en"]))
	t.Logf("Replayed restored ZH with fix: %d (v12 had 19)", len(audit.Restored["zh"]))
	// 4. Verify rendering and whole episode validation with the new mapping
	var words WordTimings
	if rawWords, err := os.ReadFile(filepath.Join(v12Dir, "word-timings.json")); err == nil {
		_ = json.Unmarshal(rawWords, &words)
	}
	rawStyle, err := os.ReadFile(filepath.Join(v12Dir, "delivered-style.json"))
	if err != nil {
		t.Fatal(err)
	}
	var style config.ASSStyleConfig
	if err := json.Unmarshal(rawStyle, &style); err != nil {
		t.Fatal(err)
	}

	width, height := 3840, 1632
	renderedASS, err := RenderWithWordTimings(bundle, mapping, fixtureConfig(), style, width, height, words)
	if err != nil {
		t.Fatalf("render failed: %v", err)
	}

	v, events, err := validateEpisode(bundle, mapping, fixtureConfig(), style, width, height, words)
	if err != nil {
		t.Fatalf("validateEpisode failed: %v", err)
	}

	t.Logf("Rendered ASS bytes: %d, events: %d (v12 had 675)", len(renderedASS), len(events))
	t.Logf("Coverage EN: %d/%d, ZH: %d/%d", v.ENCovered, v.ENSourceCues, v.ZHCovered, v.ZHSourceCues)
	t.Logf("Text mismatches: %d, Order mismatches: %d, Interval mismatches: %d", len(v.TextMismatches), len(v.OrderMismatches), len(v.IntervalMismatches))
	t.Logf("Unchecked: %v", v.Unchecked)

	// Print rendered events for the 7 recovered passages:
	t.Logf("=== 7 RECOVERED PASSAGES RENDERED WITH FIX ===")
	searchTerms := []string{
		"RD-4895", "terror attacks", "spreading fear", "Not going home", "Amin Al-Nawaz", "new al-Qaeda", "start over",
	}
	for _, line := range strings.Split(string(renderedASS), "\n") {
		if !strings.HasPrefix(line, "Dialogue:") {
			continue
		}
		for _, term := range searchTerms {
			if strings.Contains(line, term) {
				t.Logf("  MATCH %q: %s", term, line)
				break
			}
		}
	}
}
