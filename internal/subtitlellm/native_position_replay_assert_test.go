package subtitlellm

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const (
	positionRecordDirEnv      = "FUSIONN_POSITION_RECORD_DIR"
	positionRecordManifestEnv = "FUSIONN_POSITION_RECORD_MANIFEST"
	positionSourcesEnv        = "FUSIONN_POSITION_SOURCES"
	positionStyleEnv          = "FUSIONN_POSITION_STYLE"
	positionWordsEnv          = "FUSIONN_POSITION_WORDS"
	positionBaselineEnv       = "FUSIONN_POSITION_BASELINE_MAPPING"
	positionAssEnv            = "FUSIONN_POSITION_ASS"
)

// expectedPair is one bilingual group that must survive reconciliation.
type expectedPair struct {
	en []int
	zh []int
}

// positionPairs are the 7 groups recovered by the native-position fix.
var positionPairs = []expectedPair{
	{en: []int{25, 26}, zh: []int{26}},
	{en: []int{112, 113}, zh: []int{91}},
	{en: []int{116, 117}, zh: []int{94}},
	{en: []int{133, 134}, zh: []int{108}},
	{en: []int{137}, zh: []int{113}},
	{en: []int{138}, zh: []int{114}},
	{en: []int{194}, zh: []int{168}},
}

// truncationPairs are the 5 groups recovered by the truncated-context fix.
var truncationPairs = []expectedPair{
	{en: []int{160, 161}, zh: []int{132}},
	{en: []int{350, 351}, zh: []int{330, 331}},
	{en: []int{409, 410, 411}, zh: []int{384, 385}},
	{en: []int{492}, zh: []int{467, 468}},
	{en: []int{499, 500}, zh: []int{474}},
}

// expectedConflict is one conflict that must still be refused after both fixes.
type expectedConflict struct {
	en    []int
	zh    []int
	layer string
	kind  string
	owner int
}

var retainedConflicts = []expectedConflict{
	{en: []int{}, zh: []int{81}, layer: LayerTop, kind: KindOnScreenText, owner: 1},
	{en: []int{93, 94}, zh: []int{}, layer: LayerBottom, kind: KindDialogue, owner: 1},
	{en: []int{97, 98, 99}, zh: []int{}, layer: LayerBottom, kind: KindDialogue, owner: 1},
	{en: []int{98}, zh: []int{}, layer: LayerBottom, kind: KindDialogue, owner: 2},
	{en: []int{99}, zh: []int{}, layer: LayerBottom, kind: KindDialogue, owner: 2},
	{en: []int{}, zh: []int{169}, layer: LayerTop, kind: KindOnScreenText, owner: 3},
	{en: []int{495, 496}, zh: []int{471}, layer: LayerBottom, kind: KindDialogue, owner: 12},
}

func pairKey(en, zh []int) string {
	return fmt.Sprintf("en:%v_zh:%v", en, zh)
}

// TestReplayRecordedResponsesWithPositionFix replays the recorded native responses
// offline and proves that both the native-position fix and the truncated-context fix
// hold on the same inputs, while the 7 genuine conflicts are still refused.
//
// This test is explicit opt-in: without FUSIONN_POSITION_RECORD_DIR (or an explicit
// manifest path) it skips cleanly, so default repository tests stay portable.
func TestReplayRecordedResponsesWithPositionFix(t *testing.T) {
	dir := os.Getenv(positionRecordDirEnv)
	manifestPath := os.Getenv(positionRecordManifestEnv)
	if dir == "" && manifestPath == "" {
		t.Skipf("skipping recorded replay: set %s or %s to run", positionRecordDirEnv, positionRecordManifestEnv)
	}

	cfg := replayConfig{
		Dir:          dir,
		ManifestPath: manifestPath,
		SourcesPath:  os.Getenv(positionSourcesEnv),
		StylePath:    os.Getenv(positionStyleEnv),
		WordsPath:    os.Getenv(positionWordsEnv),
		ASSPath:      os.Getenv(positionAssEnv),
	}
	if cfg.SourcesPath == "" && dir != "" {
		cfg.SourcesPath = filepath.Join(dir, "sources.json")
	}
	if cfg.StylePath == "" && dir != "" {
		cfg.StylePath = filepath.Join(dir, "delivered-style.json")
	}
	if cfg.WordsPath == "" && dir != "" {
		cfg.WordsPath = filepath.Join(dir, "word-timings.json")
	}
	// An ASS path given explicitly by the caller is a declaration and must be valid.
	// The auto-derived default is only used when the file actually exists, so a
	// directory without a delivered ASS still replays the mapping.
	if cfg.ASSPath == "" && dir != "" {
		cand := filepath.Join(dir, "E01.zh20-bilingual-layout.ass")
		if _, err := os.Stat(cand); err == nil {
			cfg.ASSPath = cand
		}
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

	res, err := replayRecordedResponses(cfg)
	if err != nil {
		t.Fatalf("replay recorded responses: %v", err)
	}
	mapping, audit, bundle := res.Mapping, res.Audit, res.Bundle

	// 1. The position fix must remove every native_position_conflict, and the only
	// remaining rejection is the genuine over-span row in window 1.
	for _, r := range audit.Rejected {
		if r.Reason == "native_position_conflict" {
			t.Fatalf("native_position_conflict still present: %+v", r)
		}
	}
	if len(audit.Rejected) != 1 || audit.Rejected[0].Reason != "group_span_over_30s" {
		t.Fatalf("expected exactly one group_span_over_30s rejection, got %+v", audit.Rejected)
	}

	// 2. Layout metadata on the source cues is never mutated.
	rawSources, err := os.ReadFile(cfg.SourcesPath)
	if err != nil {
		t.Fatalf("re-read sources: %v", err)
	}
	var fresh Bundle
	if err := json.Unmarshal(rawSources, &fresh); err != nil {
		t.Fatalf("re-decode sources: %v", err)
	}
	for i, c := range bundle.EN {
		if c.Top != fresh.EN[i].Top || c.Alignment != fresh.EN[i].Alignment {
			t.Fatalf("EN cue %d position mutated: top %v->%v align %d->%d",
				c.ID, fresh.EN[i].Top, c.Top, fresh.EN[i].Alignment, c.Alignment)
		}
	}
	for i, c := range bundle.ZH {
		if c.Top != fresh.ZH[i].Top || c.Alignment != fresh.ZH[i].Alignment {
			t.Fatalf("ZH cue %d position mutated: top %v->%v align %d->%d",
				c.ID, fresh.ZH[i].Top, c.Top, fresh.ZH[i].Alignment, c.Alignment)
		}
	}

	// 3. Every source cue appears exactly once in the accepted mapping.
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
			t.Fatalf("EN cue %d appears %d times (want 1)", c.ID, enCounts[c.ID])
		}
	}
	for _, c := range bundle.ZH {
		if zhCounts[c.ID] != 1 {
			t.Fatalf("ZH cue %d appears %d times (want 1)", c.ID, zhCounts[c.ID])
		}
	}

	// 4. All 12 recovered groups (7 position + 5 truncation) exist and are model-owned.
	groupMap := map[string]Group{}
	for _, g := range mapping.Groups {
		groupMap[pairKey(g.ENIDs, g.ZHIDs)] = g
	}
	allPairs := append(append([]expectedPair{}, positionPairs...), truncationPairs...)
	for _, p := range allPairs {
		key := pairKey(p.en, p.zh)
		g, ok := groupMap[key]
		if !ok {
			t.Errorf("recovered group %s missing from mapping", key)
			continue
		}
		if g.Origin != OriginModel {
			t.Errorf("recovered group %s origin = %s, want model", key, g.Origin)
		}
	}

	// 5. None of those 36 cues may fall back to one-sided restoration.
	restoredEN, restoredZH := map[int]bool{}, map[int]bool{}
	for _, id := range audit.Restored["en"] {
		restoredEN[id] = true
	}
	for _, id := range audit.Restored["zh"] {
		restoredZH[id] = true
	}
	for _, p := range allPairs {
		for _, id := range p.en {
			if restoredEN[id] {
				t.Errorf("EN cue %d wrongly restored as one-sided", id)
			}
		}
		for _, id := range p.zh {
			if restoredZH[id] {
				t.Errorf("ZH cue %d wrongly restored as one-sided", id)
			}
		}
	}

	// 6. Exact delta against the v12 baseline: 36 one-sided groups become 12 bilingual
	// groups, and every other group keeps its layer, origin, and kind.
	if baselinePath != "" {
		rawBaseline, err := os.ReadFile(baselinePath)
		if err != nil {
			t.Fatalf("read baseline %s: %v", baselinePath, err)
		}
		var baseline Mapping
		if err := json.Unmarshal(rawBaseline, &baseline); err != nil {
			t.Fatalf("decode baseline %s: %v", baselinePath, err)
		}

		removed := map[string]bool{}
		added := map[string]bool{}
		for _, p := range allPairs {
			for _, id := range p.en {
				removed[pairKey([]int{id}, []int{})] = true
			}
			for _, id := range p.zh {
				removed[pairKey([]int{}, []int{id})] = true
			}
			added[pairKey(p.en, p.zh)] = true
		}
		if len(removed) != 36 {
			t.Fatalf("expected 36 one-sided baseline groups to disappear, got %d", len(removed))
		}
		if len(added) != 12 {
			t.Fatalf("expected 12 bilingual groups to appear, got %d", len(added))
		}

		baselineMap := map[string]Group{}
		for _, g := range baseline.Groups {
			baselineMap[pairKey(g.ENIDs, g.ZHIDs)] = g
		}
		normKind := func(k string) string {
			if k == KindUnknown {
				return ""
			}
			return k
		}
		for key, bg := range baselineMap {
			ng, exists := groupMap[key]
			if removed[key] {
				if exists {
					t.Fatalf("baseline one-sided group %s still present", key)
				}
				continue
			}
			if !exists {
				t.Fatalf("baseline group %s disappeared from the new mapping", key)
			}
			if ng.Layer != bg.Layer || ng.Origin != bg.Origin || normKind(ng.Kind) != normKind(bg.Kind) {
				t.Fatalf("group %s changed: new=%+v baseline=%+v", key, ng, bg)
			}
		}
		for key := range groupMap {
			if added[key] {
				if _, exists := baselineMap[key]; exists {
					t.Fatalf("recovered group %s already existed in the baseline", key)
				}
				continue
			}
			if _, exists := baselineMap[key]; !exists {
				t.Fatalf("unexpected new group %s outside the recovered set", key)
			}
		}
		want := len(baseline.Groups) - len(removed) + len(added)
		if len(mapping.Groups) != want {
			t.Fatalf("group count %d != baseline %d - %d + %d = %d",
				len(mapping.Groups), len(baseline.Groups), len(removed), len(added), want)
		}
	}

	// 7. Exactly the 7 genuine conflicts survive, with their real members and windows.
	if len(audit.Conflicts) != len(retainedConflicts) {
		t.Fatalf("expected %d retained conflicts, got %d: %+v",
			len(retainedConflicts), len(audit.Conflicts), audit.Conflicts)
	}
	for i, want := range retainedConflicts {
		got := audit.Conflicts[i]
		if !reflect.DeepEqual(got.ENIDs, want.en) || !reflect.DeepEqual(got.ZHIDs, want.zh) ||
			got.Layer != want.layer || got.Kind != want.kind || got.OwnerWindow != want.owner {
			t.Errorf("conflict[%d]: got en=%v zh=%v layer=%s kind=%s owner=%d, want en=%v zh=%v layer=%s kind=%s owner=%d",
				i, got.ENIDs, got.ZHIDs, got.Layer, got.Kind, got.OwnerWindow,
				want.en, want.zh, want.layer, want.kind, want.owner)
		}
	}

	// 8. Restored one-sided counts are the computed remainder, not a coverage proxy.
	if len(audit.Restored["en"]) != 16 {
		t.Errorf("expected 16 restored EN cues, got %d: %v", len(audit.Restored["en"]), audit.Restored["en"])
	}
	if len(audit.Restored["zh"]) != 4 {
		t.Errorf("expected 4 restored ZH cues, got %d: %v", len(audit.Restored["zh"]), audit.Restored["zh"])
	}

	// 9. Offline replay must report recorded responses, not HTTP cache hits.
	if audit.CacheHits != 0 {
		t.Errorf("offline replay must report 0 cache hits, got %d", audit.CacheHits)
	}
	if res.RecordedCount != 15 {
		t.Errorf("expected 15 replayed recorded responses, got %d", res.RecordedCount)
	}

	t.Logf("replayed %d recorded responses with 0 network requests: %d groups, %d rejected, %d conflicts, restored EN=%d ZH=%d",
		res.RecordedCount, len(mapping.Groups), len(audit.Rejected), len(audit.Conflicts),
		len(audit.Restored["en"]), len(audit.Restored["zh"]))
}
