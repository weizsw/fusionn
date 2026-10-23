package subtitlellm

import (
	"strings"
	"testing"

	"github.com/fusionn/internal/config"
)

// Boundary acceptance B01--B16 from docs/llm-subtitle-integration.zh-CN.md
// section 补充边界验收. Rows that are already covered elsewhere cite the
// existing test in the comment instead of duplicating it.

// --- B01/B02: report rounding to ASS centiseconds ---------------------------

// assEventTimes returns start/end in ASS centiseconds as serialized.
func assEventTimes(t *testing.T, content string) [][2]int {
	t.Helper()
	var out [][2]int
	for _, d := range parseDialogues(t, content) {
		out = append(out, [2]int{d.start / 10, d.end / 10})
	}
	return out
}

// B01: exactly at 1.2s / 0.8s floors and 10ms below, measured on serialized ASS.
func TestB01_SyncAndFallbackFloorBoundaries(t *testing.T) {
	// Total duration 2400ms: exactly 2*1.2s, so a proportional cut at the exact
	// middle is admissible; 2390ms cannot produce two >=1.2s fragments.
	// Below the floor the pair cannot split in sync: only the over-limit
	// English splits, and the Chinese stays whole.
	cases := []struct {
		name       string
		start, end int
		want       []string
		synced     bool
	}{
		{"exact_2x1200ms", 1000, 3400, []string{"甲。Alpha.", "乙。Bravo."}, true},
		{"10ms_below", 1000, 3390, []string{"甲。 乙。Alpha.", "甲。 乙。Bravo."}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := renderTestConfig()
			// "Alpha. Bravo." is over the English limit, so the pair must split.
			cfg.EnChars = 8
			cfg.ZhChars = 25
			// Equal-length halves put the natural proportional cut exactly at the
			// midpoint, so the 1.2s floor is met or missed by exactly 10ms.
			bundle := makeBundle(
				[]Cue{{ID: 1, Start: tc.start, End: tc.end, Display: "Alpha. Bravo.",
					Plain: "Alpha.\nBravo."}},
				[]Cue{{ID: 1, Start: tc.start, End: tc.end, Display: "甲。 乙。", Plain: "甲。\n乙。"}},
			)
			mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue}})
			mapping = confirmAllSplits(t, bundle, mapping, cfg)
			out, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
			if err != nil {
				t.Fatal(err)
			}
			events := parseDialogues(t, string(out))
			if len(events) != len(tc.want) {
				t.Fatalf("events = %d, want %d", len(events), len(tc.want))
			}
			for i, want := range tc.want {
				assertGlyphs(t, events[i].text, want)
			}
			times := assEventTimes(t, string(out))
			for i, span := range times {
				if span[1] <= span[0] {
					t.Fatalf("event %d has zero/negative ASS duration: %v", i, span)
				}
				if i > 0 && times[i-1][1] > span[0] {
					t.Fatalf("events %d and %d overlap after ASS rounding: %v %v", i-1, i, times[i-1], span)
				}
			}
			if tc.synced {
				for i, span := range times {
					if span[1]-span[0] < minSyncFragmentMS/10 {
						t.Fatalf("fragment %d shorter than 1.2s in ASS centiseconds: %v", i, span)
					}
				}
			}
		})
	}
}

// B01 fallback floor: 0.8s per new fallback fragment, and 10ms below.
func TestB01_FallbackFloorBoundaries(t *testing.T) {
	cases := []struct {
		name       string
		start, end int
		wantEvents int
	}{
		{"exact_2x800ms", 1000, 2600, 2},
		{"10ms_below", 1000, 2590, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := renderTestConfig()
			cfg.ZhChars = 5 // forced fallback: Chinese over cap
			bundle := makeBundle(
				[]Cue{{ID: 1, Start: tc.start, End: tc.end, Display: "Short english whole."}},
				[]Cue{{ID: 1, Start: tc.start, End: tc.end, Display: "第一段中文 第二段中文",
					Plain: "第一段中文\n第二段中文"}},
			)
			mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue}})
			out, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
			if err != nil {
				t.Fatal(err)
			}
			events := parseDialogues(t, string(out))
			if len(events) != tc.wantEvents {
				t.Fatalf("events = %d, want %d", len(events), tc.wantEvents)
			}
			for i, span := range assEventTimes(t, string(out)) {
				if span[1] <= span[0] {
					t.Fatalf("event %d zero-duration after ASS rounding: %v", i, span)
				}
				if tc.wantEvents == 2 && span[1]-span[0] < minFallbackFragmentMS/10 {
					t.Fatalf("fallback fragment %d below 0.8s: %v", i, span)
				}
			}
		})
	}
}

// B02: a cut that rounds onto the boundary must not emit a zero-length event.
func TestB02_NoZeroDurationAfterRounding(t *testing.T) {
	cfg := renderTestConfig()
	cfg.ZhChars = 5
	// 5ms shy of the 0.8s floor on the first side: a naive split would round to
	// an 800ms/795ms pair where the second is below the floor, so no split.
	bundle := makeBundle(
		[]Cue{{ID: 1, Start: 1000, End: 2605, Display: "Short english whole."}},
		[]Cue{{ID: 1, Start: 1000, End: 2605, Display: "第一段中文内容 第二段中文内容",
			Plain: "第一段中文内容\n第二段中文内容"}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue}})
	out, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	for i, span := range assEventTimes(t, string(out)) {
		if span[1] <= span[0] {
			t.Fatalf("zero-duration ASS event %d: %v", i, span)
		}
	}
}

// --- B03: on-screen text grouping threshold --------------------------------

// B03: 490/500ms keep the run, 510ms breaks it.
func TestB03_OnScreenTextGroupGapThreshold(t *testing.T) {
	cases := []struct {
		name      string
		gapMS     int
		wantGroup bool
	}{
		{"gap_490ms", 490, true},
		{"gap_500ms", 500, true},
		{"gap_510ms", 510, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := renderTestConfig()
			firstEnd := 1000 + 500
			secondStart := firstEnd + tc.gapMS
			bundle := makeBundle(
				[]Cue{
					{ID: 1, Start: 1000, End: firstEnd, Display: "Sign one"},
					{ID: 2, Start: secondStart, End: secondStart + 500, Display: "Sign two"},
					{ID: 3, Start: secondStart - 100, End: secondStart + 600, Display: "Speech"},
				},
				[]Cue{
					{ID: 1, Start: 1000, End: firstEnd, Display: "文字一"},
					{ID: 2, Start: secondStart, End: secondStart + 500, Display: "文字二"},
					{ID: 3, Start: secondStart - 100, End: secondStart + 600, Display: "台词"},
				},
			)
			mapping := makeMapping(bundle, []Group{
				{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindOnScreenText},
				{ENIDs: []int{2}, ZHIDs: []int{2}, Layer: LayerBottom, Origin: OriginModel, Kind: KindOnScreenText},
				{ENIDs: []int{3}, ZHIDs: []int{3}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
			})
			out, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
			if err != nil {
				t.Fatal(err)
			}
			events := parseDialogues(t, string(out))
			var firstTop, secondTop bool
			for _, e := range events {
				switch {
				case strings.Contains(e.text, "文字一"):
					firstTop = strings.HasPrefix(e.style, "Top")
				case strings.Contains(e.text, "文字二"):
					secondTop = strings.HasPrefix(e.style, "Top")
				}
			}
			if tc.wantGroup && firstTop != secondTop {
				t.Fatalf("gap %dms: run split unexpectedly (first top=%v, second top=%v)", tc.gapMS, firstTop, secondTop)
			}
			if !tc.wantGroup && firstTop {
				t.Fatalf("gap %dms: first cue should not have joined a run and moved", tc.gapMS)
			}
		})
	}
}

// --- B04: adjacency vs a real 10ms intersection ----------------------------

// B04: contiguous end==start is not an overlap; a real 10ms overlap is.
func TestB04_AdjacencyVersusRealOverlap(t *testing.T) {
	cases := []struct {
		name                   string
		speechStart, speechEnd int
		wantMove               bool
	}{
		// Contiguous (speech starts exactly when the sign ends) is not a conflict.
		{"adjacent_no_overlap", 2000, 3000, false},
		// A real 10ms intersection is a conflict and must be treated as occupied.
		{"real_10ms_overlap", 1990, 3000, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := renderTestConfig()
			bundle := makeBundle(
				[]Cue{
					{ID: 1, Start: 1000, End: 2000, Display: "Sign text"},
					{ID: 2, Start: tc.speechStart, End: tc.speechEnd, Display: "Speech text"},
				},
				[]Cue{
					{ID: 1, Start: 1000, End: 2000, Display: "文字"},
					{ID: 2, Start: tc.speechStart, End: tc.speechEnd, Display: "台词"},
				},
			)
			mapping := makeMapping(bundle, []Group{
				{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindOnScreenText},
				{ENIDs: []int{2}, ZHIDs: []int{2}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
			})
			out, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
			if err != nil {
				t.Fatal(err)
			}
			var moved bool
			for _, e := range parseDialogues(t, string(out)) {
				if strings.Contains(e.text, "文字") && strings.HasPrefix(e.style, "Top") {
					moved = true
				}
			}
			if moved != tc.wantMove {
				t.Fatalf("case %s: moved=%v, want %v", tc.name, moved, tc.wantMove)
			}
		})
	}
}

// --- B05/B07: per-language consecutive visibility, gaps -------------------

// B05: a held side is measured by its own consecutive visible window, and a
// short middle event must not delete valid content.
func TestB05_HoldVisibilityPerLanguage(t *testing.T) {
	bundle, mapping, cfg, style := nToOneFixture(t)
	v, events, err := validateEpisode(bundle, mapping, cfg, style, 1920, 1080, nil)
	if err != nil {
		t.Fatal(err)
	}
	if failures := v.Verify(); len(failures) != 0 {
		t.Fatalf("hold across counterpart cuts failed validation: %v", failures)
	}
	// The held Chinese side must cover the union of its own windows, not a sum
	// of per-emission lengths with restarts.
	var zhWindows [][2]int
	for _, e := range events {
		if e.zhText != "" {
			zhWindows = append(zhWindows, [2]int{e.assStart, e.assEnd})
		}
	}
	merged := union(zhWindows)
	if len(merged) != 1 {
		t.Fatalf("held Chinese should form one consecutive window, got %v", merged)
	}
}

// B07: a real gap inside a hold must not be summed into fake reading time.
func TestB07_HoldDoesNotSumDiscontinuousFlashes(t *testing.T) {
	cfg := fixtureConfig()
	bundle := fixtureBundle(
		[]Cue{
			{ID: 1, Start: 1000, End: 2000, Display: "First english slice."},
			{ID: 2, Start: 3000, End: 4000, Display: "Second english slice."},
		},
		[]Cue{{ID: 1, Start: 1000, End: 4000, Display: "保持的中文", Plain: "保持的中文"}},
	)
	mapping := fixtureMapping(bundle, []Group{{ENIDs: []int{1, 2}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel}})
	_, events, err := renderWithAudits(bundle, mapping, cfg, fixtureStyle(), 1920, 1080, nil)
	if err != nil {
		t.Fatal(err)
	}
	var gaps int
	for i := 1; i < len(events); i++ {
		if events[i].assStart > events[i-1].assEnd {
			gaps++
		}
	}
	if gaps == 0 {
		t.Fatal("source gap was silently filled; B07 requires no new gap filling")
	}
	// Content must not be shifted into the gap window.
	for _, e := range events {
		if e.zhText != "" && normText(e.zhText) != normText("保持的中文") {
			t.Fatalf("held text changed: %q", e.zhText)
		}
	}
}

// --- B05/B06/B07: validator identity distinctions --------------------------

// B06 already covered: identical text from different source IDs stays valid
// (TestReviewProbeValidatorAcceptsSamePhraseDifferentSourceInOneGroup), and a
// true A-B-A-B replay fails (TestReviewProbeValidatorRejectsOneToOneReplay,
// TestNToOneHeldSourceKeepsSinglePassOrder).

// B06 repeat inside one source cue is preserved, not deduplicated.
func TestB06_RepeatedPhraseInsideOneSourceIsPreserved(t *testing.T) {
	cfg := fixtureConfig()
	bundle := fixtureBundle(
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "No. No. No.", Plain: "No. No. No."}},
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "不 不 不", Plain: "不 不 不"}},
	)
	mapping := fixtureMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel}})
	v, events, err := validateEpisode(bundle, mapping, cfg, fixtureStyle(), 1920, 1080, nil)
	if err != nil {
		t.Fatal(err)
	}
	if failures := v.Verify(); len(failures) != 0 {
		t.Fatalf("repeated phrase inside one source rejected: %v", failures)
	}
	if !strings.Contains(events[0].enText, "No. No. No.") && !strings.Contains(normText(events[0].enText), "No.No.No.") {
		t.Fatalf("repeated phrase lost from output: %q", events[0].enText)
	}
}

// --- B08: shared reservations across policies -----------------------------

// B08: an on-screen sign really overlapping a block that must partition keeps
// the reservation, so the partition does not steal the free side.
func TestB08_MigrationAndPartitionRealOverlapReservation(t *testing.T) {
	oldHook := fontWidthMeasurerOverride
	// Too wide to merge: forces the partition path for the ordinary 2+2 block.
	fontWidthMeasurerOverride = func(string, string, float64, int, float64, bool) (float64, bool) {
		return 5000, true
	}
	defer func() { fontWidthMeasurerOverride = oldHook }()

	cfg := fixtureConfig()
	cfg.ZhChars, cfg.EnChars = 4, 6
	bundle := fixtureBundle(
		[]Cue{
			{ID: 1, Start: 1000, End: 4000, Display: "Sign", Plain: "Sign"},
			{ID: 2, Start: 3000, End: 4200, Display: "Alpha. Bravo.", Plain: "Alpha.\nBravo."},
		},
		[]Cue{
			{ID: 1, Start: 1000, End: 4000, Display: "标牌", Plain: "标牌"},
			{ID: 2, Start: 3000, End: 4200, Display: "甲乙丙 丁戊己", Plain: "甲乙丙\n丁戊己"},
		},
	)
	mapping := fixtureMapping(bundle, []Group{
		{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindOnScreenText},
		{ENIDs: []int{2}, ZHIDs: []int{2}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
	})
	_, events, err := renderWithAudits(bundle, mapping, cfg, fixtureStyle(), 1920, 1080, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("migration must reserve the top against an overlapping partition: got %d events", len(events))
	}
	for _, e := range events {
		if e.group == 1 && (len(sourceLines(e.zhText)) != 2 || len(sourceLines(e.enText)) != 2 || e.alignment >= 7) {
			t.Fatalf("block whose destination was already reserved must stay whole at the bottom: %+v", e)
		}
		if e.group == 0 && e.alignment < 7 {
			t.Fatalf("sign must actually have moved to the free side: %+v", e)
		}
	}
}

// B08b: no two emitted events may share a side at overlapping times, including
// events created by different policies.
func TestB08_NoSameSideDoubleBooking(t *testing.T) {
	cfg := renderTestConfig()
	cfg.ZhChars = 4
	cfg.EnChars = 10
	bundle := makeBundle(
		[]Cue{
			{ID: 1, Start: 1000, End: 2000, Display: "Sign text"},
			{ID: 2, Start: 2000, End: 3200, Display: "First english line. Second english line.",
				Plain: "First english line.\nSecond english line."},
		},
		[]Cue{
			{ID: 1, Start: 1000, End: 2000, Display: "文字"},
			{ID: 2, Start: 2000, End: 3200, Display: "第一段中文 第二段中文", Plain: "第一段中文\n第二段中文"},
		},
	)
	mapping := makeMapping(bundle, []Group{
		{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindOnScreenText},
		{ENIDs: []int{2}, ZHIDs: []int{2}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
	})
	oldHook := fontWidthMeasurerOverride
	defer func() { fontWidthMeasurerOverride = oldHook }()
	fontWidthMeasurerOverride = func(string, string, float64, int, float64, bool) (float64, bool) { return 2000, true }

	out, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	type occ struct {
		side int
		s, e int
	}
	var occs []occ
	for _, d := range parseDialogues(t, string(out)) {
		side := 0
		if strings.HasPrefix(d.style, "Top") {
			side = 1
		}
		occs = append(occs, occ{side: side, s: d.start, e: d.end})
	}
	for i := range occs {
		for j := i + 1; j < len(occs); j++ {
			if occs[i].side != occs[j].side {
				continue
			}
			if max(occs[i].s, occs[j].s) < min(occs[i].e, occs[j].e) {
				t.Fatalf("same side double-booked: %+v and %+v", occs[i], occs[j])
			}
		}
	}
}

// --- B09: explicit source pos/an must not pin relocated output ------------

// B09: a relocated cue must carry the new anchor, not its old \pos.
func TestB09_RelocatedCueDoesNotKeepSourcePosition(t *testing.T) {
	cfg := renderTestConfig()
	pos := [2]float64{960, 1000}
	bundle := makeBundle(
		[]Cue{
			{ID: 1, Start: 1000, End: 2000, Display: "Sign text", Position: &pos, Alignment: 2},
			{ID: 2, Start: 1500, End: 3000, Display: "Speech text"},
		},
		[]Cue{
			{ID: 1, Start: 1000, End: 2000, Display: "文字", Position: &pos, Alignment: 2},
			{ID: 2, Start: 1500, End: 3000, Display: "台词"},
		},
	)
	mapping := makeMapping(bundle, []Group{
		{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindOnScreenText},
		{ENIDs: []int{2}, ZHIDs: []int{2}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
	})
	out, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range parseDialogues(t, string(out)) {
		if !strings.Contains(d.text, "文字") {
			continue
		}
		if !strings.HasPrefix(d.style, "Top") {
			t.Fatalf("sign should have moved to the top style, got %q", d.style)
		}
		if d.hasPos {
			t.Fatalf("relocated cue kept an explicit \\pos: %q", d.text)
		}
	}
}

// --- B10/B11/B12: source line counting and no fabrication -----------------

// B10: one-sided group keeps its one side, fabricates nothing.
func TestB10_OneSidedGroupKeepsSingleSide(t *testing.T) {
	cfg := fixtureConfig()
	bundle := fixtureBundle(
		[]Cue{{ID: 1, Start: 1000, End: 3000, Display: "English only."}},
		[]Cue{},
	)
	mapping := fixtureMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{}, Layer: LayerBottom, Origin: OriginModel}})
	out, events, err := renderWithAudits(bundle, mapping, cfg, fixtureStyle(), 1920, 1080, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("expected exactly one event, got %d", len(events))
	}
	if events[0].zhText != "" {
		t.Fatalf("fabricated Chinese text: %q", events[0].zhText)
	}
	if !strings.Contains(string(out), "English only.") {
		t.Fatal("lost the visible source text")
	}
}

// B11: blank lines / format-only lines do not create fake extra rows.
func TestB11_BlankAndFormatOnlyLinesDoNotFakeRows(t *testing.T) {
	cases := []struct {
		plain string
		want  int
	}{
		{"First english line.\nSecond english line.", 2},
		{"First english line.\n\nSecond english line.", 2},
		{"First english line.\r\nSecond english line.", 2},
		{"First english line.\r\n\r\nSecond english line.\r\n", 2},
		// ASS break markup is a single visible source line, not an extra row.
		{"First english line \\N Second english line.", 1},
		// Source `plain` is already tag-stripped upstream, so format-only content
		// arrives as whitespace and contributes no visible row. The end-to-end
		// skip of a format-only cue is covered by
		// TestRenderSkipsFormatOnlySourceEvents.
		{"\n\n", 0},
		{"   \n\t\n", 0},
	}
	for _, tc := range cases {
		if got := len(sourceLines(tc.plain)); got != tc.want {
			t.Fatalf("sourceLines(%q) = %d, want %d", tc.plain, got, tc.want)
		}
	}

}

// B12: three or more source lines keep all text; nothing is truncated to 2.
func TestB12_ThreeOrMoreSourceLinesKeepAllText(t *testing.T) {
	cfg := renderTestConfig()
	cfg.ZhChars = 40
	cfg.EnChars = 60
	plainZh := "第一行\n第二行\n第三行"
	plainEn := "First line.\nSecond line.\nThird line."
	bundle := makeBundle(
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: plainEn, Plain: plainEn}},
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: plainZh, Plain: plainZh}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue}})
	out, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	joined := normText(string(out))
	for _, want := range []string{"第一行", "第二行", "第三行", "Firstline.", "Secondline.", "Thirdline."} {
		if !strings.Contains(joined, want) {
			t.Fatalf("lost content %q from three-line source", want)
		}
	}
}

// --- B13/B14: measurement and safe-cut conservatism -----------------------

// B13: same font family, different sizes -> the two sides must not be confused.
func TestB13_SameFamilyDifferentSizesNotConfused(t *testing.T) {
	reviewWidthHook(t)
	cfg := fixtureConfig()
	cfg.EnChars = 6
	style := fixtureStyle()
	style.PrimaryFont = "Same Family"
	style.SecondaryFont = "Same Family"
	style.PrimarySize = 80
	style.SecondarySize = 40
	bundle := fixtureBundle(
		[]Cue{{ID: 1, Start: 1000, End: 2200, Display: "Long english here.", Plain: "Long english\nhere."}},
		[]Cue{{ID: 1, Start: 1000, End: 2200, Display: "保留中文", Plain: "保留中文"}},
	)
	mapping := fixtureMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue}})
	_, events, err := renderWithAudits(bundle, mapping, cfg, style, 1920, 1080, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range events {
		if !strings.Contains(e.zhText, "保留中文") {
			t.Fatalf("Chinese was overwritten when both sides share a font family: %+v", e)
		}
	}
}

// B13: the cap counts letters, digits and spaces in runes, never bytes;
// punctuation and symbols do not count.
func TestB13_CapCountsSpacesNotPunctuation(t *testing.T) {
	// 17 runes: the two dashes do not count, the four spaces do.
	if got := countedLength("- A long - B line"); got != 15 {
		t.Fatalf("countedLength = %d, want 15", got)
	}
	for _, tt := range []struct {
		cap    int
		merged bool
	}{{cap: 14, merged: false}, {cap: 15, merged: true}} {
		cfg := fixtureConfig()
		cfg.EnChars = tt.cap
		bundle := fixtureBundle(
			[]Cue{{ID: 1, Start: 1000, End: 2200, Display: "- A long - B line", Plain: "- A long\n- B line", MultiSpeaker: true}},
			[]Cue{{ID: 1, Start: 1000, End: 2200, Display: "答复", Plain: "答复"}},
		)
		mapping := fixtureMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue}})
		oldHook := fontWidthMeasurerOverride
		fontWidthMeasurerOverride = func(text, fontName string, fontSize float64, scaleX int, spacing float64, bold bool) (float64, bool) {
			return 100, true
		}
		_, events, err := renderWithAudits(bundle, mapping, cfg, fixtureStyle(), 1920, 1080, nil)
		fontWidthMeasurerOverride = oldHook
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range events {
			if merged := len(sourceLines(e.enText)) == 1; merged != tt.merged {
				t.Fatalf("cap %d: merged = %v, want %v: %q", tt.cap, merged, tt.merged, e.enText)
			}
		}
	}
}

// B14: measurement failure keeps the complete text at original size.
func TestB14_MeasurementFailureKeepsCompleteText(t *testing.T) {
	cfg := renderTestConfig()
	// Cap fits each original source line, so the only way to reach one line is a
	// width-driven merge. Without a measurement it needs the em upper bound to
	// fit, and 35 characters at size 52 exceed the 1760px usable width.
	cfg.ZhChars = 17
	cfg.EnChars = 20
	line1 := strings.Repeat("第一段中文", 3) + "第一"
	line2 := strings.Repeat("第二段中文", 3) + "第二"
	bundle := makeBundle(
		[]Cue{{ID: 1, Start: 1000, End: 2200, Display: "Short english.", Plain: "Short english."}},
		[]Cue{{ID: 1, Start: 1000, End: 2200, Display: line1 + " " + line2, Plain: line1 + "\n" + line2}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue}})
	oldHook := fontWidthMeasurerOverride
	defer func() { fontWidthMeasurerOverride = oldHook }()
	fontWidthMeasurerOverride = func(string, string, float64, int, float64, bool) (float64, bool) {
		return 0, false
	}
	out, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	joined := normText(string(out))
	if !strings.Contains(joined, "第一段中文") || !strings.Contains(joined, "第二段中文") {
		t.Fatal("measurement failure dropped source text")
	}
	// Evidence from the actual per-language lines: no width merge happened, so the
	// Chinese side must still occupy two lines and carry no merge separator.
	for _, d := range parseDialogues(t, string(out)) {
		zh := d.text
		if idx := strings.Index(zh, `{\rEng}`); idx >= 0 {
			zh = zh[:idx]
		}
		// ASS writes intra-event breaks as the literal two-character `\N`.
		lines := sourceLines(strings.ReplaceAll(zh, `\N`, "\n"))
		if len(lines) != 2 {
			t.Fatalf("expected the original two Chinese lines without width merge, got %q", zh)
		}
		for _, line := range lines {
			if strings.Contains(line, mergeSeparator) {
				t.Fatalf("merged without a reliable measurement: %q", line)
			}
		}
	}
}

// B14: wrapping never splits an English token.
func TestB14_WrapKeepsEnglishTokensIntact(t *testing.T) {
	long := "supercalifragilisticexpialidocious extraordinary antidisestablishmentarianism"
	for _, limit := range []int{20, 30, 60} {
		wrapped := wrapDisplay(long, "en", limit, false)
		// Every whitespace token of the input must survive verbatim.
		tokens := strings.Fields(long)
		for _, token := range tokens {
			if !strings.Contains(wrapped, token) {
				t.Fatalf("limit %d split or dropped token %q: %q", limit, token, wrapped)
			}
		}
	}
}

// B14: generated cut points never split a grapheme cluster.
func TestB14_UnsafeCutPointsAreRefused(t *testing.T) {
	cases := []struct {
		name string
		text string
		cap  int
	}{
		{"zwj_family_emoji", "\U0001F468\u200d\U0001F469\u200d\U0001F467\u200d\U0001F466 X", 4},
		{"combining_marks", "e\u0301e\u0301e\u0301 X", 3},
		{"variation_selector", "\u2764\ufe0f\u2764\ufe0f X", 2},
		{"flag_pair", "\U0001F1E8\U0001F1F3\U0001F1EF\U0001F1F5 X", 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for i := 1; i < len([]rune(tc.text)); i++ {
				if !isSafeCutIndex([]rune(tc.text), i) {
					continue
				}
				// A "safe" index must not separate a cluster in the fixture.
				left, right := []rune(tc.text)[i-1], []rune(tc.text)[i]
				if left == '\u200d' || right == '\u200d' || isGraphemeContinuation(right) {
					t.Fatalf("index %d accepted though it splits a cluster", i)
				}
			}
			// Wrapping must never emit a broken line.
			for _, line := range wrapLine(tc.text, "zh", tc.cap) {
				if strings.HasSuffix(line, "\u200d") || strings.HasPrefix(strings.TrimSpace(line), "\u200d") {
					t.Fatalf("wrapLine broke a ZWJ sequence: %q", line)
				}
			}
		})
	}
}

// --- B15/B16: word timing robustness and determinism ---------------------

// B15 reuse: TestWordAlignmentKeepsCompletedCuesOnServiceFailure,
// TestWordAlignmentCancellationAndDisabledPath,
// TestWordAlignmentRejectsInvalidResponseTimes and
// TestBadWordTimingFallsBackToProportion already cover partial success,
// invalid results and cancellation. Below adds the "wrong words" case.

// B15: results whose words do not match the cue are rejected.
func TestB15_WrongWordResultFallsBack(t *testing.T) {
	plain := "Tell me if it starts to weaken"
	lines := sourceLines("Tell me if it starts to weaken\nor get thready.")
	cuts := wordCuts(plain, lines, []WordTiming{
		{Text: "completely", StartMS: 0, EndMS: 100},
		{Text: "different", StartMS: 100, EndMS: 200},
	})
	if len(cuts) != 0 {
		t.Fatalf("wrong-word timing produced a cut: %v", cuts)
	}
}

// B16: rendering the same inputs twice is deterministic and mutates nothing.
func TestB16_RenderIsDeterministicAndReadOnly(t *testing.T) {
	bundle := fixtureBundle(
		[]Cue{
			{ID: 1, Start: 1000, End: 4000, Display: "First english half here.", Plain: "First english half here."},
			{ID: 2, Start: 4000, End: 7000, Display: "Second english half here.", Plain: "Second english half here."},
		},
		[]Cue{{ID: 1, Start: 1000, End: 7000, Display: "第一段中文 第二段中文", Plain: "第一段中文\n第二段中文"}},
	)
	mapping := fixtureMapping(bundle, []Group{{ENIDs: []int{1, 2}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel}})
	cfg := fixtureConfig()
	style := fixtureStyle()

	beforeEN := append([]Cue(nil), bundle.EN...)
	beforeZH := append([]Cue(nil), bundle.ZH...)
	beforeGroups := append([]Group(nil), mapping.Groups...)

	first, _, err := renderWithAudits(bundle, mapping, cfg, style, 1920, 1080, nil)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := renderWithAudits(bundle, mapping, cfg, style, 1920, 1080, nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("identical inputs produced different ASS output")
	}
	for i := range beforeEN {
		if beforeEN[i].Display != bundle.EN[i].Display || beforeEN[i].Start != bundle.EN[i].Start {
			t.Fatalf("render mutated source bundle EN[%d]", i)
		}
	}
	for i := range beforeZH {
		if beforeZH[i].Display != bundle.ZH[i].Display || beforeZH[i].Start != bundle.ZH[i].Start {
			t.Fatalf("render mutated source bundle ZH[%d]", i)
		}
	}
	for i := range beforeGroups {
		if intsKey(beforeGroups[i].ENIDs) != intsKey(mapping.Groups[i].ENIDs) {
			t.Fatalf("render mutated mapping group %d", i)
		}
	}
}

// helper kept local: config import is used by B01/B13 fixtures.
var _ = config.LLMConfig{}
