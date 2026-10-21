package subtitlellm

import (
	"strings"
	"testing"
)

func TestParent33EffectivePositionChangeAlsoBreaksRun(t *testing.T) {
	noWidthMeasurer(t)
	bundle := fixtureBundle(nil, []Cue{
		{ID: 1, Start: 1000, End: 1600, Display: "文字一", Plain: "文字一", Alignment: 2},
		{ID: 2, Start: 1700, End: 2300, Display: "文字二", Plain: "文字二", Alignment: 2},
		{ID: 3, Start: 2400, End: 3000, Display: "文字三", Plain: "文字三", Alignment: 2},
		{ID: 4, Start: 2500, End: 3200, Display: "顶部台词", Plain: "顶部台词", Top: true, Alignment: 8},
	})
	groups := []Group{
		{ZHIDs: []int{1}, Layer: LayerTop, Origin: OriginModel, Kind: KindOnScreenText},
		{ZHIDs: []int{2}, Layer: LayerBottom, Origin: OriginModel, Kind: KindOnScreenText},
		{ZHIDs: []int{3}, Layer: LayerTop, Origin: OriginModel, Kind: KindOnScreenText},
		{ZHIDs: []int{4}, Layer: LayerTop, Origin: OriginModel, Kind: KindDialogue},
	}
	out, _, err := renderWithAudits(bundle, fixtureMapping(bundle, groups), fixtureConfig(), fixtureStyle(), 1920, 1080, nil)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, e := range parseDialogues(t, string(out)) {
		for _, label := range []string{"文字一", "文字二", "文字三"} {
			if strings.Contains(e.text, label) {
				seen[label] = true
				wantTop := label == "文字一"
				gotTop := strings.HasPrefix(e.style, "Top")
				if gotTop != wantTop {
					t.Errorf("%s migrated across effective-position change: got style %q wantTop=%v", label, e.style, wantTop)
				}
			}
		}
	}
	if len(seen) != 3 {
		t.Fatalf("probe lost visible signs: %v", seen)
	}
}

func TestParent33AdjacentOriginalRepetitionIsNotHold(t *testing.T) {
	noWidthMeasurer(t)
	cfg := fixtureConfig()
	cfg.EnChars = 3
	bundle := fixtureBundle(
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "No. No.", Plain: "No.\nNo."}},
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "不要", Plain: "不要"}},
	)
	mapping := fixtureMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel}})
	v, events, err := validateEpisode(bundle, mapping, cfg, fixtureStyle(), 1920, 1080, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("probe requires 2 real source fragments, got %d", len(events))
	}
	if failures := v.Verify(); len(failures) != 0 {
		t.Fatalf("two adjacent No. source lines are valid, not a removable hold: %v", failures)
	}
}

func TestParent33FallbackWordCandidatesUseASSFloor(t *testing.T) {
	cfg := fixtureConfig()
	cfg.EnChars = 8
	bundle := fixtureBundle(
		[]Cue{{ID: 1, Start: 1004, End: 2600, Display: "Alpha. Bravo.", Plain: "Alpha.\nBravo."}},
		[]Cue{{ID: 1, Start: 1004, End: 2600, Display: "甲", Plain: "甲"}},
	)
	candidates := WordAlignmentCues(bundle, cfg)
	if len(candidates) != 1 || candidates[0].ID != 1 {
		t.Fatalf("1.6s serialized cue supports 2x0.8s fragments and needs optional word alignment, got %v", candidates)
	}
}

func TestParent33ChineseSourceCutUsesRoundedFallbackBudget(t *testing.T) {
	noWidthMeasurer(t)
	cfg := fixtureConfig()
	cfg.ZhChars = 4
	bundle := fixtureBundle(
		[]Cue{{ID: 1, Start: 1004, End: 2600, Display: "Short.", Plain: "Short."}},
		[]Cue{{ID: 1, Start: 1004, End: 2600, Display: "甲 乙丙丁戊", Plain: "甲\n乙丙丁戊"}},
	)
	mapping := fixtureMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel}})
	_, events, err := renderWithAudits(bundle, mapping, cfg, fixtureStyle(), 1920, 1080, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("probe requires 2 fallback fragments, got %d", len(events))
	}
	want := []string{"甲", "乙丙丁戊"}
	for i, e := range events {
		if normText(e.zhText) != want[i] {
			t.Errorf("fragment %d lost preferred original line despite exact2x0.8s ASS budget: got %q want %q", i, e.zhText, want[i])
		}
		if e.assEnd-e.assStart < 800 {
			t.Errorf("fragment %d is below final ASS floor", i)
		}
	}
}

func TestParent33PartialHoldWithRepeatedPrefixRemainsValid(t *testing.T) {
	noWidthMeasurer(t)
	cfg := fixtureConfig()
	cfg.EnChars, cfg.ZhChars = 8, 3
	bundle := fixtureBundle(
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "No. No. Run.", Plain: "No.\nNo. Run."}},
		[]Cue{
			{ID: 1, Start: 1000, End: 1500, Display: "甲乙丙丁", Plain: "甲乙丙丁"},
			{ID: 2, Start: 1500, End: 5000, Display: "戊己庚辛", Plain: "戊己庚辛"},
		},
	)
	mapping := fixtureMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1, 2}, Layer: LayerBottom, Origin: OriginModel}})
	v, events, err := validateEpisode(bundle, mapping, cfg, fixtureStyle(), 1920, 1080, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 || normText(events[0].enText) != "No." || normText(events[1].enText) != "No." || normText(events[2].enText) != "No.Run." {
		t.Fatalf("probe must exercise source No./No.Run. with legitimate first-piece hold across Chinese: %+v", events)
	}
	if failures := v.Verify(); len(failures) > 0 {
		t.Fatalf("valid partial hold with genuinely repeated source prefix rejected: %v", failures)
	}
}
