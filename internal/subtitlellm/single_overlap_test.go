package subtitlellm

import (
	"strings"
	"testing"
)

// renderSingleOverlap renders the given cues and groups and returns the
// event holding the paired English line and the event holding the
// Chinese-only line "安德列诺夫先生".
func renderSingleOverlap(t *testing.T, en, zh []Cue, groups []Group) (dialogue, dialogue) {
	t.Helper()
	noWidthMeasurer(t)
	bundle := fixtureBundle(en, zh)
	out, _, err := renderWithAudits(bundle, fixtureMapping(bundle, groups), fixtureConfig(), fixtureStyle(), 1920, 1080, nil)
	if err != nil {
		t.Fatal(err)
	}
	var pair, single dialogue
	var pairs, singles int
	for _, e := range parseDialogues(t, string(out)) {
		if strings.Contains(e.text, "Ask any questions") {
			pair = e
			pairs++
		}
		if strings.Contains(e.text, "安德列诺夫先生") {
			single = e
			singles++
		}
	}
	if pairs != 1 || singles != 1 {
		t.Fatalf("want one pair and one single event, got %d and %d:\n%s", pairs, singles, out)
	}
	return pair, single
}

var (
	singleOverlapEN  = Cue{ID: 1, Display: "Ask any questions you want.", Plain: "Ask any questions you want."}
	singleOverlapZH  = Cue{Display: "想问什么问题随你", Plain: "想问什么问题随你"}
	singleOverlapZH1 = Cue{Display: "安德列诺夫先生", Plain: "安德列诺夫先生"}
)

func cueAt(c Cue, id, start, end int) Cue {
	c.ID, c.Start, c.End = id, start, end
	return c
}

// The 27:23 case: the pair ends on English time 0.46 s after the Chinese-only
// line starts. The pair yields, so the single keeps its Chinese time.
func TestPairYieldsToFollowingChineseSingle(t *testing.T) {
	pair, single := renderSingleOverlap(t,
		[]Cue{cueAt(singleOverlapEN, 1, 21600, 23810)},
		[]Cue{cueAt(singleOverlapZH, 1, 21770, 23350), cueAt(singleOverlapZH1, 2, 23350, 24520)},
		[]Group{
			{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
			{ZHIDs: []int{2}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
		})
	if pair.start != 21600 || pair.end != 23350 || single.start != 23350 || single.end != 24520 {
		t.Fatalf("pair %d-%d, single %d-%d; want pair 21600-23350, single 23350-24520", pair.start, pair.end, single.start, single.end)
	}
}

// The 35:34 case: the pair starts on English time 0.501 s (0.50 s in the
// file) before the Chinese-only line ends, and starts when the single ends.
func TestPairYieldsToPrecedingChineseSingle(t *testing.T) {
	pair, single := renderSingleOverlap(t,
		[]Cue{cueAt(singleOverlapEN, 1, 34591, 36468)},
		[]Cue{cueAt(singleOverlapZH1, 1, 34007, 35092), cueAt(singleOverlapZH, 2, 35634, 36468)},
		[]Group{
			{ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
			{ENIDs: []int{1}, ZHIDs: []int{2}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
		})
	if pair.start != 35090 || pair.end != 36470 || single.start != 34010 || single.end != 35090 {
		t.Fatalf("pair %d-%d, single %d-%d; want pair 35090-36470, single 34010-35090", pair.start, pair.end, single.start, single.end)
	}
}

// A pair that would drop below one second keeps its time; the Chinese-only
// line is cut instead.
func TestShortPairCutsChineseSingle(t *testing.T) {
	pair, single := renderSingleOverlap(t,
		[]Cue{cueAt(singleOverlapEN, 1, 10000, 11200)},
		[]Cue{cueAt(singleOverlapZH, 1, 10000, 11200), cueAt(singleOverlapZH1, 2, 10800, 12000)},
		[]Group{
			{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
			{ZHIDs: []int{2}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
		})
	if pair.start != 10000 || pair.end != 11200 || single.start != 11200 || single.end != 12000 {
		t.Fatalf("pair %d-%d, single %d-%d; want pair 10000-11200, single 11200-12000", pair.start, pair.end, single.start, single.end)
	}
}

// An overlap longer than 0.5 s is speech at the same time, not two timings of
// one pause; both lines keep their times.
func TestLongOverlapWithChineseSingleStays(t *testing.T) {
	pair, single := renderSingleOverlap(t,
		[]Cue{cueAt(singleOverlapEN, 1, 21600, 24400)},
		[]Cue{cueAt(singleOverlapZH, 1, 21600, 24400), cueAt(singleOverlapZH1, 2, 23350, 25000)},
		[]Group{
			{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
			{ZHIDs: []int{2}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
		})
	if pair.end != 24400 || single.start != 23350 {
		t.Fatalf("pair ends %d, single starts %d; want 24400 and 23350", pair.end, single.start)
	}
}
