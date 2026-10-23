package subtitlellm

import (
	"strings"
	"testing"
)

// switchFixture is the 11:33 case: the first English cue is too long and
// splits at its line break, and the Chinese switches at zhSwitch.
func switchFixture(zhSwitch int) (Bundle, []Group) {
	bundle := fixtureBundle(
		[]Cue{
			{ID: 1, Start: 1000, End: 4250, Display: "Depending on who you believe, James Reece is either in Paris, France", Plain: "Depending on who you believe,\nJames Reece is either in Paris, France"},
			{ID: 2, Start: 4250, End: 6000, Display: "or Paris, Texas.", Plain: "or Paris, Texas."},
		},
		[]Cue{
			{ID: 1, Start: 1120, End: zhSwitch, Display: "有各种说法", Plain: "有各种说法"},
			{ID: 2, Start: zhSwitch, End: 5710, Display: "说詹姆斯里斯在法国巴黎 或在得克萨斯州的巴黎", Plain: "说詹姆斯里斯在法国巴黎\n或在得克萨斯州的巴黎"},
		},
	)
	groups := []Group{{ENIDs: []int{1, 2}, ZHIDs: []int{1, 2}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue}}
	return bundle, groups
}

func renderSwitchFixture(t *testing.T, zhSwitch int, words WordTimings) []dialogue {
	t.Helper()
	noWidthMeasurer(t)
	bundle, groups := switchFixture(zhSwitch)
	out, _, err := renderWithAudits(bundle, fixtureMapping(bundle, groups), fixtureConfig(), fixtureStyle(), 1920, 1080, words)
	if err != nil {
		t.Fatal(err)
	}
	events := parseDialogues(t, string(out))
	for _, e := range events {
		if e.end-e.start < snapToleranceMS {
			t.Errorf("flash event %d-%d: %s", e.start, e.end, e.text)
		}
	}
	return events
}

// switchAt returns the time the English part "James Reece..." first shows and
// the time the Chinese "说詹姆斯里斯..." first shows.
func switchAt(t *testing.T, events []dialogue) (int, int) {
	t.Helper()
	en, zh := -1, -1
	for _, e := range events {
		if en < 0 && strings.Contains(e.text, "James Reece") {
			en = e.start
		}
		if zh < 0 && strings.Contains(e.text, "说詹姆斯里斯") {
			zh = e.start
		}
	}
	if en < 0 || zh < 0 {
		t.Fatalf("missing text: %+v", events)
	}
	return en, zh
}

// Without word timing the English break is estimated from text length; the
// Chinese switch was timed by ear, so the English break moves to it.
func TestEnglishSplitFollowsNearbyChineseSwitch(t *testing.T) {
	en, zh := switchAt(t, renderSwitchFixture(t, 2500, nil))
	if en != 2500 || zh != 2500 {
		t.Fatalf("switch English %d, Chinese %d; want both 2500", en, zh)
	}
}

// With word timing the English break is where the words break; the Chinese
// switch within snapToleranceMS of it moves there.
func TestChineseSwitchFollowsEnglishWordCut(t *testing.T) {
	words := WordTimings{1: {
		{Text: "Depending", StartMS: 1000, EndMS: 1300}, {Text: "on", StartMS: 1300, EndMS: 1400},
		{Text: "who", StartMS: 1400, EndMS: 1550}, {Text: "you", StartMS: 1550, EndMS: 1700},
		{Text: "believe,", StartMS: 1700, EndMS: 2250}, {Text: "James", StartMS: 2350, EndMS: 2600},
		{Text: "Reece", StartMS: 2600, EndMS: 2900}, {Text: "is", StartMS: 2900, EndMS: 3000},
		{Text: "either", StartMS: 3000, EndMS: 3300}, {Text: "in", StartMS: 3300, EndMS: 3400},
		{Text: "Paris,", StartMS: 3400, EndMS: 3700}, {Text: "France", StartMS: 3700, EndMS: 4200},
	}}
	en, zh := switchAt(t, renderSwitchFixture(t, 2500, words))
	if en != 2300 || zh != 2300 {
		t.Fatalf("switch English %d, Chinese %d; want both 2300", en, zh)
	}
}

// A Chinese switch farther than snapToleranceMS from the English break marks
// another place in the sentence; both stay where they are.
func TestDistantChineseSwitchStays(t *testing.T) {
	en, zh := switchAt(t, renderSwitchFixture(t, 3200, nil))
	if zh != 3200 || en == 3200 {
		t.Fatalf("switch English %d, Chinese %d; want Chinese 3200 and English unchanged", en, zh)
	}
}
