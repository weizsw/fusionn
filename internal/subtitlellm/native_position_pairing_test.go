package subtitlellm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
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
