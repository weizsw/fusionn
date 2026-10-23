package subtitlellm

// Portable regressions for the review round on fixed head 968bbf4. Every case
// below failed against that head and must keep failing if the underlying
// behavior regresses.

import (
	"strings"
	"testing"
)

// noWidthMeasurer disables width measurement so a case cannot accidentally rely
// on a merge succeeding.
func noWidthMeasurer(t *testing.T) {
	t.Helper()
	prior := fontWidthMeasurerOverride
	fontWidthMeasurerOverride = func(string, string, float64, int, float64, bool) (float64, bool) { return 0, false }
	t.Cleanup(func() { fontWidthMeasurerOverride = prior })
}

func TestGateFinalASSFloorAllowsNaturalCut(t *testing.T) {
	noWidthMeasurer(t)
	bundle := fixtureBundle(
		[]Cue{{ID: 1, Start: 1004, End: 3400, Display: "Alpha. Bravo.", Plain: "Alpha.\nBravo."}},
		[]Cue{{ID: 1, Start: 1004, End: 3400, Display: "甲。 乙。", Plain: "甲。\n乙。"}},
	)
	mapping := fixtureMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel}})
	cfg := fixtureConfig()
	// "Alpha. Bravo." is over the English limit, so the pair must split.
	cfg.EnChars = 8
	mapping = confirmAllSplits(t, bundle, mapping, cfg)
	out, _, err := renderWithAudits(bundle, mapping, cfg, fixtureStyle(), 1920, 1080, nil)
	if err != nil {
		t.Fatal(err)
	}
	events := parseDialogues(t, string(out))
	if len(events) != 2 {
		t.Fatalf("natural midpoint serializes to two 1200ms fragments but was rejected: %+v", events)
	}
	for _, e := range events {
		if e.end-e.start < 1200 {
			t.Fatalf("serialized fragment below sync floor: %+v", e)
		}
	}
}

// A label that must move off bottom speech must not drag neighbouring labels
// that sit on another source side.
func TestRunsBreakOnRawSourceSideChange(t *testing.T) {
	noWidthMeasurer(t)
	bundle := fixtureBundle(
		[]Cue{{ID: 1, Start: 2500, End: 3200, Display: "Bottom line.", Plain: "Bottom line."}},
		[]Cue{
			{ID: 1, Start: 1000, End: 1600, Display: "文字一", Plain: "文字一", Top: true, Alignment: 8},
			{ID: 2, Start: 1700, End: 2300, Display: "文字二", Plain: "文字二", Alignment: 2},
			{ID: 3, Start: 2400, End: 3000, Display: "文字三", Plain: "文字三", Alignment: 2},
		})
	groups := []Group{
		{ZHIDs: []int{1}, Layer: LayerTop, Origin: OriginModel, Kind: KindOnScreenText},
		{ZHIDs: []int{2}, Layer: LayerBottom, Origin: OriginModel, Kind: KindOnScreenText},
		{ZHIDs: []int{3}, Layer: LayerBottom, Origin: OriginModel, Kind: KindOnScreenText},
		{ENIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
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
				// 文字二 and 文字三 form one bottom run that collides with the
				// bottom line and moves up; 文字一 is already top in its source.
				wantTop := true
				gotTop := strings.HasPrefix(e.style, "Top")
				if gotTop != wantTop {
					t.Errorf("%s: style=%q wantTop=%v", label, e.style, wantTop)
				}
			}
		}
	}
	if len(seen) != 3 {
		t.Fatalf("probe lost sign text: %v", seen)
	}
}

func TestValidationRejectsReversedSourceOrder(t *testing.T) {
	cfg := fixtureConfig()
	bundle := fixtureBundle(
		[]Cue{{ID: 1, Start: 1000, End: 3000, Display: "Alpha.", Plain: "Alpha."}, {ID: 2, Start: 3000, End: 5000, Display: "Beta.", Plain: "Beta."}},
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "甲乙", Plain: "甲乙"}},
	)
	mapping := fixtureMapping(bundle, []Group{{ENIDs: []int{1, 2}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel}})
	bad := []renderedEvent{{group: 0, enIDs: []int{1, 2}, zhIDs: []int{1}, assStart: 1000, assEnd: 5000, enText: "Beta. Alpha.", zhText: "甲乙"}}
	v, err := verifyEpisode(bundle, mapping, cfg, bad)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Verify()) == 0 {
		t.Fatalf("validator accepted reversed original source order: %+v", v)
	}
}

func TestValidationAcceptsYesNoYesByDistinctSource(t *testing.T) {
	cfg := fixtureConfig()
	cfg.EnChars = 3
	bundle := fixtureBundle(
		[]Cue{{ID: 1, Start: 1000, End: 3000, Display: "Yes.", Plain: "Yes."}, {ID: 2, Start: 3000, End: 5000, Display: "No.", Plain: "No."}, {ID: 3, Start: 5000, End: 7000, Display: "Yes.", Plain: "Yes."}},
		[]Cue{{ID: 1, Start: 1000, End: 7000, Display: "回答", Plain: "回答"}},
	)
	mapping := fixtureMapping(bundle, []Group{{ENIDs: []int{1, 2, 3}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel}})
	v, events, err := validateEpisode(bundle, mapping, cfg, fixtureStyle(), 1920, 1080, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("probe requires three original speech events, got %d", len(events))
	}
	if failures := v.Verify(); len(failures) != 0 {
		t.Fatalf("valid original Yes./No./Yes. with different identities rejected: %v", failures)
	}
}

func TestValidationExcludesFormatOnlySource(t *testing.T) {
	bundle := fixtureBundle(
		[]Cue{{ID: 1, Start: 1000, End: 3000, Display: "Normal.", Plain: "Normal."}, {ID: 2, Start: 4000, End: 5000, Raw: "{\\an8}", Top: true, Alignment: 8}},
		[]Cue{{ID: 1, Start: 1000, End: 3000, Display: "正常", Plain: "正常"}},
	)
	mapping := fixtureMapping(bundle, []Group{
		{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel},
		{ENIDs: []int{2}, Layer: LayerTop, Origin: OriginModel},
	})
	v, events, err := validateEpisode(bundle, mapping, fixtureConfig(), fixtureStyle(), 1920, 1080, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected one visible event, got %d", len(events))
	}
	if failures := v.Verify(); len(failures) != 0 {
		t.Fatalf("valid omission of format-only source rejected: %v", failures)
	}
}

func TestUnicodeFamilyEmojiStaysWhole(t *testing.T) {
	noWidthMeasurer(t)
	cfg := fixtureConfig()
	cfg.ZhChars = 4
	family := "👨‍👩‍👧‍👦"
	bundle := fixtureBundle(
		[]Cue{{ID: 1, Start: 1000, End: 7000, Display: "English whole.", Plain: "English whole."}},
		[]Cue{{ID: 1, Start: 1000, End: 7000, Display: family + " X", Plain: family + "\nX"}},
	)
	mapping := fixtureMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel}})
	_, events, err := renderWithAudits(bundle, mapping, cfg, fixtureStyle(), 1920, 1080, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		for _, part := range sourceLines(e.zhText) {
			if strings.ContainsAny(part, "👨👩👧👦") && !strings.Contains(part, family) {
				t.Errorf("family emoji was divided at a generated boundary: %q", part)
			}
		}
	}
}

func TestMigrationReservesSideAgainstOverlappingPartition(t *testing.T) {
	prior := fontWidthMeasurerOverride
	fontWidthMeasurerOverride = func(string, string, float64, int, float64, bool) (float64, bool) { return 5000, true }
	t.Cleanup(func() { fontWidthMeasurerOverride = prior })
	cfg := fixtureConfig()
	cfg.ZhChars, cfg.EnChars = 4, 6
	bundle := fixtureBundle(
		[]Cue{{ID: 1, Start: 1000, End: 4000, Display: "Sign", Plain: "Sign"}, {ID: 2, Start: 3000, End: 4200, Display: "Alpha. Bravo.", Plain: "Alpha.\nBravo."}},
		[]Cue{{ID: 1, Start: 1000, End: 4000, Display: "标牌", Plain: "标牌"}, {ID: 2, Start: 3000, End: 4200, Display: "甲乙丙 丁戊己", Plain: "甲乙丙\n丁戊己"}},
	)
	mapping := fixtureMapping(bundle, []Group{
		{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindOnScreenText},
		{ENIDs: []int{2}, ZHIDs: []int{2}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
	})
	out, events, err := renderWithAudits(bundle, mapping, cfg, fixtureStyle(), 1920, 1080, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("migration must reserve top against overlapping partition, got %d events: %s", len(events), out)
	}
	for _, e := range events {
		if e.group == 1 && (len(sourceLines(e.zhText)) != 2 || len(sourceLines(e.enText)) != 2 || e.alignment >= 7) {
			t.Errorf("remaining ordinary4-line block must stay whole at bottom: %+v", e)
		}
		if e.group == 0 && e.alignment < 7 {
			t.Errorf("sign must have really moved top: %+v", e)
		}
	}
}

func TestValidationAcceptsRepeatsInsideOneSource(t *testing.T) {
	noWidthMeasurer(t)
	cfg := fixtureConfig()
	cfg.EnChars = 6
	bundle := fixtureBundle(
		[]Cue{{ID: 1, Start: 1000, End: 7000, Display: "Alpha. Beta. Alpha.", Plain: "Alpha.\nBeta.\nAlpha.", Alignment: 2}},
		[]Cue{{ID: 1, Start: 1000, End: 7000, Display: "甲", Plain: "甲", Alignment: 2}},
	)
	mapping := fixtureMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel}})
	v, events, err := validateEpisode(bundle, mapping, cfg, fixtureStyle(), 1920, 1080, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("probe requires three legitimate fragments from the original source, got %d", len(events))
	}
	if failures := v.Verify(); len(failures) != 0 {
		t.Fatalf("valid original Alpha./Beta./Alpha. falsely treated as extra replay: %v", failures)
	}
}
