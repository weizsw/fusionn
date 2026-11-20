package subtitlellm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/fusionn/internal/config"
)

// renderPlanEvents renders a bundle with the given mapping and returns the
// emitted events and the screen-plan audit.
func renderPlanEvents(t *testing.T, bundle Bundle, mapping Mapping, cfg config.LLMConfig) ([]renderedEvent, *ScreenPlanAudit) {
	t.Helper()
	_, events, audit, err := renderPlanWithAudits(bundle, mapping, cfg, fixtureStyle(), 1920, 1080, nil)
	if err != nil {
		t.Fatal(err)
	}
	return events, audit
}

// eventsForEnglish returns the events that reference one English source cue.
func eventsForEnglish(events []renderedEvent, id int) []renderedEvent {
	var result []renderedEvent
	for _, e := range events {
		for _, got := range e.enIDs {
			if got == id {
				result = append(result, e)
			}
		}
	}
	return result
}

// --- A. 100ms gap closing --------------------------------------------------

func TestGapClosingMovesOnlyTheEarlierEnd(t *testing.T) {
	for _, tc := range []struct {
		name       string
		firstEnd   int
		secondFrom int
		wantEnd    int
	}{
		{"90ms_gap_closed", 5000, 5090, 5090},
		{"100ms_gap_kept", 5000, 5100, 5000},
		{"0ms_gap_untouched", 5000, 5000, 5000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Both screens are long enough that rule C does not apply, so the
			// only end that can move is the one rule A closes.
			bundle := fixtureBundle(
				[]Cue{
					{ID: 1, Start: 1000, End: tc.firstEnd, OriginalStart: 1000, OriginalEnd: tc.firstEnd, Plain: "First line.", Display: "First line.", Alignment: 2},
					{ID: 2, Start: tc.secondFrom, End: 9000, OriginalStart: tc.secondFrom, OriginalEnd: 9000, Plain: "Second line.", Display: "Second line.", Alignment: 2},
				},
				[]Cue{
					{ID: 1, Start: 1000, End: tc.firstEnd, OriginalStart: 1000, OriginalEnd: tc.firstEnd, Plain: "第一句", Display: "第一句", Alignment: 2},
					{ID: 2, Start: tc.secondFrom, End: 9000, OriginalStart: tc.secondFrom, OriginalEnd: 9000, Plain: "第二句", Display: "第二句", Alignment: 2},
				},
			)
			mapping := fixtureMapping(bundle, []Group{
				{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel},
				{ENIDs: []int{2}, ZHIDs: []int{2}, Layer: LayerBottom, Origin: OriginModel},
			})
			events, _ := renderPlanEvents(t, bundle, mapping, fixtureConfig())
			first := eventsForEnglish(events, 1)
			second := eventsForEnglish(events, 2)
			if len(first) != 1 || len(second) != 1 {
				t.Fatalf("want one event per cue, got %d and %d", len(first), len(second))
			}
			if first[0].assStart != 1000 || second[0].assStart != roundCentiseconds(tc.secondFrom) {
				t.Fatalf("a start moved: first %d, second %d", first[0].assStart, second[0].assStart)
			}
			if first[0].assEnd != tc.wantEnd {
				t.Fatalf("first end = %d, want %d", first[0].assEnd, tc.wantEnd)
			}
		})
	}
}

// --- B. A one-sided cue is never split ------------------------------------

func TestOneSidedCueKeepsWholeTextAndSourceLines(t *testing.T) {
	// Over zh_chars, so the source line break is the one the source has.
	const symbol = "（上海证券交易所\n516.83 -13.05 (-0.37%) - 6个月）"
	bundle := fixtureBundle(
		[]Cue{},
		[]Cue{{ID: 1, Start: 1000, End: 3500, OriginalStart: 1000, OriginalEnd: 3500, Plain: symbol, Display: symbol, Alignment: 2}},
	)
	mapping := fixtureMapping(bundle, []Group{
		{ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginRestored},
	})
	events, _ := renderPlanEvents(t, bundle, mapping, fixtureConfig())
	for _, e := range events {
		if strings.Contains(e.zhText, "上海证券交易所") && !strings.Contains(e.zhText, "6个月") {
			t.Fatalf("the one-sided sign text was split into %q", e.zhText)
		}
	}
}

// --- C. Short screens grow only their end --------------------------------

func TestShortScreenGrowsToTheFloorWithoutMovingStarts(t *testing.T) {
	bundle := fixtureBundle(
		[]Cue{
			{ID: 1, Start: 1000, End: 1800, OriginalStart: 1000, OriginalEnd: 1800, Plain: "Short.", Display: "Short.", Alignment: 2},
			{ID: 2, Start: 4000, End: 6000, OriginalStart: 4000, OriginalEnd: 6000, Plain: "Next.", Display: "Next.", Alignment: 2},
		},
		[]Cue{
			{ID: 1, Start: 1000, End: 1800, OriginalStart: 1000, OriginalEnd: 1800, Plain: "短句", Display: "短句", Alignment: 2},
			{ID: 2, Start: 4000, End: 6000, OriginalStart: 4000, OriginalEnd: 6000, Plain: "下一句", Display: "下一句", Alignment: 2},
		},
	)
	mapping := fixtureMapping(bundle, []Group{
		{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel},
		{ENIDs: []int{2}, ZHIDs: []int{2}, Layer: LayerBottom, Origin: OriginModel},
	})
	events, _ := renderPlanEvents(t, bundle, mapping, fixtureConfig())
	first := eventsForEnglish(events, 1)[0]
	if first.assStart != 1000 || first.assEnd != 2200 {
		t.Fatalf("short screen = %d..%d, want 1000..2200", first.assStart, first.assEnd)
	}
	if second := eventsForEnglish(events, 2)[0]; second.assStart != 4000 || second.assEnd != 6000 {
		t.Fatalf("a long screen changed: %d..%d", second.assStart, second.assEnd)
	}
}

func TestShortScreenStopsAtTheNextStartOfItsRegion(t *testing.T) {
	bundle := fixtureBundle(
		[]Cue{
			{ID: 1, Start: 1000, End: 1500, OriginalStart: 1000, OriginalEnd: 1500, Plain: "Short.", Display: "Short.", Alignment: 2},
			{ID: 2, Start: 2000, End: 4000, OriginalStart: 2000, OriginalEnd: 4000, Plain: "Next.", Display: "Next.", Alignment: 2},
		},
		[]Cue{
			{ID: 1, Start: 1000, End: 1500, OriginalStart: 1000, OriginalEnd: 1500, Plain: "短句", Display: "短句", Alignment: 2},
			{ID: 2, Start: 2000, End: 4000, OriginalStart: 2000, OriginalEnd: 4000, Plain: "下一句", Display: "下一句", Alignment: 2},
		},
	)
	mapping := fixtureMapping(bundle, []Group{
		{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel},
		{ENIDs: []int{2}, ZHIDs: []int{2}, Layer: LayerBottom, Origin: OriginModel},
	})
	events, _ := renderPlanEvents(t, bundle, mapping, fixtureConfig())
	first := eventsForEnglish(events, 1)[0]
	// Rule C cannot pass the next start, so rule A closes the remaining blank.
	if first.assStart != 1000 || first.assEnd != 2000 {
		t.Fatalf("short screen = %d..%d, want 1000..2000", first.assStart, first.assEnd)
	}
	if second := eventsForEnglish(events, 2)[0]; second.assStart != 2000 {
		t.Fatalf("the next start moved to %d", second.assStart)
	}
}

func TestShortScreenExtensionIgnoresTheOtherRegion(t *testing.T) {
	bundle := fixtureBundle(
		[]Cue{{ID: 1, Start: 1000, End: 1500, OriginalStart: 1000, OriginalEnd: 1500, Plain: "Short.", Display: "Short.", Alignment: 2}},
		[]Cue{
			{ID: 1, Start: 1000, End: 1500, OriginalStart: 1000, OriginalEnd: 1500, Plain: "短句", Display: "短句", Alignment: 2},
			{ID: 2, Start: 1700, End: 3000, OriginalStart: 1700, OriginalEnd: 3000, Plain: "顶部", Display: "顶部", Alignment: 2, Top: true},
		},
	)
	mapping := fixtureMapping(bundle, []Group{
		{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel},
		{ZHIDs: []int{2}, Layer: LayerTop, Origin: OriginModel},
	})
	events, _ := renderPlanEvents(t, bundle, mapping, fixtureConfig())
	if bottom := eventsForEnglish(events, 1)[0]; bottom.assStart != 1000 || bottom.assEnd != 2200 {
		t.Fatalf("bottom screen = %d..%d, want 1000..2200", bottom.assStart, bottom.assEnd)
	}
}

// --- D. Dialogue merge by measured width ---------------------------------

// dialogueMergeBundle is one native two-speaker Chinese cue over two English
// cues, so the merged side exceeds the character limit but may still fit.
func dialogueMergeBundle() Bundle {
	return fixtureBundle(
		[]Cue{
			{ID: 1, Start: 1000, End: 2500, OriginalStart: 1000, OriginalEnd: 2500, Plain: "I am sorry, I do not quite.", Display: "I am sorry, I do not quite.", Alignment: 2},
			{ID: 2, Start: 2500, End: 4000, OriginalStart: 2500, OriginalEnd: 4000, Plain: "I have proof.", Display: "I have proof.", Alignment: 2},
		},
		[]Cue{{ID: 1, Start: 1000, End: 4000, OriginalStart: 1000, OriginalEnd: 4000,
			Plain:   "- 抱歉 我不太明白你的意思\n- 我有证据表明你说了谎",
			Display: "- 抱歉 我不太明白你的意思   - 我有证据表明你说了谎", MultiSpeaker: true, Alignment: 2}},
	)
}

func TestDialogueMergeUsesMeasuredWidthPastTheCharacterLimit(t *testing.T) {
	bundle := dialogueMergeBundle()
	mapping := fixtureMapping(bundle, []Group{
		{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
		{ENIDs: []int{2}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
	})
	merged := false
	old := fontWidthMeasurerOverride
	fontWidthMeasurerOverride = func(text, fontName string, fontSize float64, scaleX int, spacing float64, bold bool) (float64, bool) {
		return 100, true
	}
	t.Cleanup(func() { fontWidthMeasurerOverride = old })
	for _, e := range eventsForAnyLang(t, bundle, mapping) {
		if strings.Contains(e.zhText, "抱歉") && strings.Contains(e.zhText, "证据") && sourceLines(e.zhText) != nil && len(sourceLines(e.zhText)) == 1 {
			merged = true
		}
	}
	if !merged {
		t.Fatal("a dialogue line that measured width proves fits one line was not merged")
	}
}

func TestDialogueMergeKeepsTheLimitWhenWidthIsUnreliable(t *testing.T) {
	bundle := dialogueMergeBundle()
	mapping := fixtureMapping(bundle, []Group{
		{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
		{ENIDs: []int{2}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
	})
	old := fontWidthMeasurerOverride
	fontWidthMeasurerOverride = func(text, fontName string, fontSize float64, scaleX int, spacing float64, bold bool) (float64, bool) {
		return 0, false
	}
	t.Cleanup(func() { fontWidthMeasurerOverride = old })
	for _, e := range eventsForAnyLang(t, bundle, mapping) {
		if strings.Contains(e.zhText, "抱歉") && strings.Contains(e.zhText, "证据") && len(sourceLines(e.zhText)) == 1 {
			t.Fatalf("an unreliable width measurement merged dialogue past its limit: %q", e.zhText)
		}
	}
}

func eventsForAnyLang(t *testing.T, bundle Bundle, mapping Mapping) []renderedEvent {
	t.Helper()
	events, _ := renderPlanEvents(t, bundle, mapping, fixtureConfig())
	return events
}

// --- E. Screen plan request and storage ---------------------------------

// planningServer answers alignment and screen-plan requests for an engine run.
func planningServer(t *testing.T, plans string, planRequests *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload requestPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode: %v", err)
			w.WriteHeader(500)
			return
		}
		content := `{"alignments":[{"en_ids":[1],"zh_ids":[1],"layer":"bottom","kind":"dialogue"},{"en_ids":[2],"zh_ids":[1],"layer":"bottom","kind":"dialogue"}]}`
		if strings.Contains(payload.Messages[1].Content, `"groups"`) {
			if planRequests != nil {
				planRequests.Add(1)
			}
			content = plans
		}
		if err := json.NewEncoder(w).Encode(map[string]any{
			"model":    "mock",
			"choices":  []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": content}}},
			"usage":    map[string]any{"prompt_tokens": 1, "completion_tokens": 1},
			"id":       "mock",
			"object":   "chat.completion",
			"created":  1,
			"model_id": "mock",
		}); err != nil {
			t.Errorf("encode: %v", err)
		}
	}))
}

// alignWithPlans runs Align against a mock that answers the plan request with
// the given content and returns the mapping plus the number of plan requests.
func alignWithPlans(t *testing.T, planContent string) (Mapping, Audit, int32) {
	t.Helper()
	var planRequests atomic.Int32
	server := planningServer(t, planContent, &planRequests)
	t.Cleanup(server.Close)
	bundle := dialogueMergeBundle()
	cfg := testConfig(t, server.URL+"/v1")
	cfg.WindowSeconds = 100
	cfg.OverlapSeconds = 0
	cfg.Retries = 0
	cfg.Workers = 1
	mapping, audit, err := NewEngine().Align(context.Background(), bundle, cfg)
	if err != nil {
		t.Fatal(err)
	}
	return mapping, audit, planRequests.Load()
}

func TestAlignStoresScreenPlansAndCountsThem(t *testing.T) {
	mapping, audit, requests := alignWithPlans(t, `{"plans":[]}`)
	if requests != 1 {
		t.Fatalf("plan requests = %d, want 1", requests)
	}
	if audit.ScreenPlans == nil || audit.ScreenPlans.Candidates != 1 {
		t.Fatalf("candidates = %+v, want 1", audit.ScreenPlans)
	}
	if len(mapping.ScreenPlans) != 0 {
		t.Fatalf("an empty answer produced plans: %+v", mapping.ScreenPlans)
	}
}

func TestAlignRequestFailureLeavesGroupsWithoutPlans(t *testing.T) {
	var planRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload requestPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			w.WriteHeader(500)
			return
		}
		if strings.Contains(payload.Messages[1].Content, `"groups"`) {
			planRequests.Add(1)
			w.WriteHeader(500)
			return
		}
		content := `{"alignments":[{"en_ids":[1],"zh_ids":[1],"layer":"bottom","kind":"dialogue"},{"en_ids":[2],"zh_ids":[1],"layer":"bottom","kind":"dialogue"}]}`
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model":   "mock",
			"choices": []any{map[string]any{"finish_reason": "stop", "message": map[string]any{"content": content}}},
		})
	}))
	t.Cleanup(server.Close)
	bundle := dialogueMergeBundle()
	cfg := testConfig(t, server.URL+"/v1")
	cfg.WindowSeconds = 100
	cfg.OverlapSeconds = 0
	cfg.Retries = 0
	cfg.Workers = 1
	mapping, audit, err := NewEngine().Align(context.Background(), bundle, cfg)
	if err != nil {
		t.Fatalf("a failed plan request failed the merge: %v", err)
	}
	if planRequests.Load() != 1 {
		t.Fatalf("plan requests = %d, want 1", planRequests.Load())
	}
	if len(mapping.Groups) != 1 || len(mapping.Groups[0].ENIDs) != 2 {
		t.Fatalf("pairing changed: %+v", mapping.Groups)
	}
	if len(mapping.ScreenPlans) != 0 {
		t.Fatalf("a failed request produced plans: %+v", mapping.ScreenPlans)
	}
	if audit.ScreenPlans == nil || audit.ScreenPlans.Rejected["request_failed"] != 1 {
		t.Fatalf("failure not recorded: %+v", audit.ScreenPlans)
	}
}

func TestMappingFingerprintWithoutPlansIsUnchanged(t *testing.T) {
	bundle := dialogueMergeBundle()
	groups := []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel}}
	plain := Mapping{Version: Version, SourceFingerprint: bundle.Fingerprint, Groups: groups}
	withEmpty := plain
	if _, err := mappingDigest(plain); err != nil {
		t.Fatal(err)
	}
	withEmpty.ScreenPlans = nil
	first, err := mappingDigest(plain)
	if err != nil {
		t.Fatal(err)
	}
	second, err := mappingDigest(withEmpty)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("a mapping without plans changed its fingerprint: %q vs %q", first, second)
	}
	planned := plain
	planned.ScreenPlans = []ScreenPlan{{ENIDs: []int{1}, ZHIDs: []int{1}, Screens: []ScreenText{{EN: "a", ZH: "b"}}}}
	third, err := mappingDigest(planned)
	if err != nil {
		t.Fatal(err)
	}
	if third == first {
		t.Fatal("a mapping with plans kept the plan-free fingerprint")
	}
}

// --- F1: rule C runs before rule A ---------------------------------------

// TestShortScreenGrowsToTheFloorThenClosesTheGap covers the C-then-A order: C
// stops at start+1200, and A then closes the blank that is still shorter than
// 100ms to the next screen.
func TestShortScreenGrowsToTheFloorThenClosesTheGap(t *testing.T) {
	bundle := fixtureBundle(
		[]Cue{
			{ID: 1, Start: 1000, End: 1500, OriginalStart: 1000, OriginalEnd: 1500, Plain: "Short.", Display: "Short.", Alignment: 2},
			{ID: 2, Start: 2250, End: 4000, OriginalStart: 2250, OriginalEnd: 4000, Plain: "Next.", Display: "Next.", Alignment: 2},
		},
		[]Cue{
			{ID: 1, Start: 1000, End: 1500, OriginalStart: 1000, OriginalEnd: 1500, Plain: "短句", Display: "短句", Alignment: 2},
			{ID: 2, Start: 2250, End: 4000, OriginalStart: 2250, OriginalEnd: 4000, Plain: "下一句", Display: "下一句", Alignment: 2},
		},
	)
	mapping := fixtureMapping(bundle, []Group{
		{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel},
		{ENIDs: []int{2}, ZHIDs: []int{2}, Layer: LayerBottom, Origin: OriginModel},
	})
	events, _ := renderPlanEvents(t, bundle, mapping, fixtureConfig())
	first := eventsForEnglish(events, 1)[0]
	// C takes 1000 to 2200; the remaining 50ms blank to 2250 is then closed by A.
	if first.assStart != 1000 || first.assEnd != 2250 {
		t.Fatalf("first screen = %d..%d, want 1000..2250 (floor, then the gap closed)", first.assStart, first.assEnd)
	}
	if second := eventsForEnglish(events, 2)[0]; second.assStart != 2250 {
		t.Fatalf("the next start moved to %d", second.assStart)
	}
}

// --- F6: word times only for real in-cue cuts ----------------------------

// TestWordAlignmentOnlyForCuesCutInside checks that a plan pointing at every
// English cue of a group does not make all of them word-alignment candidates:
// only a cue a piece boundary falls strictly inside is added.
func TestWordAlignmentOnlyForCuesCutInside(t *testing.T) {
	en := []Cue{
		{ID: 1, Start: 1000, End: 4000, Plain: "Open the door now please", Display: "Open the door now please", Alignment: 2},
		{ID: 2, Start: 4000, End: 6000, Plain: "Then close it.", Display: "Then close it.", Alignment: 2},
	}
	zh := []Cue{
		{ID: 1, Start: 1000, End: 3000, Plain: "开门", Display: "开门", Alignment: 2},
		{ID: 2, Start: 3000, End: 6000, Plain: "关上", Display: "关上", Alignment: 2},
	}
	bundle := fixtureBundle(en, zh)
	cfg := fixtureConfig()
	// Without plans, neither cue is long enough to be an alignment candidate.
	if cues := WordAlignmentCuesFor(bundle, cfg, nil); len(cues) != 0 {
		t.Fatalf("a plan-free bundle produced candidates: %+v", cues)
	}
	// A cut strictly inside cue 1 makes only cue 1 a candidate.
	cut := []ScreenPlan{{ENIDs: []int{1, 2}, ZHIDs: []int{1, 2}, Screens: []ScreenText{
		{EN: "Open the door", ZH: "开门"},
		{EN: "now please", ZH: "关上"},
		{EN: "Then close it.", ZH: ""},
	}}}
	cues := WordAlignmentCuesFor(bundle, cfg, cut)
	if len(cues) != 1 || cues[0].ID != 1 {
		t.Fatalf("candidates = %+v, want only cue 1", cues)
	}
	// A plan that only uses cue boundaries adds none.
	whole := []ScreenPlan{{ENIDs: []int{1, 2}, ZHIDs: []int{1, 2}, Screens: []ScreenText{
		{EN: "Open the door now please", ZH: "开门"},
		{EN: "Then close it.", ZH: "关上"},
	}}}
	if cues := WordAlignmentCuesFor(bundle, cfg, whole); len(cues) != 0 {
		t.Fatalf("a boundary-only plan produced candidates: %+v", cues)
	}
	// A plan whose pieces do not match its cue text is skipped, not trusted.
	broken := []ScreenPlan{{ENIDs: []int{1}, ZHIDs: []int{1}, Screens: []ScreenText{
		{EN: "Open the door", ZH: "开门"},
		{EN: "and close it again", ZH: ""},
	}}}
	if cues := WordAlignmentCuesFor(bundle, cfg, broken); len(cues) != 0 {
		t.Fatalf("a mismatched plan produced candidates: %+v", cues)
	}
}

// --- B3: one-sided cues, one line when it fits --------------------------

// TestOneSidedCueIsOneLineWhenItFits covers rule B: a one-sided cue is never
// split in time. Text within the language limit goes on one line; text over it
// keeps exactly its source line breaks and gains no wrap.
func TestOneSidedCueIsOneLineWhenItFits(t *testing.T) {
	for _, tc := range []struct {
		name    string
		plain   string
		display string
		limit   int
		want    string
	}{
		// 4 counted characters, under the limit: one line.
		{"short_lines", "甲乙\n丙丁", "甲乙 丙丁", 20, "甲乙 丙丁"},
		// 14 counted characters, over the limit: the source lines stay as they are.
		{"long_line", "甲乙丙丁戊己庚辛壬癸\n子丑寅卯", "甲乙丙丁戊己庚辛壬癸 子丑寅卯", 5, "甲乙丙丁戊己庚辛壬癸\n子丑寅卯"},
		// A dialogue-flavored one-sided line keeps its marks on one line.
		{"speaker_marks", "- 走吧\n- 等着", "- 走吧 - 等着", 20, "- 走吧 - 等着"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bundle := fixtureBundle(nil, []Cue{{ID: 1, Start: 1000, End: 6000,
				OriginalStart: 1000, OriginalEnd: 6000, Plain: tc.plain, Display: tc.display, Alignment: 2}})
			mapping := fixtureMapping(bundle, []Group{{ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginRestored}})
			cfg := fixtureConfig()
			cfg.ZhChars = tc.limit
			events, _ := renderPlanEvents(t, bundle, mapping, cfg)
			if len(events) != 1 {
				t.Fatalf("a one-sided cue produced %d events", len(events))
			}
			if events[0].zhText != tc.want {
				t.Fatalf("want %q got %q", tc.want, events[0].zhText)
			}
		})
	}
}

// --- R3/R4: word alignment selection ------------------------------------

// TestWordAlignmentForPlannedMultiSpeakerCue covers R3: a plan that cuts inside
// a native two-speaker English cue still asks for word times, because the cut
// time is a word boundary.
func TestWordAlignmentForPlannedMultiSpeakerCue(t *testing.T) {
	speaker := Cue{ID: 1, Start: 1000, End: 7000, Plain: "- Go now.\n- Wait here.",
		Display: "- Go now.   - Wait here.", MultiSpeaker: true, Alignment: 2}
	speakerZH := Cue{ID: 1, Start: 1000, End: 7000, Plain: "- 走吧\n- 等着",
		Display: "- 走吧   - 等着", MultiSpeaker: true, Alignment: 2}
	bundle := fixtureBundle([]Cue{speaker}, []Cue{speakerZH})
	plan := ScreenPlan{ENIDs: []int{1}, ZHIDs: []int{1}, Screens: []ScreenText{
		{EN: "- Go now.", ZH: "- 走吧"},
		{EN: "- Wait here.", ZH: "- 等着"},
	}}
	en := map[int]Cue{1: speaker}
	zh := map[int]Cue{1: speakerZH}
	if _, err := resolveScreenPlan(plan, en, zh, nil, planLimits()); err != nil {
		t.Fatalf("a valid plan was rejected: %v", err)
	}
	if cues := WordAlignmentCuesFor(bundle, fixtureConfig(), []ScreenPlan{plan}); len(cues) != 1 || cues[0].ID != 1 {
		t.Fatalf("a planned two-speaker cue was not selected: %+v", cues)
	}
	// The 30s cap still applies.
	long := speaker
	long.End = 40000
	slow := fixtureBundle([]Cue{long}, []Cue{speakerZH})
	if cues := WordAlignmentCuesFor(slow, fixtureConfig(), []ScreenPlan{plan}); len(cues) != 0 {
		t.Fatalf("a cue past the 30s cap was selected: %+v", cues)
	}
}

// TestWordAlignmentIgnoresWholeCueJoins covers R4: a screen that joins whole
// cues is not an in-cue cut, so it adds no word-alignment work.
func TestWordAlignmentIgnoresWholeCueJoins(t *testing.T) {
	en, zh := planCues()
	bundle := fixtureBundle([]Cue{en[1], en[2]}, []Cue{zh[1], zh[2]})
	joined := ScreenPlan{ENIDs: []int{1, 2}, ZHIDs: []int{1, 2}, Screens: []ScreenText{
		{EN: en[1].Display + " " + en[2].Display, ZH: zh[1].Display + " " + zh[2].Display},
	}}
	if cues := WordAlignmentCuesFor(bundle, fixtureConfig(), []ScreenPlan{joined}); len(cues) != 0 {
		t.Fatalf("a whole-cue join requested word alignment for %+v", cues)
	}
	// A real interior cut still selects exactly that cue.
	cut := ScreenPlan{ENIDs: []int{1, 2}, ZHIDs: []int{1, 2}, Screens: []ScreenText{
		{EN: "I never spoke to Mo", ZH: zh[1].Display},
		{EN: "about the loss", ZH: zh[2].Display},
		{EN: "of his father and his sisters.", ZH: ""},
	}}
	cues := WordAlignmentCuesFor(bundle, fixtureConfig(), []ScreenPlan{cut})
	if len(cues) != 1 || cues[0].ID != 1 {
		t.Fatalf("candidates = %+v, want only cue 1", cues)
	}
}
