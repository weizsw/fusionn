package subtitlellm

import (
	"reflect"
	"testing"
)

// irregularStarts returns count cue starts with uneven gaps, so that only one
// shift lines two copies up.
func irregularStarts(count, seed int) []int {
	starts := make([]int, count)
	t := 1000
	for i := range starts {
		starts[i] = t
		t += 1500 + (i*7919+seed*104729)%2300
	}
	return starts
}

func cuesAt(starts []int, shift int) []Cue {
	cues := make([]Cue, len(starts))
	for i, s := range starts {
		cues[i] = Cue{ID: i + 1, Start: s + shift, End: s + shift + 1200, OriginalStart: s + shift, OriginalEnd: s + shift + 1200, Plain: "line"}
	}
	return cues
}

func TestEstimateZHShift(t *testing.T) {
	starts := irregularStarts(200, 1)
	en := cuesAt(starts, 0)
	tests := []struct {
		name string
		zh   []Cue
		want int
	}{
		{"synced tracks", cuesAt(starts, 0), 0},
		{"chinese 12s late", cuesAt(starts, 12000), -12000},
		{"chinese 95s early", cuesAt(starts, -95000), 95000},
		{"offset below threshold", cuesAt(starts, 300), 0},
		{"unrelated tracks", cuesAt(irregularStarts(200, 7), 0), 0},
		{"too few cues", cuesAt(starts[:minShiftMatches-1], 12000), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := estimateZHShift(en, tt.zh); got != tt.want {
				t.Fatalf("estimateZHShift() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestEstimateZHShiftIgnoresPartialMatch(t *testing.T) {
	// Only the first 30 of 200 Chinese cues follow the English timing after a
	// shift; the rest is unrelated. A fifth of the track must match.
	starts := irregularStarts(200, 1)
	en := cuesAt(starts, 0)
	zh := append(cuesAt(starts[:30], 12000), cuesAt(irregularStarts(170, 9), 600000)...)
	if got := estimateZHShift(en, zh); got != 0 {
		t.Fatalf("estimateZHShift() = %d, want 0", got)
	}
}

func TestShiftCuesKeepsOriginalTimes(t *testing.T) {
	cues := []Cue{
		{ID: 1, Start: 500, End: 1500, OriginalStart: 500, OriginalEnd: 1500},
		{ID: 2, Start: 3000, End: 5000, OriginalStart: 3000, OriginalEnd: 5000},
		{ID: 3, Start: 10000, End: 12000, OriginalStart: 10000, OriginalEnd: 12000},
	}
	got := shiftCues(cues, -4000)
	want := [][2]int{{0, 1000}, {0, 1000}, {6000, 8000}}
	for i, cue := range got {
		if [2]int{cue.Start, cue.End} != want[i] {
			t.Errorf("cue %d = [%d,%d], want %v", cue.ID, cue.Start, cue.End, want[i])
		}
		if cue.OriginalStart != cues[i].OriginalStart || cue.OriginalEnd != cues[i].OriginalEnd {
			t.Errorf("cue %d original times changed", cue.ID)
		}
	}
	if cues[2].Start != 10000 {
		t.Fatal("shiftCues modified its input")
	}
}

func TestIsTimingDialogue(t *testing.T) {
	tests := []struct {
		cue  Cue
		want bool
	}{
		{Cue{Plain: "How are you?"}, true},
		{Cue{Plain: "你好吗？"}, true},
		{Cue{Plain: "(Laughs) Fine."}, true},
		{Cue{Plain: "（伦敦）"}, false},
		{Cue{Plain: "[door slams]"}, false},
		{Cue{Plain: "【字幕组】"}, false},
		{Cue{Plain: "♪ Hold me closer ♪"}, false},
		{Cue{Plain: "♪ Silent night ♪\n♪ holy night ♪"}, false},
		{Cue{Plain: "- ♪ Silent night ♪\n- Yeah, thank you."}, true},
		{Cue{Plain: "Hello.", Top: true}, false},
		{Cue{Plain: "  "}, false},
	}
	for _, tt := range tests {
		if got := isTimingDialogue(tt.cue); got != tt.want {
			t.Errorf("isTimingDialogue(%q, top=%v) = %v, want %v", tt.cue.Plain, tt.cue.Top, got, tt.want)
		}
	}
}

func timingCue(id, start, end int, text string) Cue {
	return Cue{ID: id, Start: start, End: end, OriginalStart: start, OriginalEnd: end, Plain: text, Display: text}
}

func restoredGroup(lang string, id int) Group {
	g := Group{ENIDs: []int{}, ZHIDs: []int{}, Layer: LayerBottom, Origin: OriginRestored, Kind: KindUnknown}
	if lang == "en" {
		g.ENIDs = []int{id}
	} else {
		g.ZHIDs = []int{id}
	}
	return g
}

func groupIDs(groups []Group) [][2][]int {
	out := make([][2][]int, len(groups))
	for i, g := range groups {
		out[i] = [2][]int{g.ENIDs, g.ZHIDs}
	}
	return out
}

func TestPairRestoredByTiming(t *testing.T) {
	bundle := Bundle{
		EN: []Cue{
			timingCue(1, 0, 2000, "Hello."),
			timingCue(2, 3000, 5000, "How are you?"),
			timingCue(3, 6000, 9000, "Fine, thanks, and you?"),
			timingCue(4, 10000, 12000, "♪ Song ♪"),
		},
		ZH: []Cue{
			timingCue(1, 0, 2000, "你好。"),
			timingCue(2, 3100, 5000, "你好吗？"),
			timingCue(3, 6000, 7400, "很好，谢谢"),
			timingCue(4, 7500, 9000, "你呢？"),
			timingCue(5, 10000, 12000, "（伦敦）"),
		},
	}
	groups := []Group{
		{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
		restoredGroup("en", 2), restoredGroup("en", 3), restoredGroup("en", 4),
		restoredGroup("zh", 2), restoredGroup("zh", 3), restoredGroup("zh", 4), restoredGroup("zh", 5),
	}
	got, paired := pairRestoredByTiming(bundle, groups)

	want := [][2][]int{
		{{1}, {1}},
		{{2}, {2}},
		{{3}, {3, 4}},
		{{4}, {}},
		{{}, {5}},
	}
	if !reflect.DeepEqual(groupIDs(got), want) {
		t.Fatalf("groups = %v, want %v", groupIDs(got), want)
	}
	for _, g := range got[1:3] {
		if g.Origin != OriginTiming || g.Layer != LayerBottom || g.Kind != KindDialogue {
			t.Errorf("timing group = %+v", g)
		}
	}
	if got[0].Origin != OriginModel || got[3].Origin != OriginRestored || got[4].Origin != OriginRestored {
		t.Errorf("origins = %s/%s/%s", got[0].Origin, got[3].Origin, got[4].Origin)
	}
	wantPaired := map[string][]int{"en": {2, 3}, "zh": {2, 3, 4}}
	if !reflect.DeepEqual(paired, wantPaired) {
		t.Fatalf("paired = %v, want %v", paired, wantPaired)
	}
}

func TestPairRestoredByTimingNonAdjacentClusterKeepsMutualBest(t *testing.T) {
	// EN 2 belongs to a model group, so EN 1 and EN 3 are not adjacent.
	// ZH 1 overlaps both enough; only its best match is paired.
	bundle := Bundle{
		EN: []Cue{
			timingCue(1, 0, 3000, "Wait."),
			timingCue(2, 3000, 3500, "No."),
			timingCue(3, 3500, 6500, "Listen to me."),
		},
		ZH: []Cue{
			timingCue(1, 800, 6000, "听我说"),
			timingCue(2, 3000, 3500, "不"),
		},
	}
	groups := []Group{
		restoredGroup("en", 1),
		{ENIDs: []int{2}, ZHIDs: []int{2}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
		restoredGroup("en", 3),
		restoredGroup("zh", 1),
	}
	got, paired := pairRestoredByTiming(bundle, groups)
	// Groups are ordered by their earliest cue: ZH 1 starts at 800.
	want := [][2][]int{{{1}, {}}, {{3}, {1}}, {{2}, {2}}}
	if !reflect.DeepEqual(groupIDs(got), want) {
		t.Fatalf("groups = %v, want %v", groupIDs(got), want)
	}
	if !reflect.DeepEqual(paired, map[string][]int{"en": {3}, "zh": {1}}) {
		t.Fatalf("paired = %v", paired)
	}
}

func TestPairRestoredByTimingLeavesModelDecisionsAlone(t *testing.T) {
	bundle := Bundle{
		EN: []Cue{timingCue(1, 0, 2000, "Hello."), timingCue(2, 3000, 5000, "Bye.")},
		ZH: []Cue{timingCue(1, 0, 2000, "你好。"), timingCue(2, 3000, 5000, "再见。")},
	}
	topZH := restoredGroup("zh", 2)
	topZH.Layer = LayerTop
	groups := []Group{
		// The model kept EN 1 one-sided on purpose.
		{ENIDs: []int{1}, ZHIDs: []int{}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
		restoredGroup("zh", 1),
		restoredGroup("en", 2),
		topZH,
	}
	got, paired := pairRestoredByTiming(bundle, groups)
	if !reflect.DeepEqual(got, groups) {
		t.Fatalf("groups changed: %v", groupIDs(got))
	}
	if len(paired["en"]) != 0 || len(paired["zh"]) != 0 {
		t.Fatalf("paired = %v, want none", paired)
	}
}

func TestPairingStats(t *testing.T) {
	bundle := Bundle{
		EN: []Cue{
			timingCue(1, 0, 1000, "Hi."),
			timingCue(2, 1000, 2000, "Who?"),
			timingCue(3, 2000, 3000, "♪ la ♪"),
			timingCue(4, 3000, 4000, "LONDON"),
		},
		ZH: []Cue{
			timingCue(1, 0, 1000, "嗨"),
			timingCue(2, 2000, 3000, "（伦敦）"),
			timingCue(3, 3000, 4000, "伦敦"),
		},
	}
	groups := []Group{
		{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Kind: KindDialogue},
		{ENIDs: []int{2}, ZHIDs: []int{}, Layer: LayerBottom, Kind: KindDialogue},
		{ENIDs: []int{3}, ZHIDs: []int{2}, Layer: LayerBottom, Kind: KindDialogue},
		{ENIDs: []int{4}, ZHIDs: []int{3}, Layer: LayerTop, Kind: KindOnScreenText},
	}
	got := pairingStats(bundle, groups)
	want := Pairing{ENPaired: 1, ENTotal: 2, ZHPaired: 1, ZHTotal: 1}
	if got != want {
		t.Fatalf("pairingStats() = %+v, want %+v", got, want)
	}
	if got.ENRatio() != 0.5 || got.ZHRatio() != 1 {
		t.Fatalf("ratios = %v/%v", got.ENRatio(), got.ZHRatio())
	}
	if (Pairing{}).ENRatio() != 0 {
		t.Fatal("empty ratio should be 0")
	}
	if !got.Low() || (Pairing{}).Low() || (Pairing{ENPaired: 3, ENTotal: 4}).Low() {
		t.Fatal("Low() should flag only English pairing below LowPairingRatio")
	}
}
