package subtitlellm

import (
	"strings"
	"testing"
)

// planCues builds the source cues a plan-validation test needs.
func planCues() (map[int]Cue, map[int]Cue) {
	en := map[int]Cue{
		1: {ID: 1, Start: 1000, End: 4000, Plain: "I never spoke to Mo about the loss", Display: "I never spoke to Mo about the loss", Alignment: 2},
		2: {ID: 2, Start: 4000, End: 6000, Plain: "of his father and his sisters.", Display: "of his father and his sisters.", Alignment: 2},
	}
	zh := map[int]Cue{
		1: {ID: 1, Start: 1000, End: 4000, Plain: "我从没跟莫谈过他父亲", Display: "我从没跟莫谈过他父亲", Alignment: 2},
		2: {ID: 2, Start: 4000, End: 6000, Plain: "和他姐妹的死", Display: "和他姐妹的死", Alignment: 2},
	}
	return en, zh
}

func planLimits() planGeometry {
	return planGeometry{enChars: 65, zhChars: 20}
}

// planError returns the validation error text for one plan, or "".
func planError(t *testing.T, en, zh map[int]Cue, plan ScreenPlan, limits planGeometry) string {
	t.Helper()
	if _, err := resolveScreenPlan(plan, en, zh, nil, limits); err != nil {
		return err.Error()
	}
	return ""
}

func TestScreenPlanValidatesAndTimesWholeCues(t *testing.T) {
	en, zh := planCues()
	plan := ScreenPlan{
		ENIDs: []int{1, 2}, ZHIDs: []int{1, 2},
		Screens: []ScreenText{
			{EN: "I never spoke to Mo about the loss", ZH: "我从没跟莫谈过他父亲"},
			{EN: "of his father and his sisters.", ZH: "和他姐妹的死"},
		},
	}
	screens, err := resolveScreenPlan(plan, en, zh, nil, planLimits())
	if err != nil {
		t.Fatal(err)
	}
	if len(screens) != 2 {
		t.Fatalf("screens = %d, want 2", len(screens))
	}
	if screens[0].start != 1000 || screens[0].end != 4000 {
		t.Fatalf("screen 0 = %d..%d, want 1000..4000", screens[0].start, screens[0].end)
	}
	if screens[1].start != 4000 || screens[1].end != 6000 {
		t.Fatalf("screen 1 = %d..%d, want 4000..6000", screens[1].start, screens[1].end)
	}
}

// TestScreenPlanRejectsChangedText covers validation rule 1: every character
// must come from the source, in order.
func TestScreenPlanRejectsChangedText(t *testing.T) {
	en, zh := planCues()
	for _, tc := range []struct {
		name   string
		plan   ScreenPlan
		reason string
	}{
		{"added_word", ScreenPlan{ENIDs: []int{1, 2}, ZHIDs: []int{1, 2}, Screens: []ScreenText{
			{EN: "I never spoke to Mo about the awful loss", ZH: "我从没跟莫谈过他父亲"}, {EN: "of his father and his sisters.", ZH: "和他姐妹的死"}}},
			"does not continue the source text"},
		{"dropped_word", ScreenPlan{ENIDs: []int{1, 2}, ZHIDs: []int{1, 2}, Screens: []ScreenText{
			{EN: "I never spoke to Mo", ZH: "我从没跟莫谈过他父亲"}, {EN: "of his father and his sisters.", ZH: "和他姐妹的死"}}},
			"does not continue the source text"},
		{"chinese_changed", ScreenPlan{ENIDs: []int{1, 2}, ZHIDs: []int{1, 2}, Screens: []ScreenText{
			{EN: "I never spoke to Mo about the loss", ZH: "我没跟莫谈过他父亲"}, {EN: "of his father and his sisters.", ZH: "和他姐妹的死"}}},
			"does not continue the source text"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			message := planError(t, en, zh, tc.plan, planLimits())
			if !strings.Contains(message, tc.reason) {
				t.Fatalf("rejection = %q, want it to mention %q", message, tc.reason)
			}
		})
	}
}

// TestScreenPlanRejectsCutsInsideWords covers validation rule 2.
func TestScreenPlanRejectsCutsInsideWords(t *testing.T) {
	en, zh := planCues()
	// A cut between "lo" and "ss" is not a word boundary.
	plan := ScreenPlan{ENIDs: []int{1, 2}, ZHIDs: []int{1, 2}, Screens: []ScreenText{
		{EN: "I never spoke to Mo about the lo", ZH: "我从没跟莫谈过他父亲"},
		{EN: "ss of his father and his sisters.", ZH: "和他姐妹的死"},
	}}
	message := planError(t, en, zh, plan, planLimits())
	if !strings.Contains(message, "not on a word boundary") {
		t.Fatalf("rejection = %q", message)
	}
}

// TestScreenPlanRejectsEmptyScreen covers validation rule 3.
func TestScreenPlanRejectsEmptyScreen(t *testing.T) {
	en, zh := planCues()
	plan := ScreenPlan{ENIDs: []int{1, 2}, ZHIDs: []int{1, 2}, Screens: []ScreenText{
		{EN: "I never spoke to Mo about the loss of his father", ZH: ""},
		{EN: "", ZH: "我从没跟莫谈过他父亲 和他姐妹的死"},
	}}
	message := planError(t, en, zh, plan, planLimits())
	if message == "" {
		t.Fatal("a plan with an empty screen and a partial Chinese side passed")
	}
}

// TestScreenPlanAllowsChineseOnlyScreenOfWholeCues covers the allowed empty
// English side.
func TestScreenPlanAllowsChineseOnlyScreenOfWholeCues(t *testing.T) {
	en, zh := planCues()
	// The Chinese-only cue has its own window after the English, so nothing else
	// in the plan overlaps it.
	zh[2] = Cue{ID: 2, Start: 8000, End: 9000, Plain: "和他姐妹的死", Display: "和他姐妹的死", Alignment: 2}
	plan := ScreenPlan{ENIDs: []int{1, 2}, ZHIDs: []int{1, 2}, Screens: []ScreenText{
		{EN: "I never spoke to Mo about the loss", ZH: "我从没跟莫谈过他父亲"},
		{EN: "of his father and his sisters.", ZH: ""},
		{EN: "", ZH: "和他姐妹的死"},
	}}
	screens, err := resolveScreenPlan(plan, en, zh, nil, planLimits())
	if err != nil {
		t.Fatal(err)
	}
	if screens[2].start != 8000 || screens[2].end != 9000 {
		t.Fatalf("the Chinese-only screen did not use its source cue time: %+v", screens[2])
	}
	if len(screens[2].zhIDs) != 1 || screens[2].zhIDs[0] != 2 || screens[2].en != "" {
		t.Fatalf("the Chinese-only screen lost its cue: %+v", screens[2])
	}
}

// TestScreenPlanPairsInvertedOrderByPosition covers the inversion rule: the
// Chinese meaning crosses, but the pieces still pair one line to one line.
func TestScreenPlanPairsInvertedOrderByPosition(t *testing.T) {
	en, zh := planCues()
	plan := ScreenPlan{ENIDs: []int{1, 2}, ZHIDs: []int{1, 2}, Screens: []ScreenText{
		{EN: "I never spoke to Mo about the loss", ZH: "我从没跟莫谈过他父亲"},
		{EN: "of his father and his sisters.", ZH: "和他姐妹的死"},
	}}
	screens, err := resolveScreenPlan(plan, en, zh, nil, planLimits())
	if err != nil {
		t.Fatal(err)
	}
	if screens[0].zh != "我从没跟莫谈过他父亲" || screens[1].zh != "和他姐妹的死" {
		t.Fatalf("pieces were reordered: %+v", screens)
	}
	if screens[0].start >= screens[1].start {
		t.Fatalf("screens are not in order: %+v", screens)
	}
}

// TestScreenPlanEnglishJoinSpansItsCues covers the two-or-more-cue fallback,
// which the join rule keeps only for Chinese it cannot cut. The fixture's
// Chinese has a space, so the join is now rejected and the same cues pass as
// two screens; the unbreakable Chinese below still joins.
func TestScreenPlanEnglishJoinSpansItsCues(t *testing.T) {
	en, zh := planCues()
	cuttable := ScreenPlan{ENIDs: []int{1, 2}, ZHIDs: []int{1, 2}, Screens: []ScreenText{
		{EN: "I never spoke to Mo about the loss of his father and his sisters.", ZH: "我从没跟莫谈过他父亲 和他姐妹的死"},
	}}
	if message := planError(t, en, zh, cuttable, planLimits()); !strings.Contains(message, "joins English cues") {
		t.Fatalf("a join whose Chinese can be cut passed: %q", message)
	}
	// The same group with the Chinese cut at its space passes, one cue each.
	twoScreens := ScreenPlan{ENIDs: []int{1, 2}, ZHIDs: []int{1, 2}, Screens: []ScreenText{
		{EN: "I never spoke to Mo about the loss", ZH: "我从没跟莫谈过他父亲"},
		{EN: "of his father and his sisters.", ZH: "和他姐妹的死"},
	}}
	if _, err := resolveScreenPlan(twoScreens, en, zh, nil, planLimits()); err != nil {
		t.Fatalf("cutting the Chinese at its space was rejected: %v", err)
	}
	// One unbreakable Chinese cue cannot follow the English cues, so the join
	// is the only way to keep one line per language.
	joined := ScreenPlan{ENIDs: []int{1, 2}, ZHIDs: []int{1}, Screens: []ScreenText{
		{EN: "I never spoke to Mo about the loss of his father and his sisters.", ZH: "我从没跟莫谈过他父亲和他姐妹的死"},
	}}
	zhJoined := map[int]Cue{1: {ID: 1, Start: 1000, End: 6000,
		Plain: "我从没跟莫谈过他父亲和他姐妹的死", Display: "我从没跟莫谈过他父亲和他姐妹的死", Alignment: 2}}
	screens, err := resolveScreenPlan(joined, en, zhJoined, nil, planLimits())
	if err != nil {
		t.Fatalf("a join of unbreakable Chinese was rejected: %v", err)
	}
	if len(screens) != 1 || screens[0].start != 1000 || screens[0].end != 6000 {
		t.Fatalf("joined screen = %+v", screens)
	}
	if len(screens[0].enIDs) != 2 || len(screens[0].zhIDs) != 1 {
		t.Fatalf("joined screen lost its cues: %+v", screens[0])
	}
}

// TestScreenPlanSpeakerDashRules covers validation rule 7 in both languages: a
// piece that is not made of whole cues and opens a speaker turn drops its
// leading mark, unless the piece still holds another turn.
func TestScreenPlanSpeakerDashRules(t *testing.T) {
	en := map[int]Cue{
		1: {ID: 1, Start: 1000, End: 5000, Plain: "- Stay here - while I check the room.",
			Display: "- Stay here - while I check the room.", Alignment: 2},
		2: {ID: 2, Start: 5000, End: 8000, Plain: "Hey, I need to ID a body.",
			Display: "Hey, I need to ID a body.", Alignment: 2},
	}
	zh := map[int]Cue{
		1: {ID: 1, Start: 1000, End: 5000, Plain: "- 留在这里 - 让我检查房间",
			Display: "- 留在这里 - 让我检查房间", Alignment: 2},
		2: {ID: 2, Start: 5000, End: 8000, Plain: "- 嘿 我需要确认一具尸体",
			Display: "- 嘿 我需要确认一具尸体", Alignment: 2},
	}
	// A cut inside cue 1: neither piece holds a further turn, so each one's
	// leading mark goes — the piece no longer opens that turn.
	split := ScreenPlan{ENIDs: []int{1}, ZHIDs: []int{1}, Screens: []ScreenText{
		{EN: "- Stay here", ZH: "- 留在这里"},
		{EN: "- while I check the room.", ZH: "- 让我检查房间"},
	}}
	screens, err := resolveScreenPlan(split, en, zh, nil, planLimits())
	if err != nil {
		t.Fatal(err)
	}
	for _, screen := range screens {
		if strings.HasPrefix(screen.en, "-") || strings.HasPrefix(screen.zh, "-") {
			t.Fatalf("an in-cue piece with no further turn kept its leading mark: %+v", screen)
		}
	}
	// A piece that still holds another turn keeps its marks: it lists speakers
	// rather than opening one. The piece is long enough that the in-cue floor
	// does not interfere.
	turns := map[int]Cue{1: {ID: 1, Start: 1000, End: 12000, Plain: "- one three five - two - four six",
		Display: "- one three five - two - four six", Alignment: 2}}
	turnsZH := map[int]Cue{1: {ID: 1, Start: 1000, End: 12000, Plain: "甲 乙 丙", Display: "甲 乙 丙", Alignment: 2}}
	kept := ScreenPlan{ENIDs: []int{1}, ZHIDs: []int{1}, Screens: []ScreenText{
		{EN: "- one three five", ZH: "甲"},
		{EN: "- two - four six", ZH: "乙 丙"},
	}}
	screens, err = resolveScreenPlan(kept, turns, turnsZH, nil, planLimits())
	if err != nil {
		t.Fatal(err)
	}
	// The first piece opens a turn and holds no further one, so its mark goes.
	if screens[0].en != "one three five" {
		t.Fatalf("a first piece kept its mark: %q", screens[0].en)
	}
	// The second piece still lists two turns, so its marks stay.
	if screens[1].en != "- two - four six" {
		t.Fatalf("a piece that still holds another turn lost its marks: %q", screens[1].en)
	}
	// A whole cue keeps the source text in both languages.
	whole := ScreenPlan{ENIDs: []int{2}, ZHIDs: []int{2}, Screens: []ScreenText{
		{EN: "Hey, I need to ID a body.", ZH: "- 嘿 我需要确认一具尸体"},
	}}
	screens, err = resolveScreenPlan(whole, en, zh, nil, planLimits())
	if err != nil {
		t.Fatal(err)
	}
	if screens[0].zh != "- 嘿 我需要确认一具尸体" {
		t.Fatalf("a whole Chinese cue changed its text: %q", screens[0].zh)
	}
	// A piece cut from a cue whose text has no further speaker turn drops its
	// mark, in both languages. The whole piece is 800ms or more so the in-cue
	// floor does not interfere.
	headCue := map[int]Cue{1: {ID: 1, Start: 1000, End: 9000, Plain: "- Hey, I need to identify a body",
		Display: "- Hey, I need to identify a body", Alignment: 2}}
	headZH := map[int]Cue{1: {ID: 1, Start: 1000, End: 9000, Plain: "- 嘿 我需要确认一具尸体",
		Display: "- 嘿 我需要确认一具尸体", Alignment: 2}}
	head := ScreenPlan{ENIDs: []int{1}, ZHIDs: []int{1}, Screens: []ScreenText{
		{EN: "- Hey,", ZH: "- 嘿"},
		{EN: "I need to identify a body", ZH: "我需要确认一具尸体"},
	}}
	screens, err = resolveScreenPlan(head, headCue, headZH, nil, planLimits())
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(screens[0].zh, "-") || strings.HasPrefix(screens[0].en, "-") {
		t.Fatalf("a cut piece kept its leading mark: %q / %q", screens[0].en, screens[0].zh)
	}
	// A hyphen inside a word is not a speaker mark.
	if got := dropSpeakerMark("阿尔-纳瓦兹"); got != "阿尔-纳瓦兹" {
		t.Fatalf("an interior hyphen was treated as a speaker mark: %q", got)
	}
	if got := dropSpeakerMark("- 瓦西里安德列诺夫"); got != "瓦西里安德列诺夫" {
		t.Fatalf("a leading mark was kept: %q", got)
	}
}

// TestScreenPlanTimesInCueCuts covers validation rule 4: the word time wins,
// and proportional time is the fallback.
func TestScreenPlanTimesInCueCuts(t *testing.T) {
	en := map[int]Cue{1: {ID: 1, Start: 1000, End: 5000, Plain: "Open the door now please",
		Display: "Open the door now please", Alignment: 2}}
	zh := map[int]Cue{
		1: {ID: 1, Start: 1000, End: 3000, Plain: "开门", Display: "开门", Alignment: 2},
		2: {ID: 2, Start: 3000, End: 5000, Plain: "请现在", Display: "请现在", Alignment: 2},
	}
	plan := ScreenPlan{ENIDs: []int{1}, ZHIDs: []int{1, 2}, Screens: []ScreenText{
		{EN: "Open the door", ZH: "开门"},
		{EN: "now please", ZH: "请现在"},
	}}
	words := WordTimings{1: []WordTiming{
		{Text: "Open", StartMS: 1000, EndMS: 1400},
		{Text: "the", StartMS: 1400, EndMS: 1600},
		{Text: "door", StartMS: 1600, EndMS: 2000},
		{Text: "now", StartMS: 2400, EndMS: 2800},
		{Text: "please", StartMS: 2800, EndMS: 5000},
	}}
	withWords, err := resolveScreenPlan(plan, en, zh, words, planLimits())
	if err != nil {
		t.Fatal(err)
	}
	if withWords[1].start != 2200 {
		t.Fatalf("word-time cut = %d, want 2200 (midpoint of the word gap)", withWords[1].start)
	}
	withoutWords, err := resolveScreenPlan(plan, en, zh, nil, planLimits())
	if err != nil {
		t.Fatal(err)
	}
	// Without word times the cut is a proportional share of the cue, still
	// strictly inside it and different from the word-time cut.
	if withoutWords[1].start <= 1000 || withoutWords[1].start >= 5000 {
		t.Fatalf("proportional cut = %d, not inside the cue", withoutWords[1].start)
	}
	if withoutWords[1].start == withWords[1].start {
		t.Fatalf("the proportional fallback reused the word time %d", withoutWords[1].start)
	}
}

// TestScreenPlanInCueFloorAppliesToBothScreens covers validation rule 5: the
// fallback floor is 800ms and both screens next to a cut are checked. Word
// timings fix the cut exactly, so the fixtures isolate the floor.
func TestScreenPlanInCueFloorAppliesToBothScreens(t *testing.T) {
	en := map[int]Cue{1: {ID: 1, Start: 1000, End: 5000, Plain: "Yeah I understand",
		Display: "Yeah I understand", Alignment: 2}}
	zh := map[int]Cue{1: {ID: 1, Start: 1000, End: 5000, Plain: "是 我明白", Display: "是 我明白", Alignment: 2}}
	words := func(cut int) WordTimings {
		return WordTimings{1: []WordTiming{
			{Text: "Yeah", StartMS: 1000, EndMS: cut},
			{Text: "I", StartMS: cut, EndMS: cut + 500},
			{Text: "understand", StartMS: cut + 500, EndMS: 5000},
		}}
	}
	plan := ScreenPlan{ENIDs: []int{1}, ZHIDs: []int{1}, Screens: []ScreenText{
		{EN: "Yeah", ZH: "是"},
		{EN: "I understand", ZH: "我明白"},
	}}
	// 790ms after the start: the first screen is under the floor.
	if _, err := resolveScreenPlan(plan, en, zh, words(1790), planLimits()); err == nil ||
		!strings.Contains(err.Error(), "shorter than 800ms") {
		t.Fatalf("a 790ms screen next to an in-cue cut passed: %v", err)
	}
	// Exactly 800ms on ASS centiseconds: accepted.
	if _, err := resolveScreenPlan(plan, en, zh, words(1800), planLimits()); err != nil {
		t.Fatalf("an 800ms screen next to an in-cue cut was rejected: %v", err)
	}
	// The screen that ENDS at the cut is checked too: a long head and a 790ms
	// tail must fail.
	tailPlan := ScreenPlan{ENIDs: []int{1}, ZHIDs: []int{1}, Screens: []ScreenText{
		{EN: "Yeah I", ZH: "是"},
		{EN: "understand", ZH: "我明白"},
	}}
	tailWords := WordTimings{1: []WordTiming{
		{Text: "Yeah", StartMS: 1000, EndMS: 2000},
		{Text: "I", StartMS: 2000, EndMS: 4210},
		{Text: "understand", StartMS: 4210, EndMS: 5000},
	}}
	if _, err := resolveScreenPlan(tailPlan, en, zh, tailWords, planLimits()); err == nil ||
		!strings.Contains(err.Error(), "shorter than 800ms") {
		t.Fatalf("a 790ms tail screen next to an in-cue cut passed: %v", err)
	}
	tailWords[1][1].EndMS = 4200
	if _, err := resolveScreenPlan(tailPlan, en, zh, tailWords, planLimits()); err != nil {
		t.Fatalf("an 800ms tail screen was rejected: %v", err)
	}
}

// TestScreenPlanLengthRules covers validation rule 6 for both languages.
func TestScreenPlanLengthRules(t *testing.T) {
	en, zh := planCues()
	// One screen holds both English cues and both Chinese cues: its English
	// piece joins two cues, so it has no whole-cue exception.
	long := ScreenPlan{ENIDs: []int{1, 2}, ZHIDs: []int{1}, Screens: []ScreenText{
		{EN: "I never spoke to Mo about the loss of his father and his sisters.", ZH: "我从没跟莫谈过他父亲和他姐妹的死"},
	}}
	// The Chinese of the joined group is one unbreakable cue, so the join rule
	// allows the join and the length rule is what rejects it.
	zhJoined := map[int]Cue{1: {ID: 1, Start: 1000, End: 6000,
		Plain: "我从没跟莫谈过他父亲和他姐妹的死", Display: "我从没跟莫谈过他父亲和他姐妹的死", Alignment: 2}}
	limits := planLimits()
	limits.enChars = 20
	limits.zhChars = 5
	message := planError(t, en, zhJoined, long, limits)
	if !strings.Contains(message, "characters, limit") {
		t.Fatalf("a piece past the length limits passed: %q", message)
	}
	// A piece that is exactly one whole single-line English cue keeps the source
	// exception, even past en_chars.
	limits = planLimits()
	limits.enChars = 5
	if _, err := resolveScreenPlan(exceptionPlan(), en, zh, nil, limits); err != nil {
		t.Fatalf("a whole single-line English cue lost its length exception: %v", err)
	}
	// A Chinese piece past zh_chars is rejected on the character count alone:
	// a plan piece has no measured-width exemption, so even a font that proves
	// one line fits cannot make it pass. The source is two lines, so the
	// single-line exception does not apply either.
	zhTwoLines := map[int]Cue{
		1: {ID: 1, Start: 1000, End: 4000, Plain: "我从没跟莫谈过他父亲\n和他姐妹", Display: "我从没跟莫谈过他父亲 和他姐妹", Alignment: 2},
		2: {ID: 2, Start: 4000, End: 6000, Plain: "和他姐妹的死", Display: "和他姐妹的死", Alignment: 2},
	}
	// exceptionPlan's Chinese pieces must continue this fixture's source text.
	twoLinePlan := ScreenPlan{ENIDs: []int{1, 2}, ZHIDs: []int{1, 2}, Screens: []ScreenText{
		{EN: "I never spoke to Mo about the loss", ZH: "我从没跟莫谈过他父亲"},
		{EN: "of his father and his sisters.", ZH: "和他姐妹 和他姐妹的死"},
	}}
	limits = planLimits()
	limits.zhChars = 5
	message = planError(t, en, zhTwoLines, twoLinePlan, limits)
	if !strings.Contains(message, "characters, limit 5") {
		t.Fatalf("a Chinese piece past the cap was not rejected on its count: %q", message)
	}
	measured := 0
	fontWidth := func(string, string, float64, int, float64, bool) (float64, bool) {
		measured++
		return 100, true
	}
	old := fontWidthMeasurerOverride
	fontWidthMeasurerOverride = fontWidth
	defer func() { fontWidthMeasurerOverride = old }()
	if message := planError(t, en, zhTwoLines, twoLinePlan, limits); !strings.Contains(message, "characters, limit 5") {
		t.Fatalf("a narrow Chinese piece past the cap passed: %q", message)
	}
	if measured != 0 {
		t.Fatalf("the plan check measured width %d times, want none", measured)
	}
}

// TestScreenPlanRejectsOverlappingChineseOnlyScreen covers the overlap rule.
func TestScreenPlanRejectsOverlappingChineseOnlyScreen(t *testing.T) {
	en, zh := planCues()
	// The first screen has no English and covers Chinese cue 1 (1000..4000).
	// The English of that cue is shown later in the plan, so the Chinese-only
	// screen and the English screen overlap.
	plan := ScreenPlan{ENIDs: []int{1, 2}, ZHIDs: []int{1, 2}, Screens: []ScreenText{
		{EN: "", ZH: "我从没跟莫谈过他父亲"},
		{EN: "I never spoke to Mo about the loss", ZH: ""},
		{EN: "of his father and his sisters.", ZH: "和他姐妹的死"},
	}}
	// The order rule rejects it first, which is the same defect; accept either
	// rejection so the test does not depend on which check runs first.
	message := planError(t, en, zh, plan, planLimits())
	if !strings.Contains(message, "overlaps") && !strings.Contains(message, "starts before") {
		t.Fatalf("rejection = %q", message)
	}
}

// TestApplyScreenPlansRejectsOnePlanAndKeepsThatGroup covers the fallback: a
// rejected plan leaves its group on the deterministic rules and never drops a
// source cue.
func TestApplyScreenPlansRejectsOnePlanAndKeepsThatGroup(t *testing.T) {
	bundle := fixtureBundle(
		[]Cue{
			{ID: 1, Start: 1000, End: 4000, OriginalStart: 1000, OriginalEnd: 4000, Plain: "First line.", Display: "First line.", Alignment: 2},
			{ID: 2, Start: 4000, End: 6000, OriginalStart: 4000, OriginalEnd: 6000, Plain: "Second line.", Display: "Second line.", Alignment: 2},
		},
		[]Cue{
			{ID: 1, Start: 1000, End: 6000, OriginalStart: 1000, OriginalEnd: 6000, Plain: "整句", Display: "整句", Alignment: 2},
		},
	)
	mapping := fixtureMapping(bundle, []Group{
		{ENIDs: []int{1, 2}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel},
	})
	mapping.ScreenPlans = []ScreenPlan{{
		ENIDs: []int{1, 2}, ZHIDs: []int{1},
		Screens: []ScreenText{
			{EN: "First line with invented words.", ZH: "整句"},
			{EN: "Second line.", ZH: ""},
		},
	}}
	events, audit := renderPlanEvents(t, bundle, mapping, fixtureConfig())
	if audit == nil || audit.Applied != 0 {
		t.Fatalf("a changed plan was applied: %+v", audit)
	}
	if len(audit.Rejected) == 0 {
		t.Fatal("the rejection reason was not recorded")
	}
	en, zh, err := cueIndex(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if err := verifySourceCoverage(eventsToBlocks(events), en, zh); err != nil {
		t.Fatalf("a rejected plan lost a source cue: %v", err)
	}
}

// eventsToBlocks rebuilds blocks from rendered events for the coverage check.
func eventsToBlocks(events []renderedEvent) []block {
	blocks := make([]block, len(events))
	for i, e := range events {
		blocks[i] = block{start: e.assStart, end: e.assEnd, enIDs: e.enIDs, zhIDs: e.zhIDs}
	}
	return blocks
}

// exceptionPlan is the plan whose pieces are whole single-line cues.
func exceptionPlan() ScreenPlan {
	return ScreenPlan{ENIDs: []int{1, 2}, ZHIDs: []int{1, 2}, Screens: []ScreenText{
		{EN: "I never spoke to Mo about the loss", ZH: "我从没跟莫谈过他父亲"},
		{EN: "of his father and his sisters.", ZH: "和他姐妹的死"},
	}}
}

// TestScreenPlanRejectsReorderedChineseOnlyScreen covers R1 with the reviewer's
// fixture: a plan whose Chinese-only screen falls before the English-anchored
// screen would reverse the Chinese text order, so it is rejected as a whole and
// the group falls back with its order intact.
func TestScreenPlanRejectsReorderedChineseOnlyScreen(t *testing.T) {
	en := map[int]Cue{1: {ID: 1, Start: 3000, End: 5000, Plain: "First line.", Display: "First line.", Alignment: 2}}
	zh := map[int]Cue{
		1: {ID: 1, Start: 1000, End: 2000, Plain: "甲", Display: "甲", Alignment: 2},
		2: {ID: 2, Start: 2200, End: 2800, Plain: "乙", Display: "乙", Alignment: 2},
	}
	plan := ScreenPlan{ENIDs: []int{1}, ZHIDs: []int{1, 2}, Screens: []ScreenText{
		{EN: "First line.", ZH: "甲"},
		{ZH: "乙"},
	}}
	if _, err := resolveScreenPlan(plan, en, zh, nil, planLimits()); err == nil {
		t.Fatal("a plan that reverses Chinese display order was accepted")
	} else if !strings.Contains(err.Error(), "starts before") {
		t.Fatalf("rejection = %q", err)
	}
	bundle := fixtureBundle([]Cue{en[1]}, []Cue{zh[1], zh[2]})
	mapping := fixtureMapping(bundle, []Group{
		{ENIDs: []int{1}, ZHIDs: []int{1, 2}, Layer: LayerBottom, Kind: KindDialogue, Origin: OriginModel},
	})
	mapping.ScreenPlans = []ScreenPlan{plan}
	events, audit := renderPlanEvents(t, bundle, mapping, fixtureConfig())
	if audit.Applied != 0 || len(audit.RejectedGroup) != 1 {
		t.Fatalf("the rejected plan was not recorded: %+v", audit)
	}
	// The fallback must still show the Chinese text in source order.
	var zhText string
	for _, e := range events {
		zhText += e.zhText
	}
	if strings.Index(zhText, "甲") > strings.Index(zhText, "乙") {
		t.Fatalf("Chinese order reversed in the output: %q", zhText)
	}
}

// TestScreenPlanAcceptsChineseOnlyScreenInOrder keeps the order rule from being
// over-strict: a Chinese-only screen that fits between its neighbors is allowed.
func TestScreenPlanAcceptsChineseOnlyScreenInOrder(t *testing.T) {
	en := map[int]Cue{
		1: {ID: 1, Start: 1000, End: 2000, Plain: "First line.", Display: "First line.", Alignment: 2},
		2: {ID: 2, Start: 5000, End: 6000, Plain: "Second line.", Display: "Second line.", Alignment: 2},
	}
	zh := map[int]Cue{
		1: {ID: 1, Start: 1000, End: 2000, Plain: "甲", Display: "甲", Alignment: 2},
		2: {ID: 2, Start: 3000, End: 4000, Plain: "乙", Display: "乙", Alignment: 2},
		3: {ID: 3, Start: 5000, End: 6000, Plain: "丙", Display: "丙", Alignment: 2},
	}
	plan := ScreenPlan{ENIDs: []int{1, 2}, ZHIDs: []int{1, 2, 3}, Screens: []ScreenText{
		{EN: "First line.", ZH: "甲"},
		{ZH: "乙"},
		{EN: "Second line.", ZH: "丙"},
	}}
	if _, err := resolveScreenPlan(plan, en, zh, nil, planLimits()); err != nil {
		t.Fatalf("an in-order Chinese-only screen was rejected: %v", err)
	}
}
