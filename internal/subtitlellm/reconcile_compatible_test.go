package subtitlellm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// makeCompatibleTestCues creates test cues using generic bilingual sentences
// with arbitrary IDs and shifted times, containing no real show names, lines, or IDs.
func makeCompatibleTestCues() ([]Cue, []Cue) {
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
	en := []Cue{
		cue(1, 98000, 99000, "First clause of speech."),
		cue(2, 99000, 100000, "Second clause continues."),
		cue(3, 101000, 102000, "Next sentence starts here."),
	}
	zh := []Cue{
		cue(1, 98000, 100000, "第一句的前半部分和后半部分。"),
		cue(2, 101000, 102000, "下一句话从这里开始。"),
	}
	return en, zh
}

// TestEngine_CompatibleRefinementAcrossCores_Behavior1 verifies Behavior 1 from
// /tmp/fusionn-compatible-refinement-design-20261022.md:
// Base time setup: core=100s, overlap=30s.
// EN[101] 98–99s, EN[102] 99–100s, EN[103] 101–102s; ZH[201] 98–100s, ZH[202] 101–102s.
// Windows 0 and 1 both fully receive these 5 cues.
// Window 0 response: [101, 102] <-> [201], [103] <-> [202] (fine partition).
// Window 1 response: [101, 102, 103] <-> [201, 202] (coarse group).
// Required outcome:
// Exactly preserve the two bilingual fine groups, each member used once, component has 0 restored / 0 conflict.
// Second fine group has logical owner = 1 (although its original supporting response came from Window 0).
// Source fingerprint unchanged, cannot invent other pairings.
func TestEngine_CompatibleRefinementAcrossCores_Behavior1(t *testing.T) {
	en, zh := makeCompatibleTestCues()
	bundle := testBundle(t, en, zh)

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var p requestPayload
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			t.Errorf("decode request: %v", err)
			w.WriteHeader(500)
			return
		}
		if len(p.Messages) < 2 {
			t.Error("missing source view message")
			w.WriteHeader(500)
			return
		}
		var view windowView
		if err := json.Unmarshal([]byte(p.Messages[1].Content), &view); err != nil {
			t.Errorf("unmarshal source view: %v", err)
			w.WriteHeader(500)
			return
		}

		// Verify that both windows 0 and 1 fully received all 5 cues (3 EN, 2 ZH).
		if len(view.EN) != 3 || len(view.ZH) != 2 {
			t.Errorf("window core %v received EN=%d ZH=%d, expected EN=3 ZH=2", view.CoreMS, len(view.EN), len(view.ZH))
			w.WriteHeader(500)
			return
		}

		var content string
		if view.CoreMS[0] == 0 {
			// Window 0: fine partition across core boundary
			content = `{"alignments":[{"en_ids":[1,2],"zh_ids":[1],"layer":"bottom","kind":"dialogue"},{"en_ids":[3],"zh_ids":[2],"layer":"bottom","kind":"dialogue"}]}`
		} else {
			// Window 1: coarse group covering all 5 cues
			content = `{"alignments":[{"en_ids":[1,2,3],"zh_ids":[1,2],"layer":"bottom","kind":"dialogue"}]}`
		}

		if err := json.NewEncoder(w).Encode(map[string]any{
			"model": "mock",
			"choices": []any{
				map[string]any{"finish_reason": "stop", "message": map[string]any{"content": content}},
			},
		}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer server.Close()

	cfg := testConfig(t, server.URL+"/v1")
	cfg.WindowSeconds = 100
	cfg.OverlapSeconds = 30
	cfg.Retries = 0
	engine := NewEngine()

	mapping, audit, err := engine.Align(context.Background(), bundle, cfg)
	if err != nil {
		t.Fatalf("Align failed: %v", err)
	}

	if len(audit.Conflicts) != 0 {
		t.Fatalf("expected 0 conflicts, got %d: %+v", len(audit.Conflicts), audit.Conflicts)
	}
	if len(audit.Restored["en"]) != 0 || len(audit.Restored["zh"]) != 0 {
		t.Fatalf("expected 0 restored cues, got EN=%v ZH=%v", audit.Restored["en"], audit.Restored["zh"])
	}
	if len(mapping.Groups) != 2 {
		t.Fatalf("expected exactly 2 fine groups, got %d: %+v", len(mapping.Groups), mapping.Groups)
	}

	// Verify group 1: [1, 2] <-> [1], owner = 0
	g1 := mapping.Groups[0]
	if !reflect.DeepEqual(g1.ENIDs, []int{1, 2}) || !reflect.DeepEqual(g1.ZHIDs, []int{1}) {
		t.Errorf("expected group 1 to be [1, 2] <-> [1], got EN=%v ZH=%v", g1.ENIDs, g1.ZHIDs)
	}
	if g1.OwnerWindow != 0 {
		t.Errorf("expected group 1 owner window 0, got %d", g1.OwnerWindow)
	}
	if g1.Origin != OriginModel {
		t.Errorf("expected group 1 origin %q, got %q", OriginModel, g1.Origin)
	}

	// Verify group 2: [3] <-> [2], logical owner = 1 (even though proposed in window 0 response)
	g2 := mapping.Groups[1]
	if !reflect.DeepEqual(g2.ENIDs, []int{3}) || !reflect.DeepEqual(g2.ZHIDs, []int{2}) {
		t.Errorf("expected group 2 to be [3] <-> [2], got EN=%v ZH=%v", g2.ENIDs, g2.ZHIDs)
	}
	if g2.OwnerWindow != 1 {
		t.Errorf("expected group 2 logical owner window 1, got %d", g2.OwnerWindow)
	}
	if g2.Origin != OriginModel {
		t.Errorf("expected group 2 origin %q, got %q", OriginModel, g2.Origin)
	}

	if mapping.SourceFingerprint != bundle.Fingerprint {
		t.Errorf("source fingerprint mismatch: got %q, expected %q", mapping.SourceFingerprint, bundle.Fingerprint)
	}
}

// TestEngine_CompatibleRefinement_Behavior2_ReverseAndOrderStability verifies Behavior 2:
// 1. Swap which complete window proposes coarse vs fine division.
// 2. Propose groups inside the response in reversed order.
// 3. Control response latency so requests finish out-of-order.
// 4. Time-shift cues across later core windows (cores 5 and 6).
// Result: exactly the same member partition and correct logical owners / time order,
// with no jitter or permutation caused by response arrival order.
func TestEngine_CompatibleRefinement_Behavior2_ReverseAndOrderStability(t *testing.T) {
	t.Run("SwappedWindowAndReversedGroupOrderAndTiming", func(t *testing.T) {
		en, zh := makeCompatibleTestCues()
		bundle := testBundle(t, en, zh)

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var p requestPayload
			if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
				t.Errorf("decode request: %v", err)
				w.WriteHeader(500)
				return
			}
			var view windowView
			if err := json.Unmarshal([]byte(p.Messages[1].Content), &view); err != nil {
				t.Errorf("unmarshal source view: %v", err)
				w.WriteHeader(500)
				return
			}

			var content string
			if view.CoreMS[0] == 0 {
				// Window 0: proposes coarse group, with artificial delay
				time.Sleep(20 * time.Millisecond)
				content = `{"alignments":[{"en_ids":[1,2,3],"zh_ids":[1,2],"layer":"bottom","kind":"dialogue"}]}`
			} else {
				// Window 1: proposes fine groups in REVERSED group order ([3]<->[2] first)
				content = `{"alignments":[{"en_ids":[3],"zh_ids":[2],"layer":"bottom","kind":"dialogue"},{"en_ids":[1,2],"zh_ids":[1],"layer":"bottom","kind":"dialogue"}]}`
			}

			if err := json.NewEncoder(w).Encode(map[string]any{
				"model": "mock",
				"choices": []any{
					map[string]any{"finish_reason": "stop", "message": map[string]any{"content": content}},
				},
			}); err != nil {
				t.Errorf("encode response: %v", err)
			}
		}))
		defer server.Close()

		cfg := testConfig(t, server.URL+"/v1")
		cfg.WindowSeconds = 100
		cfg.OverlapSeconds = 30
		cfg.Retries = 0
		engine := NewEngine()

		mapping, audit, err := engine.Align(context.Background(), bundle, cfg)
		if err != nil {
			t.Fatalf("Align failed: %v", err)
		}

		if len(audit.Conflicts) != 0 {
			t.Fatalf("expected 0 conflicts, got %d: %+v", len(audit.Conflicts), audit.Conflicts)
		}
		if len(audit.Restored["en"]) != 0 || len(audit.Restored["zh"]) != 0 {
			t.Fatalf("expected 0 restored cues, got EN=%v ZH=%v", audit.Restored["en"], audit.Restored["zh"])
		}
		if len(mapping.Groups) != 2 {
			t.Fatalf("expected exactly 2 fine groups, got %d: %+v", len(mapping.Groups), mapping.Groups)
		}

		// Verify order is strictly source-track order: group 1 first, group 2 second
		g1 := mapping.Groups[0]
		if !reflect.DeepEqual(g1.ENIDs, []int{1, 2}) || !reflect.DeepEqual(g1.ZHIDs, []int{1}) {
			t.Errorf("expected group 1 to be [1, 2] <-> [1], got EN=%v ZH=%v", g1.ENIDs, g1.ZHIDs)
		}
		if g1.OwnerWindow != 0 {
			t.Errorf("expected group 1 owner window 0, got %d", g1.OwnerWindow)
		}
		if g1.Origin != OriginModel {
			t.Errorf("expected group 1 origin %q, got %q", OriginModel, g1.Origin)
		}

		g2 := mapping.Groups[1]
		if !reflect.DeepEqual(g2.ENIDs, []int{3}) || !reflect.DeepEqual(g2.ZHIDs, []int{2}) {
			t.Errorf("expected group 2 to be [3] <-> [2], got EN=%v ZH=%v", g2.ENIDs, g2.ZHIDs)
		}
		if g2.OwnerWindow != 1 {
			t.Errorf("expected group 2 owner window 1, got %d", g2.OwnerWindow)
		}
		if g2.Origin != OriginModel {
			t.Errorf("expected group 2 origin %q, got %q", OriginModel, g2.Origin)
		}
	})

	t.Run("TimeShiftedAcrossLaterCores", func(t *testing.T) {
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
		// Time shifted to cores 5 and 6 (500s..600s, 600s..700s)
		en := []Cue{
			cue(1, 598000, 599000, "Shifted clause 1."),
			cue(2, 599000, 600000, "Shifted clause 2."),
			cue(3, 601000, 602000, "Shifted clause 3."),
		}
		zh := []Cue{
			cue(1, 598000, 600000, "平移第一句。"),
			cue(2, 601000, 602000, "平移第二句。"),
		}
		bundle := testBundle(t, en, zh)

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var p requestPayload
			if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
				t.Errorf("decode request: %v", err)
				w.WriteHeader(500)
				return
			}
			var view windowView
			if err := json.Unmarshal([]byte(p.Messages[1].Content), &view); err != nil {
				t.Errorf("unmarshal source view: %v", err)
				w.WriteHeader(500)
				return
			}

			var content string
			if view.CoreMS[0] == 500000 {
				content = `{"alignments":[{"en_ids":[1,2],"zh_ids":[1],"layer":"bottom","kind":"dialogue"},{"en_ids":[3],"zh_ids":[2],"layer":"bottom","kind":"dialogue"}]}`
			} else {
				content = `{"alignments":[{"en_ids":[1,2,3],"zh_ids":[1,2],"layer":"bottom","kind":"dialogue"}]}`
			}

			if err := json.NewEncoder(w).Encode(map[string]any{
				"model": "mock",
				"choices": []any{
					map[string]any{"finish_reason": "stop", "message": map[string]any{"content": content}},
				},
			}); err != nil {
				t.Errorf("encode response: %v", err)
			}
		}))
		defer server.Close()

		cfg := testConfig(t, server.URL+"/v1")
		cfg.WindowSeconds = 100
		cfg.OverlapSeconds = 30
		cfg.Retries = 0
		engine := NewEngine()

		mapping, audit, err := engine.Align(context.Background(), bundle, cfg)
		if err != nil {
			t.Fatalf("Align failed: %v", err)
		}

		if len(audit.Conflicts) != 0 {
			t.Fatalf("expected 0 conflicts, got %d: %+v", len(audit.Conflicts), audit.Conflicts)
		}
		if len(mapping.Groups) != 2 {
			t.Fatalf("expected exactly 2 fine groups, got %d: %+v", len(mapping.Groups), mapping.Groups)
		}

		// Logical owners must be 5 (for start 598s) and 6 (for start 601s)
		g1 := mapping.Groups[0]
		if g1.OwnerWindow != 5 {
			t.Errorf("expected group 1 owner window 5, got %d", g1.OwnerWindow)
		}
		g2 := mapping.Groups[1]
		if g2.OwnerWindow != 6 {
			t.Errorf("expected group 2 owner window 6, got %d", g2.OwnerWindow)
		}
	})
}

// TestEngine_CompatibleRefinement_Behavior3_GenuineCrossConflict verifies Behavior 3:
// Window 0: [1, 2] <-> [1] and [3] <-> [2]
// Window 1: [1] <-> [1] and [2, 3] <-> [2]
// This is a genuine cross-conflict over cue 2.
// Required outcome:
// Conflict must be isolated; cue 2 cannot be arbitrarily given to either side;
// no member can be reused; all source cues are preserved for single-sided restoration.
func TestEngine_CompatibleRefinement_Behavior3_GenuineCrossConflict(t *testing.T) {
	en, zh := makeCompatibleTestCues()
	bundle := testBundle(t, en, zh)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p requestPayload
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			t.Errorf("decode request: %v", err)
			w.WriteHeader(500)
			return
		}
		var view windowView
		if err := json.Unmarshal([]byte(p.Messages[1].Content), &view); err != nil {
			t.Errorf("unmarshal source view: %v", err)
			w.WriteHeader(500)
			return
		}

		var content string
		if view.CoreMS[0] == 0 {
			// Window 0: [1, 2] <-> [1] + [3] <-> [2]
			content = `{"alignments":[{"en_ids":[1,2],"zh_ids":[1],"layer":"bottom","kind":"dialogue"},{"en_ids":[3],"zh_ids":[2],"layer":"bottom","kind":"dialogue"}]}`
		} else {
			// Window 1: [1] <-> [1] + [2, 3] <-> [2] (crosses on EN 2)
			content = `{"alignments":[{"en_ids":[1],"zh_ids":[1],"layer":"bottom","kind":"dialogue"},{"en_ids":[2,3],"zh_ids":[2],"layer":"bottom","kind":"dialogue"}]}`
		}

		if err := json.NewEncoder(w).Encode(map[string]any{
			"model": "mock",
			"choices": []any{
				map[string]any{"finish_reason": "stop", "message": map[string]any{"content": content}},
			},
		}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer server.Close()

	cfg := testConfig(t, server.URL+"/v1")
	cfg.WindowSeconds = 100
	cfg.OverlapSeconds = 30
	cfg.Retries = 0
	engine := NewEngine()

	mapping, audit, err := engine.Align(context.Background(), bundle, cfg)
	if err != nil {
		t.Fatalf("Align failed: %v", err)
	}

	// Must preserve conflict isolation
	if len(audit.Conflicts) == 0 {
		t.Fatal("expected conflicts to be reported for genuine cross-partition, got 0")
	}

	// All 5 cues must be preserved for single-sided restoration
	if len(audit.Restored["en"]) != 3 || len(audit.Restored["zh"]) != 2 {
		t.Fatalf("expected all cues restored single-sided (EN=3 ZH=2), got EN=%v ZH=%v", audit.Restored["en"], audit.Restored["zh"])
	}
	// Restored cues may then pair by timing, but no model group survives.
	for _, g := range mapping.Groups {
		if g.Origin == OriginModel {
			t.Errorf("expected no model group after the conflict, got %+v", g)
		}
	}
	assertCoverage(t, mapping, bundle)
}

// TestEngine_CompatibleRefinement_Behavior4_IncompletePartitionsCannotDisguise verifies Behavior 4:
// A fine candidate cannot be certified as compatible refinement if:
// - It misses a visible member of the coarse group (EN or ZH).
// - It adds an external cue not in the coarse group.
// - Any fine group is only single-sided (EN-only or ZH-only).
func TestEngine_CompatibleRefinement_Behavior4_IncompletePartitionsCannotDisguise(t *testing.T) {
	runTest := func(t *testing.T, w0Content, w1Content string) {
		en, zh := makeCompatibleTestCues()
		bundle := testBundle(t, en, zh)

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var p requestPayload
			if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
				t.Errorf("decode request: %v", err)
				w.WriteHeader(500)
				return
			}
			var view windowView
			if err := json.Unmarshal([]byte(p.Messages[1].Content), &view); err != nil {
				t.Errorf("unmarshal source view: %v", err)
				w.WriteHeader(500)
				return
			}

			content := w0Content
			if view.CoreMS[0] != 0 {
				content = w1Content
			}

			if err := json.NewEncoder(w).Encode(map[string]any{
				"model": "mock",
				"choices": []any{
					map[string]any{"finish_reason": "stop", "message": map[string]any{"content": content}},
				},
			}); err != nil {
				t.Errorf("encode response: %v", err)
			}
		}))
		defer server.Close()

		cfg := testConfig(t, server.URL+"/v1")
		cfg.WindowSeconds = 100
		cfg.OverlapSeconds = 30
		cfg.Retries = 0
		engine := NewEngine()

		mapping, audit, err := engine.Align(context.Background(), bundle, cfg)
		if err != nil {
			t.Fatalf("Align failed: %v", err)
		}

		// Must NOT accept incomplete fine division as refinement
		for _, g := range mapping.Groups {
			if g.Origin == OriginModel && reflect.DeepEqual(g.ENIDs, []int{1, 2}) && reflect.DeepEqual(g.ZHIDs, []int{1}) {
				t.Errorf("fine group [1, 2] <-> [1] should not have been accepted under incomplete refinement: %+v", mapping.Groups)
			}
		}
		// All source content must be preserved via restoration/conflict
		totalCuesInGroups := 0
		for _, g := range mapping.Groups {
			totalCuesInGroups += len(g.ENIDs) + len(g.ZHIDs)
		}
		if totalCuesInGroups != 5 {
			t.Errorf("expected all 5 cues accounted for in groups, got %d", totalCuesInGroups)
		}
		if len(audit.Restored["en"])+len(audit.Restored["zh"]) == 0 && len(audit.Conflicts) == 0 {
			t.Error("expected conflicts or restorations for incomplete fine split")
		}
	}

	t.Run("OmittedENMember", func(t *testing.T) {
		// Window 0 misses EN 2: [1]<->[1] and [3]<->[2]
		w0 := `{"alignments":[{"en_ids":[1],"zh_ids":[1],"layer":"bottom","kind":"dialogue"},{"en_ids":[3],"zh_ids":[2],"layer":"bottom","kind":"dialogue"}]}`
		w1 := `{"alignments":[{"en_ids":[1,2,3],"zh_ids":[1,2],"layer":"bottom","kind":"dialogue"}]}`
		runTest(t, w0, w1)
	})

	t.Run("OmittedZHMember", func(t *testing.T) {
		// Window 0 misses ZH 2: [1,2]<->[1]
		w0 := `{"alignments":[{"en_ids":[1,2],"zh_ids":[1],"layer":"bottom","kind":"dialogue"}]}`
		w1 := `{"alignments":[{"en_ids":[1,2,3],"zh_ids":[1,2],"layer":"bottom","kind":"dialogue"}]}`
		runTest(t, w0, w1)
	})

	t.Run("SingleSidedENGroupInFineResponse", func(t *testing.T) {
		// Window 0 provides [3]<->[] (single-sided EN)
		w0 := `{"alignments":[{"en_ids":[1,2],"zh_ids":[1],"layer":"bottom","kind":"dialogue"},{"en_ids":[3],"zh_ids":[],"layer":"bottom","kind":"dialogue"}]}`
		w1 := `{"alignments":[{"en_ids":[1,2,3],"zh_ids":[1,2],"layer":"bottom","kind":"dialogue"}]}`
		runTest(t, w0, w1)
	})

	t.Run("SingleSidedZHGroupInFineResponse", func(t *testing.T) {
		// Window 0 provides []<->[2] (single-sided ZH)
		w0 := `{"alignments":[{"en_ids":[1,2],"zh_ids":[1],"layer":"bottom","kind":"dialogue"},{"en_ids":[],"zh_ids":[2],"layer":"bottom","kind":"dialogue"}]}`
		w1 := `{"alignments":[{"en_ids":[1,2,3],"zh_ids":[1,2],"layer":"bottom","kind":"dialogue"}]}`
		runTest(t, w0, w1)
	})

	t.Run("ExtraExternalCueInFineResponse", func(t *testing.T) {
		// Add cue 4 to bundle
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
		en := []Cue{
			cue(1, 98000, 99000, "1"),
			cue(2, 99000, 100000, "2"),
			cue(3, 101000, 102000, "3"),
			cue(4, 102500, 103000, "4"),
		}
		zh := []Cue{
			cue(1, 98000, 100000, "1"),
			cue(2, 101000, 102000, "2"),
		}
		bundle := testBundle(t, en, zh)

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var p requestPayload
			if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
				t.Errorf("decode request: %v", err)
				w.WriteHeader(500)
				return
			}
			var view windowView
			if err := json.Unmarshal([]byte(p.Messages[1].Content), &view); err != nil {
				t.Errorf("unmarshal source view: %v", err)
				w.WriteHeader(500)
				return
			}

			// Coarse group covers only [1, 2, 3] <-> [1, 2]
			// Window 0 puts extra cue 4 into the fine group: [1, 2, 4] <-> [1]
			var content string
			if view.CoreMS[0] == 0 {
				content = `{"alignments":[{"en_ids":[1,2,4],"zh_ids":[1],"layer":"bottom","kind":"dialogue"},{"en_ids":[3],"zh_ids":[2],"layer":"bottom","kind":"dialogue"}]}`
			} else {
				content = `{"alignments":[{"en_ids":[1,2,3],"zh_ids":[1,2],"layer":"bottom","kind":"dialogue"},{"en_ids":[4],"zh_ids":[],"layer":"bottom","kind":"dialogue"}]}`
			}

			if err := json.NewEncoder(w).Encode(map[string]any{
				"model": "mock",
				"choices": []any{
					map[string]any{"finish_reason": "stop", "message": map[string]any{"content": content}},
				},
			}); err != nil {
				t.Errorf("encode response: %v", err)
			}
		}))
		defer server.Close()

		cfg := testConfig(t, server.URL+"/v1")
		cfg.WindowSeconds = 100
		cfg.OverlapSeconds = 30
		cfg.Retries = 0
		engine := NewEngine()

		mapping, audit, err := engine.Align(context.Background(), bundle, cfg)
		if err != nil {
			t.Fatalf("Align failed: %v", err)
		}

		for _, g := range mapping.Groups {
			if g.Origin == OriginModel && reflect.DeepEqual(g.ENIDs, []int{1, 2, 4}) {
				t.Errorf("fine group with extra external cue should not be accepted: %+v", mapping.Groups)
			}
		}
		if len(audit.Restored["en"])+len(audit.Restored["zh"]) == 0 && len(audit.Conflicts) == 0 {
			t.Error("expected conflicts or restorations for extra external cue")
		}
	})
}

// TestEngine_CompatibleRefinement_Behavior5_MetadataAndThirdObservation verifies Behavior 5:
// - Opposite layer between coarse and fine groups preserves conflict.
// - Explicit kind conflict (e.g. dialogue vs lyrics) preserves conflict.
// - A third complete window giving a genuine cross-partition preserves conflict across the entire component.
func TestEngine_CompatibleRefinement_Behavior5_MetadataAndThirdObservation(t *testing.T) {
	t.Run("LayerMismatchPreservesConflict", func(t *testing.T) {
		en, zh := makeCompatibleTestCues()
		bundle := testBundle(t, en, zh)

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var p requestPayload
			if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
				t.Errorf("decode request: %v", err)
				w.WriteHeader(500)
				return
			}
			var view windowView
			if err := json.Unmarshal([]byte(p.Messages[1].Content), &view); err != nil {
				t.Errorf("unmarshal source view: %v", err)
				w.WriteHeader(500)
				return
			}

			var content string
			if view.CoreMS[0] == 0 {
				// Window 0 has layer "top" on the fine group
				content = `{"alignments":[{"en_ids":[1,2],"zh_ids":[1],"layer":"top","kind":"dialogue"},{"en_ids":[3],"zh_ids":[2],"layer":"bottom","kind":"dialogue"}]}`
			} else {
				// Window 1 has layer "bottom" on the coarse group
				content = `{"alignments":[{"en_ids":[1,2,3],"zh_ids":[1,2],"layer":"bottom","kind":"dialogue"}]}`
			}

			if err := json.NewEncoder(w).Encode(map[string]any{
				"model": "mock",
				"choices": []any{
					map[string]any{"finish_reason": "stop", "message": map[string]any{"content": content}},
				},
			}); err != nil {
				t.Errorf("encode response: %v", err)
			}
		}))
		defer server.Close()

		cfg := testConfig(t, server.URL+"/v1")
		cfg.WindowSeconds = 100
		cfg.OverlapSeconds = 30
		cfg.Retries = 0
		engine := NewEngine()

		mapping, audit, err := engine.Align(context.Background(), bundle, cfg)
		if err != nil {
			t.Fatalf("Align failed: %v", err)
		}

		if len(audit.Conflicts) == 0 {
			t.Fatal("expected conflict for layer mismatch, got 0")
		}
		if len(audit.Restored["en"])+len(audit.Restored["zh"]) == 0 {
			t.Fatal("expected cues to be restored single-sided when layer conflict occurs")
		}
		for _, g := range mapping.Groups {
			if g.Origin == OriginModel && reflect.DeepEqual(g.ENIDs, []int{1, 2}) {
				t.Errorf("fine group with layer mismatch should not be accepted: %+v", mapping.Groups)
			}
		}
	})

	t.Run("KindMismatchPreservesConflict", func(t *testing.T) {
		en, zh := makeCompatibleTestCues()
		bundle := testBundle(t, en, zh)

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var p requestPayload
			if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
				t.Errorf("decode request: %v", err)
				w.WriteHeader(500)
				return
			}
			var view windowView
			if err := json.Unmarshal([]byte(p.Messages[1].Content), &view); err != nil {
				t.Errorf("unmarshal source view: %v", err)
				w.WriteHeader(500)
				return
			}

			var content string
			if view.CoreMS[0] == 0 {
				// Window 0 specifies kind "dialogue"
				content = `{"alignments":[{"en_ids":[1,2],"zh_ids":[1],"layer":"bottom","kind":"dialogue"},{"en_ids":[3],"zh_ids":[2],"layer":"bottom","kind":"dialogue"}]}`
			} else {
				// Window 1 specifies conflicting kind "lyrics"
				content = `{"alignments":[{"en_ids":[1,2,3],"zh_ids":[1,2],"layer":"bottom","kind":"lyrics"}]}`
			}

			if err := json.NewEncoder(w).Encode(map[string]any{
				"model": "mock",
				"choices": []any{
					map[string]any{"finish_reason": "stop", "message": map[string]any{"content": content}},
				},
			}); err != nil {
				t.Errorf("encode response: %v", err)
			}
		}))
		defer server.Close()

		cfg := testConfig(t, server.URL+"/v1")
		cfg.WindowSeconds = 100
		cfg.OverlapSeconds = 30
		cfg.Retries = 0
		engine := NewEngine()

		mapping, audit, err := engine.Align(context.Background(), bundle, cfg)
		if err != nil {
			t.Fatalf("Align failed: %v", err)
		}

		if len(audit.Conflicts) == 0 {
			t.Fatal("expected conflict for explicit kind mismatch, got 0")
		}
		for _, g := range mapping.Groups {
			if g.Origin == OriginModel && reflect.DeepEqual(g.ENIDs, []int{1, 2}) {
				t.Errorf("fine group with kind conflict should not be accepted: %+v", mapping.Groups)
			}
		}
	})

	t.Run("ThirdWindowCrossObservationPreservesConflict", func(t *testing.T) {
		en, zh := makeCompatibleTestCues()
		bundle := testBundle(t, en, zh)
		coreMS := 100000

		// Three windows all fully received the 5 cues:
		// Window 0: fine partition [1, 2] <-> [1] and [3] <-> [2]
		// Window 1: coarse group [1, 2, 3] <-> [1, 2]
		// Window 2: conflicting cross partition [1] <-> [1] and [2, 3] <-> [2]
		batches := []batch{
			{
				Index:   0,
				ENInput: []int{1, 2, 3},
				ZHInput: []int{1, 2},
				Valid: []Group{
					{ENIDs: []int{1, 2}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
					{ENIDs: []int{3}, ZHIDs: []int{2}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
				},
			},
			{
				Index:   1,
				ENInput: []int{1, 2, 3},
				ZHInput: []int{1, 2},
				Valid: []Group{
					{ENIDs: []int{1, 2, 3}, ZHIDs: []int{1, 2}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
				},
			},
			{
				Index:   2,
				ENInput: []int{1, 2, 3},
				ZHInput: []int{1, 2},
				Valid: []Group{
					{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
					{ENIDs: []int{2, 3}, ZHIDs: []int{2}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
				},
			},
		}

		accepted, audit := reconcile(bundle, batches, coreMS)

		// Because window 2 touches the same component with a cross partition,
		// refinement cannot be accepted: conflict must be preserved.
		if len(audit.Conflicts) == 0 {
			t.Fatal("expected conflicts when third window presents cross partition, got 0")
		}
		for _, g := range accepted {
			if g.Origin == OriginModel && reflect.DeepEqual(g.ENIDs, []int{1, 2}) {
				t.Errorf("fine group [1, 2] should not be accepted when third window disagrees: %+v", accepted)
			}
		}
		if len(audit.Restored["en"]) != 3 || len(audit.Restored["zh"]) != 2 {
			t.Errorf("expected all 5 cues restored single-sided, got EN=%v ZH=%v", audit.Restored["en"], audit.Restored["zh"])
		}
	})
}

// TestEngine_CompatibleRefinement_Behavior6_TruncationAndRejectionProtectionPreserved verifies Behavior 6:
// Existing correct input-truncation projections continue to be accepted.
// Existing rejections (unknown input, omitted visible members, etc.) continue to be rejected.
// Legitimate single-sided cues remain preserved at their original times.
func TestEngine_CompatibleRefinement_Behavior6_TruncationAndRejectionProtectionPreserved(t *testing.T) {
	t.Run("ActualInputTruncationProjectionAccepted", func(t *testing.T) {
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
		// Core 0: [0, 10s), Core 1: [10s, 20s)
		// Cues 1 and 2 start in core 1 (10500ms, 11000ms)
		en := []Cue{
			cue(1, 10500, 10800, "1"),
			cue(2, 11000, 11500, "2"),
		}
		zh := []Cue{
			cue(1, 10500, 10800, "1"),
			cue(2, 11000, 11500, "2"),
		}
		bundle := testBundle(t, en, zh)

		// W0 only received EN[1] and ZH[1] (context reached 10500 but not 11000)
		// W1 received all cues EN[1, 2] and ZH[1, 2]
		batches := []batch{
			{
				Index:   0,
				ENInput: []int{1},
				ZHInput: []int{1},
				Valid: []Group{
					{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
				},
			},
			{
				Index:   1,
				ENInput: []int{1, 2},
				ZHInput: []int{1, 2},
				Valid: []Group{
					{ENIDs: []int{1, 2}, ZHIDs: []int{1, 2}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
				},
			},
		}

		accepted, audit := reconcile(bundle, batches, 10000)
		if len(audit.Conflicts) != 0 {
			t.Fatalf("expected 0 conflicts for proven truncation projection, got %d", len(audit.Conflicts))
		}
		if len(accepted) != 1 || !reflect.DeepEqual(accepted[0].ENIDs, []int{1, 2}) {
			t.Fatalf("expected complete owner group accepted, got %+v", accepted)
		}
		if accepted[0].OwnerWindow != 1 {
			t.Errorf("expected owner window 1, got %d", accepted[0].OwnerWindow)
		}
	})

	t.Run("UnknownInputPreservesConflictProtection", func(t *testing.T) {
		en, zh := makeCompatibleTestCues()
		bundle := testBundle(t, en, zh)

		// Window 0 input is unknown (nil)
		batches := []batch{
			{
				Index:   0,
				ENInput: nil,
				ZHInput: nil,
				Valid: []Group{
					{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
				},
			},
			{
				Index:   1,
				ENInput: []int{1, 2, 3},
				ZHInput: []int{1, 2},
				Valid: []Group{
					{ENIDs: []int{1, 2, 3}, ZHIDs: []int{1, 2}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
				},
			},
		}

		accepted, audit := reconcile(bundle, batches, 100000)
		if len(audit.Conflicts) == 0 {
			t.Fatal("expected conflict when input is unknown, got 0")
		}
		for _, g := range accepted {
			if g.Origin == OriginModel && reflect.DeepEqual(g.ENIDs, []int{1, 2, 3}) {
				t.Errorf("coarse group should not be accepted when rival observation has unknown input: %+v", accepted)
			}
		}
	})

	t.Run("OmittedProvidedMemberStillConflict", func(t *testing.T) {
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
		en := []Cue{
			cue(1, 10500, 10800, "1"),
			cue(2, 10900, 11200, "2"),
		}
		zh := []Cue{
			cue(1, 10500, 10800, "1"),
			cue(2, 10900, 11200, "2"),
		}
		bundle := testBundle(t, en, zh)

		// W0 saw both 1 and 2, but returned only 1 (omitted visible member 2!)
		batches := []batch{
			{
				Index:   0,
				ENInput: []int{1, 2},
				ZHInput: []int{1, 2},
				Valid: []Group{
					{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
				},
			},
			{
				Index:   1,
				ENInput: []int{1, 2},
				ZHInput: []int{1, 2},
				Valid: []Group{
					{ENIDs: []int{1, 2}, ZHIDs: []int{1, 2}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
				},
			},
		}

		accepted, audit := reconcile(bundle, batches, 10000)
		if len(audit.Conflicts) == 0 {
			t.Fatal("expected conflict when observation omitted provided member, got 0")
		}
		for _, g := range accepted {
			if g.Origin == OriginModel && reflect.DeepEqual(g.ENIDs, []int{1, 2}) {
				t.Errorf("group [1, 2] should not be accepted when rival omitted member: %+v", accepted)
			}
		}
	})

	t.Run("LegitimateSingleSidedCuePreservedAtOriginalTime", func(t *testing.T) {
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
		// EN has 4 cues; cue 4 is untranslated (no ZH counterpart)
		en := []Cue{
			cue(1, 98000, 99000, "1"),
			cue(2, 99000, 100000, "2"),
			cue(3, 101000, 102000, "3"),
			cue(4, 105000, 106000, "Untranslated sign."),
		}
		zh := []Cue{
			cue(1, 98000, 100000, "1"),
			cue(2, 101000, 102000, "2"),
		}
		bundle := testBundle(t, en, zh)

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var p requestPayload
			if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
				t.Errorf("decode request: %v", err)
				w.WriteHeader(500)
				return
			}
			var view windowView
			if err := json.Unmarshal([]byte(p.Messages[1].Content), &view); err != nil {
				t.Errorf("unmarshal source view: %v", err)
				w.WriteHeader(500)
				return
			}

			// In window 0: fine partition [1, 2] <-> [1] and [3] <-> [2]
			// In window 1: coarse group [1, 2, 3] <-> [1, 2]
			// Cue 4 is left unaligned in both windows
			var content string
			if view.CoreMS[0] == 0 {
				content = `{"alignments":[{"en_ids":[1,2],"zh_ids":[1],"layer":"bottom","kind":"dialogue"},{"en_ids":[3],"zh_ids":[2],"layer":"bottom","kind":"dialogue"}]}`
			} else {
				content = `{"alignments":[{"en_ids":[1,2,3],"zh_ids":[1,2],"layer":"bottom","kind":"dialogue"}]}`
			}

			if err := json.NewEncoder(w).Encode(map[string]any{
				"model": "mock",
				"choices": []any{
					map[string]any{"finish_reason": "stop", "message": map[string]any{"content": content}},
				},
			}); err != nil {
				t.Errorf("encode response: %v", err)
			}
		}))
		defer server.Close()

		cfg := testConfig(t, server.URL+"/v1")
		cfg.WindowSeconds = 100
		cfg.OverlapSeconds = 30
		cfg.Retries = 0
		engine := NewEngine()

		mapping, audit, err := engine.Align(context.Background(), bundle, cfg)
		if err != nil {
			t.Fatalf("Align failed: %v", err)
		}

		if len(audit.Conflicts) != 0 {
			t.Fatalf("expected 0 conflicts, got %d: %+v", len(audit.Conflicts), audit.Conflicts)
		}

		// Cue 4 should be restored single-sided
		if len(audit.Restored["en"]) != 1 || audit.Restored["en"][0] != 4 {
			t.Fatalf("expected EN cue 4 restored, got %v", audit.Restored["en"])
		}

		// Mapping should have 3 groups: fine group 1, fine group 2, and restored cue 4
		if len(mapping.Groups) != 3 {
			t.Fatalf("expected 3 groups, got %d: %+v", len(mapping.Groups), mapping.Groups)
		}
		restoredG4 := mapping.Groups[2]
		if !reflect.DeepEqual(restoredG4.ENIDs, []int{4}) || len(restoredG4.ZHIDs) != 0 || restoredG4.Origin != OriginRestored {
			t.Errorf("expected group 3 to be restored EN 4, got %+v", restoredG4)
		}
	})
}

// TestRecordedE01_CompatibleRefinement verifies the actual offline replay of E01 recorded responses:
//   - EN 495-497 and ZH 471-472 recover from 5 single-sided restorations to 2 bilingual groups:
//     [495, 496] <-> [471] (OwnerWindow: 12)
//     [497] <-> [472] (OwnerWindow: 13, supported by window 12 response)
//   - The 6 genuine conflicts remain refused.
//   - Negative mutation check: tampering with the fine partition reproduces conflict.
func TestRecordedE01_CompatibleRefinement(t *testing.T) {
	dir := os.Getenv(positionRecordDirEnv)
	if dir == "" {
		cand := "/Users/maverick/Desktop/fusionn-subtitle-split-test/terminal-list-s02e01-dialogue-lyrics-v5"
		if _, err := os.Stat(cand); err == nil {
			dir = cand
		}
	}
	if dir == "" {
		t.Skip("skipping E01 recorded replay: fixture directory not found")
	}

	cfg := replayConfig{
		Dir:         dir,
		SourcesPath: filepath.Join(dir, "sources.json"),
		StylePath:   filepath.Join(dir, "delivered-style.json"),
		WordsPath:   filepath.Join(dir, "word-timings.json"),
	}

	res, err := replayRecordedResponses(cfg)
	if err != nil {
		t.Fatalf("replay recorded responses failed: %v", err)
	}

	mapping, audit, bundle := res.Mapping, res.Audit, res.Bundle

	// 1. Verify that EN 495, 496, 497 and ZH 471, 472 are NOT restored single-sided.
	restoredEN, restoredZH := map[int]bool{}, map[int]bool{}
	for _, id := range audit.Restored["en"] {
		restoredEN[id] = true
	}
	for _, id := range audit.Restored["zh"] {
		restoredZH[id] = true
	}
	for _, id := range []int{495, 496, 497} {
		if restoredEN[id] {
			t.Errorf("EN %d unexpectedly in restored audit: %v", id, audit.Restored["en"])
		}
	}
	for _, id := range []int{471, 472} {
		if restoredZH[id] {
			t.Errorf("ZH %d unexpectedly in restored audit: %v", id, audit.Restored["zh"])
		}
	}

	// 2. Verify that both fine bilingual groups exist in mapping with correct OwnerWindow and OriginModel.
	groupMap := map[string]Group{}
	for _, g := range mapping.Groups {
		groupMap[pairKey(g.ENIDs, g.ZHIDs)] = g
	}

	g1, ok1 := groupMap[pairKey([]int{495, 496}, []int{471})]
	if !ok1 {
		t.Fatal("recovered group [495, 496] <-> [471] missing from mapping")
	}
	if g1.Origin != OriginModel {
		t.Errorf("g1 origin = %s, want model", g1.Origin)
	}
	if g1.OwnerWindow != 12 {
		t.Errorf("g1 owner = %d, want 12", g1.OwnerWindow)
	}

	g2, ok2 := groupMap[pairKey([]int{497}, []int{472})]
	if !ok2 {
		t.Fatal("recovered group [497] <-> [472] missing from mapping")
	}
	if g2.Origin != OriginModel {
		t.Errorf("g2 origin = %s, want model", g2.Origin)
	}
	if g2.OwnerWindow != 13 {
		t.Errorf("g2 owner = %d, want 13 (logical owner by core time)", g2.OwnerWindow)
	}

	// 3. Exactly 6 genuine conflicts remain (down from 7).
	if len(audit.Conflicts) != 6 {
		t.Errorf("expected 6 genuine retained conflicts, got %d: %+v", len(audit.Conflicts), audit.Conflicts)
	}
	for _, c := range audit.Conflicts {
		if reflect.DeepEqual(c.ENIDs, []int{495, 496}) && reflect.DeepEqual(c.ZHIDs, []int{471}) {
			t.Errorf("conflict for [495, 496] <-> [471] should have been resolved by compatible refinement!")
		}
	}

	// 4. Restored counts: EN: 16 - 3 = 13, ZH: 4 - 2 = 2.
	if len(audit.Restored["en"]) != 13 {
		t.Errorf("expected 13 restored EN cues, got %d: %v", len(audit.Restored["en"]), audit.Restored["en"])
	}
	if len(audit.Restored["zh"]) != 2 {
		t.Errorf("expected 2 restored ZH cues, got %d: %v", len(audit.Restored["zh"]), audit.Restored["zh"])
	}

	// 5. Total cues conservation: every cue in bundle appears exactly once.
	enCounts, zhCounts := map[int]int{}, map[int]int{}
	for _, g := range mapping.Groups {
		for _, id := range g.ENIDs {
			enCounts[id]++
		}
		for _, id := range g.ZHIDs {
			zhCounts[id]++
		}
	}
	for _, c := range bundle.EN {
		if enCounts[c.ID] != 1 {
			t.Errorf("EN cue %d appears %d times in mapping, want 1", c.ID, enCounts[c.ID])
		}
	}
	for _, c := range bundle.ZH {
		if zhCounts[c.ID] != 1 {
			t.Errorf("ZH cue %d appears %d times in mapping, want 1", c.ID, zhCounts[c.ID])
		}
	}

	// 6. Negative mutation check:
	// If the model had omitted EN 496 from the fine response in window 12,
	// compatible refinement must NOT trigger, preserving conflict.
	t.Run("NegativeMutationOmittedMember", func(t *testing.T) {
		// Create a synthetic test with window 12 missing 496
		mutatedBatches := []batch{
			{
				Index:   12,
				ENInput: []int{495, 496, 497},
				ZHInput: []int{471, 472},
				Valid: []Group{
					{ENIDs: []int{495}, ZHIDs: []int{471}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
					{ENIDs: []int{497}, ZHIDs: []int{472}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
				},
			},
			{
				Index:   13,
				ENInput: []int{495, 496, 497},
				ZHInput: []int{471, 472},
				Valid: []Group{
					{ENIDs: []int{495, 496, 497}, ZHIDs: []int{471, 472}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
				},
			},
		}
		_, negAudit := reconcile(bundle, mutatedBatches, 210000)
		if len(negAudit.Conflicts) == 0 {
			t.Fatal("negative mutation should have triggered conflict, but got 0 conflicts")
		}
	})
}

// TestEngine_KindGuard_R1_PublicEngineTruncationCounterexample implements R1:
// A real 3-window scenario via public Engine.Align where Window 2 observes
// a truncated context projection with an explicit conflicting kind ("on_screen_text"),
// while Window 1 (coarse group) omitted kind ("").
// Under unpatched a7, findCompatibleRefinements erroneously treats Window 1's missing kind
// as a wildcard bridge, swallowing the explicit kind conflict and resulting in 0 conflicts.
// The expected behavior is that the explicit kind conflict is preserved:
// refinement is rejected, component cues are restored single-sided, and cue 9 is preserved.
func TestEngine_KindGuard_R1_PublicEngineTruncationCounterexample(t *testing.T) {
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

	en := []Cue{
		cue(1, 138000, 139000, "One."),
		cue(2, 139000, 140000, "Two."),
		cue(3, 141000, 161000, "Three."),
		cue(4, 250000, 251000, "Nine."),
	}
	zh := []Cue{
		cue(1, 138000, 140000, "一二。"),
		cue(2, 141000, 161000, "三。"),
		cue(3, 250000, 251000, "九。"),
	}
	bundle := testBundle(t, en, zh)

	type windowCall struct {
		coreMS []int
		enIDs  []int
		zhIDs  []int
	}
	var (
		mu    sync.Mutex
		calls []windowCall
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var p requestPayload
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			t.Errorf("decode request: %v", err)
			w.WriteHeader(500)
			return
		}
		var view windowView
		if err := json.Unmarshal([]byte(p.Messages[1].Content), &view); err != nil {
			t.Errorf("unmarshal source view: %v", err)
			w.WriteHeader(500)
			return
		}

		var call windowCall
		call.coreMS = view.CoreMS
		for _, c := range view.EN {
			call.enIDs = append(call.enIDs, c.ID)
		}
		for _, c := range view.ZH {
			call.zhIDs = append(call.zhIDs, c.ID)
		}

		mu.Lock()
		calls = append(calls, call)
		mu.Unlock()

		var content string
		switch view.CoreMS[0] {
		case 0:
			// Window 0: receives EN[1,2,3], ZH[1,2]
			if !reflect.DeepEqual(call.enIDs, []int{1, 2, 3}) || !reflect.DeepEqual(call.zhIDs, []int{1, 2}) {
				t.Errorf("window 0 input mismatch: EN=%v, ZH=%v", call.enIDs, call.zhIDs)
			}
			content = `{"alignments":[{"en_ids":[1,2],"zh_ids":[1],"layer":"bottom","kind":"dialogue"},{"en_ids":[3],"zh_ids":[2],"layer":"bottom","kind":"dialogue"}]}`
		case 100000:
			// Window 1: receives EN[1,2,3], ZH[1,2]
			if !reflect.DeepEqual(call.enIDs, []int{1, 2, 3}) || !reflect.DeepEqual(call.zhIDs, []int{1, 2}) {
				t.Errorf("window 1 input mismatch: EN=%v, ZH=%v", call.enIDs, call.zhIDs)
			}
			// Omitting kind
			content = `{"alignments":[{"en_ids":[1,2,3],"zh_ids":[1,2],"layer":"bottom"}]}`
		case 200000:
			// Window 2: receives EN[3,4], ZH[2,3]
			if !reflect.DeepEqual(call.enIDs, []int{3, 4}) || !reflect.DeepEqual(call.zhIDs, []int{2, 3}) {
				t.Errorf("window 2 input mismatch: EN=%v, ZH=%v", call.enIDs, call.zhIDs)
			}
			content = `{"alignments":[{"en_ids":[3],"zh_ids":[2],"layer":"bottom","kind":"on_screen_text"},{"en_ids":[4],"zh_ids":[3],"layer":"bottom","kind":"dialogue"}]}`
		default:
			t.Errorf("unexpected window CoreMS: %v", view.CoreMS)
		}

		if err := json.NewEncoder(w).Encode(map[string]any{
			"model": "mock",
			"choices": []any{
				map[string]any{"finish_reason": "stop", "message": map[string]any{"content": content}},
			},
		}); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}))
	defer server.Close()

	cfg := testConfig(t, server.URL+"/v1")
	cfg.WindowSeconds = 100
	cfg.OverlapSeconds = 90
	cfg.Retries = 0
	engine := NewEngine()

	mapping, audit, err := engine.Align(context.Background(), bundle, cfg)
	if err != nil {
		t.Fatalf("Align failed: %v", err)
	}

	// Verify that all 3 windows actually ran and saw expected inputs
	if len(calls) != 3 {
		t.Fatalf("expected 3 window calls, got %d", len(calls))
	}

	// Under R1 requirement:
	// A conflicting explicit observation on [3]:[2] ("on_screen_text" vs "dialogue")
	// must NOT be swallowed by Window 1's omitted kind.
	// Therefore, refinement must be rejected:
	// - audit.Conflicts must be non-empty (coarse group [1,2,3]<->[1,2] conflicted).
	// - Cues 1, 2, 3 in EN and 1, 2 in ZH must be restored single-sided.
	// - Independent cue (EN 4, ZH 3) must remain cleanly paired [4]<->[3].
	if len(audit.Conflicts) == 0 {
		t.Fatalf("RED EXPECTED: expected conflict for explicit kind mismatch on [3]:[2], but got 0 conflicts (swallowed by missing kind). Audit: %+v, Groups: %+v", audit, mapping.Groups)
	}

	restoredEN, restoredZH := map[int]bool{}, map[int]bool{}
	for _, id := range audit.Restored["en"] {
		restoredEN[id] = true
	}
	for _, id := range audit.Restored["zh"] {
		restoredZH[id] = true
	}
	for _, id := range []int{1, 2, 3} {
		if !restoredEN[id] {
			t.Errorf("expected EN %d restored single-sided, but was not in audit.Restored", id)
		}
	}
	for _, id := range []int{1, 2} {
		if !restoredZH[id] {
			t.Errorf("expected ZH %d restored single-sided, but was not in audit.Restored", id)
		}
	}

	// Verify independent cue (EN 4, ZH 3) is accepted normally as bilingual
	independentAccepted := false
	for _, g := range mapping.Groups {
		if reflect.DeepEqual(g.ENIDs, []int{4}) && reflect.DeepEqual(g.ZHIDs, []int{3}) {
			independentAccepted = true
			if g.Origin != OriginModel {
				t.Errorf("independent cue origin = %s, want model", g.Origin)
			}
		}
		// Fine group [1, 2]:[1] must not be accepted from the model; restored
		// cues may still pair by timing afterwards.
		if reflect.DeepEqual(g.ENIDs, []int{1, 2}) && g.Origin == OriginModel {
			t.Errorf("fine group [1, 2] should not be accepted when [3]:[2] has kind conflict: %+v", g)
		}
	}
	if !independentAccepted {
		t.Errorf("independent cue was not accepted in mapping: %+v", mapping.Groups)
	}
}

// TestEngine_KindGuard_R2_PreserveValidDefault verifies R2:
//   - Part A: When the third observation in Window 2 is also "dialogue" (matching fine group),
//     the refinement is accepted, preserving 2 fine groups, logical owner=1, 0 conflicts, 0 restored.
//   - Part B: Time-shifted across later cores verifies stability and source fingerprint preservation.
//   - Part C: Reverse kind direction (fine groups are "on_screen_text", third observation is "dialogue")
//     must also be rejected, proving the guard works bidirectionally and does not simply reject all 3-window cases.
func TestEngine_KindGuard_R2_PreserveValidDefault(t *testing.T) {
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

	t.Run("ThirdObservationDialoguePreservesRefinement", func(t *testing.T) {
		en := []Cue{
			cue(1, 138000, 139000, "One."),
			cue(2, 139000, 140000, "Two."),
			cue(3, 141000, 161000, "Three."),
			cue(4, 250000, 251000, "Nine."),
		}
		zh := []Cue{
			cue(1, 138000, 140000, "一二。"),
			cue(2, 141000, 161000, "三。"),
			cue(3, 250000, 251000, "九。"),
		}
		bundle := testBundle(t, en, zh)

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var p requestPayload
			if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
				t.Errorf("decode request: %v", err)
				w.WriteHeader(500)
				return
			}
			var view windowView
			if err := json.Unmarshal([]byte(p.Messages[1].Content), &view); err != nil {
				t.Errorf("unmarshal source view: %v", err)
				w.WriteHeader(500)
				return
			}

			var content string
			switch view.CoreMS[0] {
			case 0:
				content = `{"alignments":[{"en_ids":[1,2],"zh_ids":[1],"layer":"bottom","kind":"dialogue"},{"en_ids":[3],"zh_ids":[2],"layer":"bottom","kind":"dialogue"}]}`
			case 100000:
				content = `{"alignments":[{"en_ids":[1,2,3],"zh_ids":[1,2],"layer":"bottom"}]}`
			case 200000:
				// Window 2: also "dialogue"
				content = `{"alignments":[{"en_ids":[3],"zh_ids":[2],"layer":"bottom","kind":"dialogue"},{"en_ids":[4],"zh_ids":[3],"layer":"bottom","kind":"dialogue"}]}`
			default:
				t.Errorf("unexpected window CoreMS: %v", view.CoreMS)
			}

			if err := json.NewEncoder(w).Encode(map[string]any{
				"model": "mock",
				"choices": []any{
					map[string]any{"finish_reason": "stop", "message": map[string]any{"content": content}},
				},
			}); err != nil {
				t.Errorf("encode response: %v", err)
			}
		}))
		defer server.Close()

		cfg := testConfig(t, server.URL+"/v1")
		cfg.WindowSeconds = 100
		cfg.OverlapSeconds = 90
		cfg.Retries = 0
		engine := NewEngine()

		mapping, audit, err := engine.Align(context.Background(), bundle, cfg)
		if err != nil {
			t.Fatalf("Align failed: %v", err)
		}

		if len(audit.Conflicts) != 0 {
			t.Fatalf("expected 0 conflicts when third observation is compatible dialogue, got %d: %+v", len(audit.Conflicts), audit.Conflicts)
		}
		if len(audit.Restored["en"]) != 0 || len(audit.Restored["zh"]) != 0 {
			t.Fatalf("expected 0 restored cues, got EN=%v ZH=%v", audit.Restored["en"], audit.Restored["zh"])
		}

		// Verify 2 fine groups preserved with OwnerWindow = 1
		groupMap := map[string]Group{}
		for _, g := range mapping.Groups {
			groupMap[pairKey(g.ENIDs, g.ZHIDs)] = g
		}

		g1, ok1 := groupMap[pairKey([]int{1, 2}, []int{1})]
		if !ok1 {
			t.Fatalf("missing fine group [1, 2] <-> [1]")
		}
		if g1.OwnerWindow != 1 {
			t.Errorf("g1 OwnerWindow = %d, want 1", g1.OwnerWindow)
		}
		if g1.Kind != KindDialogue {
			t.Errorf("g1 Kind = %s, want dialogue", g1.Kind)
		}

		g2, ok2 := groupMap[pairKey([]int{3}, []int{2})]
		if !ok2 {
			t.Fatalf("missing fine group [3] <-> [2]")
		}
		if g2.OwnerWindow != 1 {
			t.Errorf("g2 OwnerWindow = %d, want 1", g2.OwnerWindow)
		}
		if g2.Kind != KindDialogue {
			t.Errorf("g2 Kind = %s, want dialogue", g2.Kind)
		}

		g3, ok3 := groupMap[pairKey([]int{4}, []int{3})]
		if !ok3 {
			t.Fatalf("missing independent group [4] <-> [3]")
		}
		if g3.OwnerWindow != 2 {
			t.Errorf("g3 OwnerWindow = %d, want 2", g3.OwnerWindow)
		}
	})

	t.Run("TimeShiftedAcrossLaterCoresVerification", func(t *testing.T) {
		// Shift all timestamps by +100000ms:
		// Window 0 is empty of these cues.
		// Window 1: [100s, 200s), context [55s, 245s) -> receives component EN[1,2,3], ZH[1,2] (start 238s < 245s)
		// Window 2: [200s, 300s), context [155s, 345s) -> receives component EN[1,2,3], ZH[1,2]
		// Window 3: [300s, 400s), context [255s, 445s) -> receives EN[3,4], ZH[2,3] (start 350s)
		shift := 100000
		en := []Cue{
			cue(1, 138000+shift, 139000+shift, "One shifted."),
			cue(2, 139000+shift, 140000+shift, "Two shifted."),
			cue(3, 141000+shift, 161000+shift, "Three shifted."),
			cue(4, 250000+shift, 251000+shift, "Nine shifted."),
		}
		zh := []Cue{
			cue(1, 138000+shift, 140000+shift, "一二平移。"),
			cue(2, 141000+shift, 161000+shift, "三平移。"),
			cue(3, 250000+shift, 251000+shift, "九平移。"),
		}
		bundle := testBundle(t, en, zh)

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var p requestPayload
			if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
				t.Errorf("decode request: %v", err)
				w.WriteHeader(500)
				return
			}
			var view windowView
			if err := json.Unmarshal([]byte(p.Messages[1].Content), &view); err != nil {
				t.Errorf("unmarshal source view: %v", err)
				w.WriteHeader(500)
				return
			}

			var content string
			switch view.CoreMS[0] {
			case 0:
				// No cues in window 0
				content = `{"alignments":[]}`
			case 100000:
				content = `{"alignments":[{"en_ids":[1,2],"zh_ids":[1],"layer":"bottom","kind":"dialogue"},{"en_ids":[3],"zh_ids":[2],"layer":"bottom","kind":"dialogue"}]}`
			case 200000:
				content = `{"alignments":[{"en_ids":[1,2,3],"zh_ids":[1,2],"layer":"bottom"}]}`
			case 300000:
				content = `{"alignments":[{"en_ids":[3],"zh_ids":[2],"layer":"bottom","kind":"dialogue"},{"en_ids":[4],"zh_ids":[3],"layer":"bottom","kind":"dialogue"}]}`
			default:
				t.Errorf("unexpected window CoreMS: %v", view.CoreMS)
			}

			if err := json.NewEncoder(w).Encode(map[string]any{
				"model": "mock",
				"choices": []any{
					map[string]any{"finish_reason": "stop", "message": map[string]any{"content": content}},
				},
			}); err != nil {
				t.Errorf("encode response: %v", err)
			}
		}))
		defer server.Close()

		cfg := testConfig(t, server.URL+"/v1")
		cfg.WindowSeconds = 100
		cfg.OverlapSeconds = 90
		cfg.Retries = 0
		engine := NewEngine()

		mapping, audit, err := engine.Align(context.Background(), bundle, cfg)
		if err != nil {
			t.Fatalf("Align failed: %v", err)
		}

		if len(audit.Conflicts) != 0 {
			t.Fatalf("expected 0 conflicts for shifted time compatible refinement, got %d", len(audit.Conflicts))
		}
		if len(audit.Restored["en"]) != 0 || len(audit.Restored["zh"]) != 0 {
			t.Fatalf("expected 0 restored cues, got EN=%v ZH=%v", audit.Restored["en"], audit.Restored["zh"])
		}

		groupMap := map[string]Group{}
		for _, g := range mapping.Groups {
			groupMap[pairKey(g.ENIDs, g.ZHIDs)] = g
		}
		g1, ok1 := groupMap[pairKey([]int{1, 2}, []int{1})]
		if !ok1 || g1.OwnerWindow != 2 {
			t.Errorf("g1 missing or owner = %d, want 2", g1.OwnerWindow)
		}
		g2, ok2 := groupMap[pairKey([]int{3}, []int{2})]
		if !ok2 || g2.OwnerWindow != 2 {
			t.Errorf("g2 missing or owner = %d, want 2", g2.OwnerWindow)
		}
	})

	t.Run("PrefixShiftedIDsVerification", func(t *testing.T) {
		// Prepend an independent bilingual cue at 10000ms:
		// EN 1 (10s), EN 2 (138s), EN 3 (139s), EN 4 (141s), EN 5 (250s)
		// ZH 1 (10s), ZH 2 (138s), ZH 3 (141s), ZH 4 (250s)
		// This shifts the component IDs to EN[2, 3, 4] and ZH[2, 3], while strictly obeying ID == i+1.
		en := []Cue{
			cue(1, 10000, 11000, "Prefix cue."),
			cue(2, 138000, 139000, "One."),
			cue(3, 139000, 140000, "Two."),
			cue(4, 141000, 161000, "Three."),
			cue(5, 250000, 251000, "Nine."),
		}
		zh := []Cue{
			cue(1, 10000, 11000, "前置独立句。"),
			cue(2, 138000, 140000, "一二。"),
			cue(3, 141000, 161000, "三。"),
			cue(4, 250000, 251000, "九。"),
		}
		bundle := testBundle(t, en, zh)

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var p requestPayload
			if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
				t.Errorf("decode request: %v", err)
				w.WriteHeader(500)
				return
			}
			var view windowView
			if err := json.Unmarshal([]byte(p.Messages[1].Content), &view); err != nil {
				t.Errorf("unmarshal source view: %v", err)
				w.WriteHeader(500)
				return
			}

			var content string
			switch view.CoreMS[0] {
			case 0:
				// Window 0 receives EN[1, 2, 3, 4], ZH[1, 2, 3]
				content = `{"alignments":[{"en_ids":[1],"zh_ids":[1],"layer":"bottom","kind":"dialogue"},{"en_ids":[2,3],"zh_ids":[2],"layer":"bottom","kind":"dialogue"},{"en_ids":[4],"zh_ids":[3],"layer":"bottom","kind":"dialogue"}]}`
			case 100000:
				// Window 1 context is [55s, 245s), so cue 1 (10s) is outside!
				// Window 1 receives EN[2, 3, 4], ZH[2, 3]
				content = `{"alignments":[{"en_ids":[2,3,4],"zh_ids":[2,3],"layer":"bottom"}]}`
			case 200000:
				// Window 2 receives EN[4, 5], ZH[3, 4]
				content = `{"alignments":[{"en_ids":[4],"zh_ids":[3],"layer":"bottom","kind":"dialogue"},{"en_ids":[5],"zh_ids":[4],"layer":"bottom","kind":"dialogue"}]}`
			default:
				t.Errorf("unexpected window CoreMS: %v", view.CoreMS)
			}

			if err := json.NewEncoder(w).Encode(map[string]any{
				"model": "mock",
				"choices": []any{
					map[string]any{"finish_reason": "stop", "message": map[string]any{"content": content}},
				},
			}); err != nil {
				t.Errorf("encode response: %v", err)
			}
		}))
		defer server.Close()

		cfg := testConfig(t, server.URL+"/v1")
		cfg.WindowSeconds = 100
		cfg.OverlapSeconds = 90
		cfg.Retries = 0
		engine := NewEngine()

		mapping, audit, err := engine.Align(context.Background(), bundle, cfg)
		if err != nil {
			t.Fatalf("Align failed: %v", err)
		}

		if len(audit.Conflicts) != 0 {
			t.Fatalf("expected 0 conflicts with shifted IDs, got %d: %+v", len(audit.Conflicts), audit.Conflicts)
		}
		if len(audit.Restored["en"]) != 0 || len(audit.Restored["zh"]) != 0 {
			t.Fatalf("expected 0 restored cues, got EN=%v ZH=%v", audit.Restored["en"], audit.Restored["zh"])
		}

		groupMap := map[string]Group{}
		for _, g := range mapping.Groups {
			groupMap[pairKey(g.ENIDs, g.ZHIDs)] = g
		}

		gPrefix, okPrefix := groupMap[pairKey([]int{1}, []int{1})]
		if !okPrefix || gPrefix.OwnerWindow != 0 {
			t.Errorf("prefix cue missing or owner = %d, want 0", gPrefix.OwnerWindow)
		}
		g1, ok1 := groupMap[pairKey([]int{2, 3}, []int{2})]
		if !ok1 || g1.OwnerWindow != 1 {
			t.Errorf("g1 [2, 3]<->[2] missing or owner = %d, want 1", g1.OwnerWindow)
		}
		g2, ok2 := groupMap[pairKey([]int{4}, []int{3})]
		if !ok2 || g2.OwnerWindow != 1 {
			t.Errorf("g2 [4]<->[3] missing or owner = %d, want 1", g2.OwnerWindow)
		}
		g3, ok3 := groupMap[pairKey([]int{5}, []int{4})]
		if !ok3 || g3.OwnerWindow != 2 {
			t.Errorf("g3 [5]<->[4] missing or owner = %d, want 2", g3.OwnerWindow)
		}
	})

	t.Run("ReverseKindDirectionMustReject", func(t *testing.T) {
		// Window 0: fine partition with kind="on_screen_text"
		// Window 1: coarse group omitting kind
		// Window 2: truncated observation on [3]:[2] with kind="dialogue"
		// Must also be rejected!
		en := []Cue{
			cue(1, 138000, 139000, "One."),
			cue(2, 139000, 140000, "Two."),
			cue(3, 141000, 161000, "Three."),
			cue(4, 250000, 251000, "Nine."),
		}
		zh := []Cue{
			cue(1, 138000, 140000, "一二。"),
			cue(2, 141000, 161000, "三。"),
			cue(3, 250000, 251000, "九。"),
		}
		bundle := testBundle(t, en, zh)

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var p requestPayload
			if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
				t.Errorf("decode request: %v", err)
				w.WriteHeader(500)
				return
			}
			var view windowView
			if err := json.Unmarshal([]byte(p.Messages[1].Content), &view); err != nil {
				t.Errorf("unmarshal source view: %v", err)
				w.WriteHeader(500)
				return
			}

			var content string
			switch view.CoreMS[0] {
			case 0:
				// Window 0 specifies "on_screen_text"
				content = `{"alignments":[{"en_ids":[1,2],"zh_ids":[1],"layer":"bottom","kind":"on_screen_text"},{"en_ids":[3],"zh_ids":[2],"layer":"bottom","kind":"on_screen_text"}]}`
			case 100000:
				content = `{"alignments":[{"en_ids":[1,2,3],"zh_ids":[1,2],"layer":"bottom"}]}`
			case 200000:
				// Window 2 specifies "dialogue" on [3]:[2]
				content = `{"alignments":[{"en_ids":[3],"zh_ids":[2],"layer":"bottom","kind":"dialogue"},{"en_ids":[4],"zh_ids":[3],"layer":"bottom","kind":"dialogue"}]}`
			default:
				t.Errorf("unexpected window CoreMS: %v", view.CoreMS)
			}

			if err := json.NewEncoder(w).Encode(map[string]any{
				"model": "mock",
				"choices": []any{
					map[string]any{"finish_reason": "stop", "message": map[string]any{"content": content}},
				},
			}); err != nil {
				t.Errorf("encode response: %v", err)
			}
		}))
		defer server.Close()

		cfg := testConfig(t, server.URL+"/v1")
		cfg.WindowSeconds = 100
		cfg.OverlapSeconds = 90
		cfg.Retries = 0
		engine := NewEngine()

		mapping, audit, err := engine.Align(context.Background(), bundle, cfg)
		if err != nil {
			t.Fatalf("Align failed: %v", err)
		}

		if len(audit.Conflicts) == 0 {
			t.Fatal("expected conflict for reversed kind direction on [3]:[2], got 0 conflicts")
		}
		if len(audit.Restored["en"]) != 3 || len(audit.Restored["zh"]) != 2 {
			t.Errorf("expected 3 EN and 2 ZH cues restored single-sided, got EN=%v ZH=%v", audit.Restored["en"], audit.Restored["zh"])
		}

		// Independent cue [4]<->[3] must still be preserved
		cue4Accepted := false
		for _, g := range mapping.Groups {
			if reflect.DeepEqual(g.ENIDs, []int{4}) && reflect.DeepEqual(g.ZHIDs, []int{3}) {
				cue4Accepted = true
			}
		}
		if !cue4Accepted {
			t.Errorf("independent cue [4]<->[3] missing from mapping: %+v", mapping.Groups)
		}
	})
}

// TestEngine_KindGuard_R3_FineGroupScopeAndDisjointKinds verifies R3:
//   - Two disjoint fine groups can have different kinds ("dialogue" and "on_screen_text")
//     when coarse group omits kind, without forcing the entire component to one kind.
//   - Third observation only affects the touched fine group.
//   - Fine group omitting kind with two mutually conflicting observations must be rejected.
//   - Fine group omitting kind with consistent/single observation is accepted.
func TestEngine_KindGuard_R3_FineGroupScopeAndDisjointKinds(t *testing.T) {
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

	t.Run("DisjointFineGroupsDifferentKindsPreserved", func(t *testing.T) {
		en := []Cue{
			cue(1, 138000, 139000, "One."),
			cue(2, 139000, 140000, "Two."),
			cue(3, 141000, 161000, "Three."),
			cue(4, 250000, 251000, "Nine."),
		}
		zh := []Cue{
			cue(1, 138000, 140000, "一二。"),
			cue(2, 141000, 161000, "三。"),
			cue(3, 250000, 251000, "九。"),
		}
		bundle := testBundle(t, en, zh)

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var p requestPayload
			if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
				t.Errorf("decode request: %v", err)
				w.WriteHeader(500)
				return
			}
			var view windowView
			if err := json.Unmarshal([]byte(p.Messages[1].Content), &view); err != nil {
				t.Errorf("unmarshal source view: %v", err)
				w.WriteHeader(500)
				return
			}

			var content string
			switch view.CoreMS[0] {
			case 0:
				// Fine group 1 is dialogue, Fine group 2 is on_screen_text!
				content = `{"alignments":[{"en_ids":[1,2],"zh_ids":[1],"layer":"bottom","kind":"dialogue"},{"en_ids":[3],"zh_ids":[2],"layer":"bottom","kind":"on_screen_text"}]}`
			case 100000:
				// Coarse group omits kind
				content = `{"alignments":[{"en_ids":[1,2,3],"zh_ids":[1,2],"layer":"bottom"}]}`
			case 200000:
				// Window 2 agrees with Fine group 2 (on_screen_text)
				content = `{"alignments":[{"en_ids":[3],"zh_ids":[2],"layer":"bottom","kind":"on_screen_text"},{"en_ids":[4],"zh_ids":[3],"layer":"bottom","kind":"dialogue"}]}`
			default:
				t.Errorf("unexpected window CoreMS: %v", view.CoreMS)
			}

			if err := json.NewEncoder(w).Encode(map[string]any{
				"model": "mock",
				"choices": []any{
					map[string]any{"finish_reason": "stop", "message": map[string]any{"content": content}},
				},
			}); err != nil {
				t.Errorf("encode response: %v", err)
			}
		}))
		defer server.Close()

		cfg := testConfig(t, server.URL+"/v1")
		cfg.WindowSeconds = 100
		cfg.OverlapSeconds = 90
		cfg.Retries = 0
		engine := NewEngine()

		mapping, audit, err := engine.Align(context.Background(), bundle, cfg)
		if err != nil {
			t.Fatalf("Align failed: %v", err)
		}

		if len(audit.Conflicts) != 0 {
			t.Fatalf("expected 0 conflicts for disjoint fine groups with different kinds, got %d", len(audit.Conflicts))
		}
		if len(audit.Restored["en"]) != 0 || len(audit.Restored["zh"]) != 0 {
			t.Fatalf("expected 0 restored cues, got EN=%v ZH=%v", audit.Restored["en"], audit.Restored["zh"])
		}

		groupMap := map[string]Group{}
		for _, g := range mapping.Groups {
			groupMap[pairKey(g.ENIDs, g.ZHIDs)] = g
		}

		g1, ok1 := groupMap[pairKey([]int{1, 2}, []int{1})]
		if !ok1 {
			t.Fatal("missing fine group [1, 2] <-> [1]")
		}
		if g1.Kind != KindDialogue {
			t.Errorf("g1 Kind = %s, want dialogue", g1.Kind)
		}

		g2, ok2 := groupMap[pairKey([]int{3}, []int{2})]
		if !ok2 {
			t.Fatal("missing fine group [3] <-> [2]")
		}
		if g2.Kind != KindOnScreenText {
			t.Errorf("g2 Kind = %s, want on_screen_text", g2.Kind)
		}
	})

	t.Run("FineDefaultWithTwoConflictingObservationsRejected", func(t *testing.T) {
		// Both fine group 2 and coarse group omit kind.
		// Window 2 observes [3]:[2] with "dialogue".
		// Window 3 observes [3]:[2] with "on_screen_text".
		// The two conflicting observations must reject the refinement!
		en := []Cue{
			cue(1, 1000, 2000, "One."),
			cue(2, 2000, 3000, "Two."),
			cue(3, 3000, 4000, "Three."),
		}
		zh := []Cue{
			cue(1, 1000, 3000, "一二。"),
			cue(2, 3000, 4000, "三。"),
		}
		bundle := testBundle(t, en, zh)

		batches := []batch{
			{
				Index:   0,
				ENInput: []int{1, 2, 3},
				ZHInput: []int{1, 2},
				Valid: []Group{
					{ENIDs: []int{1, 2}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
					{ENIDs: []int{3}, ZHIDs: []int{2}, Layer: LayerBottom, Origin: OriginModel, Kind: ""}, // fine group omits kind
				},
			},
			{
				Index:   1,
				ENInput: []int{1, 2, 3},
				ZHInput: []int{1, 2},
				Valid: []Group{
					{ENIDs: []int{1, 2, 3}, ZHIDs: []int{1, 2}, Layer: LayerBottom, Origin: OriginModel, Kind: ""}, // coarse group omits kind
				},
			},
			{
				Index:   2,
				ENInput: []int{3},
				ZHInput: []int{2},
				Valid: []Group{
					{ENIDs: []int{3}, ZHIDs: []int{2}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
				},
			},
			{
				Index:   3,
				ENInput: []int{3},
				ZHInput: []int{2},
				Valid: []Group{
					{ENIDs: []int{3}, ZHIDs: []int{2}, Layer: LayerBottom, Origin: OriginModel, Kind: KindOnScreenText},
				},
			},
		}

		accepted, audit := reconcile(bundle, batches, 100000)
		if len(audit.Conflicts) == 0 {
			t.Fatal("expected conflict when fine group with omitted kind receives two conflicting observations, got 0")
		}
		for _, g := range accepted {
			if g.Origin == OriginModel && reflect.DeepEqual(g.ENIDs, []int{1, 2}) {
				t.Errorf("fine group [1, 2] should not be accepted when [3]:[2] has kind conflict: %+v", accepted)
			}
		}
	})

	t.Run("FineDefaultWithSingleObservationAccepted", func(t *testing.T) {
		// Fine group 2 omits kind, coarse group omits kind.
		// Only Window 2 observes [3]:[2] with "dialogue" (no conflicting observation).
		// Must be accepted!
		en := []Cue{
			cue(1, 1000, 2000, "One."),
			cue(2, 2000, 3000, "Two."),
			cue(3, 3000, 4000, "Three."),
		}
		zh := []Cue{
			cue(1, 1000, 3000, "一二。"),
			cue(2, 3000, 4000, "三。"),
		}
		bundle := testBundle(t, en, zh)

		batches := []batch{
			{
				Index:   0,
				ENInput: []int{1, 2, 3},
				ZHInput: []int{1, 2},
				Valid: []Group{
					{ENIDs: []int{1, 2}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
					{ENIDs: []int{3}, ZHIDs: []int{2}, Layer: LayerBottom, Origin: OriginModel, Kind: ""}, // fine group omits kind
				},
			},
			{
				Index:   1,
				ENInput: []int{1, 2, 3},
				ZHInput: []int{1, 2},
				Valid: []Group{
					{ENIDs: []int{1, 2, 3}, ZHIDs: []int{1, 2}, Layer: LayerBottom, Origin: OriginModel, Kind: ""}, // coarse group omits kind
				},
			},
			{
				Index:   2,
				ENInput: []int{3},
				ZHInput: []int{2},
				Valid: []Group{
					{ENIDs: []int{3}, ZHIDs: []int{2}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
				},
			},
		}

		accepted, audit := reconcile(bundle, batches, 100000)
		if len(audit.Conflicts) != 0 {
			t.Fatalf("expected 0 conflicts when single observation is compatible, got %d: %+v", len(audit.Conflicts), audit.Conflicts)
		}
		if len(audit.Restored["en"]) != 0 || len(audit.Restored["zh"]) != 0 {
			t.Fatalf("expected 0 restored cues, got EN=%v ZH=%v", audit.Restored["en"], audit.Restored["zh"])
		}

		fine1Found, fine2Found := false, false
		for _, g := range accepted {
			if reflect.DeepEqual(g.ENIDs, []int{1, 2}) && reflect.DeepEqual(g.ZHIDs, []int{1}) {
				fine1Found = true
			}
			if reflect.DeepEqual(g.ENIDs, []int{3}) && reflect.DeepEqual(g.ZHIDs, []int{2}) {
				fine2Found = true
			}
		}
		if !fine1Found || !fine2Found {
			t.Errorf("expected both fine groups accepted, got %+v", accepted)
		}
	})

	t.Run("UnknownOrMissingKindDoesNotEraseTwoKnownConflicts", func(t *testing.T) {
		// Window 0: fine partition [1, 2] dialogue, [3] unknown
		// Window 1: coarse group [1, 2, 3] unknown
		// Window 2: [3] dialogue
		// Window 3: [3] on_screen_text
		// The presence of unknown or missing kinds cannot erase the conflict between dialogue and on_screen_text!
		en := []Cue{
			cue(1, 1000, 2000, "One."),
			cue(2, 2000, 3000, "Two."),
			cue(3, 3000, 4000, "Three."),
		}
		zh := []Cue{
			cue(1, 1000, 3000, "一二。"),
			cue(2, 3000, 4000, "三。"),
		}
		bundle := testBundle(t, en, zh)

		batches := []batch{
			{
				Index:   0,
				ENInput: []int{1, 2, 3},
				ZHInput: []int{1, 2},
				Valid: []Group{
					{ENIDs: []int{1, 2}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
					{ENIDs: []int{3}, ZHIDs: []int{2}, Layer: LayerBottom, Origin: OriginModel, Kind: KindUnknown},
				},
			},
			{
				Index:   1,
				ENInput: []int{1, 2, 3},
				ZHInput: []int{1, 2},
				Valid: []Group{
					{ENIDs: []int{1, 2, 3}, ZHIDs: []int{1, 2}, Layer: LayerBottom, Origin: OriginModel, Kind: KindUnknown},
				},
			},
			{
				Index:   2,
				ENInput: []int{3},
				ZHInput: []int{2},
				Valid: []Group{
					{ENIDs: []int{3}, ZHIDs: []int{2}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
				},
			},
			{
				Index:   3,
				ENInput: []int{3},
				ZHInput: []int{2},
				Valid: []Group{
					{ENIDs: []int{3}, ZHIDs: []int{2}, Layer: LayerBottom, Origin: OriginModel, Kind: KindOnScreenText},
				},
			},
		}

		accepted, audit := reconcile(bundle, batches, 100000)
		if len(audit.Conflicts) == 0 {
			t.Fatal("expected conflict when both dialogue and on_screen_text are present alongside unknown, got 0")
		}
		for _, g := range accepted {
			if g.Origin == OriginModel && reflect.DeepEqual(g.ENIDs, []int{1, 2}) {
				t.Errorf("fine group [1, 2] should not be accepted when [3]:[2] has known kind conflict: %+v", accepted)
			}
		}
	})

	t.Run("SingleKnownKindWithUnknownObservationPreservesCompatibility", func(t *testing.T) {
		// Window 0: fine partition [1, 2] dialogue, [3] "" (omitted kind)
		// Window 1: coarse group [1, 2, 3] "" (omitted kind)
		// Window 2: [3] dialogue
		// Window 3: [3] unknown
		// No second known kind (on_screen_text) is present.
		// The presence of unknown does not invent a known conflict between dialogue and on_screen_text,
		// preserving the original compatible refinement outcome.
		en := []Cue{
			cue(1, 1000, 2000, "One."),
			cue(2, 2000, 3000, "Two."),
			cue(3, 3000, 4000, "Three."),
		}
		zh := []Cue{
			cue(1, 1000, 3000, "一二。"),
			cue(2, 3000, 4000, "三。"),
		}
		bundle := testBundle(t, en, zh)

		batches := []batch{
			{
				Index:   0,
				ENInput: []int{1, 2, 3},
				ZHInput: []int{1, 2},
				Valid: []Group{
					{ENIDs: []int{1, 2}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
					{ENIDs: []int{3}, ZHIDs: []int{2}, Layer: LayerBottom, Origin: OriginModel, Kind: ""},
				},
			},
			{
				Index:   1,
				ENInput: []int{1, 2, 3},
				ZHInput: []int{1, 2},
				Valid: []Group{
					{ENIDs: []int{1, 2, 3}, ZHIDs: []int{1, 2}, Layer: LayerBottom, Origin: OriginModel, Kind: ""},
				},
			},
			{
				Index:   2,
				ENInput: []int{3},
				ZHInput: []int{2},
				Valid: []Group{
					{ENIDs: []int{3}, ZHIDs: []int{2}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
				},
			},
			{
				Index:   3,
				ENInput: []int{3},
				ZHInput: []int{2},
				Valid: []Group{
					{ENIDs: []int{3}, ZHIDs: []int{2}, Layer: LayerBottom, Origin: OriginModel, Kind: KindUnknown},
				},
			},
		}

		accepted, audit := reconcile(bundle, batches, 100000)
		if len(audit.Conflicts) != 0 {
			t.Fatalf("expected 0 conflicts when no opposing known kind exists, got %d: %+v", len(audit.Conflicts), audit.Conflicts)
		}
		if len(audit.Restored["en"]) != 0 || len(audit.Restored["zh"]) != 0 {
			t.Fatalf("expected 0 restored cues, got EN=%v ZH=%v", audit.Restored["en"], audit.Restored["zh"])
		}
		if len(accepted) != 2 {
			t.Fatalf("expected 2 fine groups accepted, got %d", len(accepted))
		}
	})
}
