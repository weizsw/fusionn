package subtitlellm

import (
	"reflect"
	"strings"
	"testing"
)

// speakerSplitFixture is the 10:49 case: one two-speaker Chinese cue over two
// English cues, one per speaker.
func speakerSplitFixture(zhPlain string, enTexts ...string) (Bundle, []Group) {
	en := []Cue{
		{ID: 1, Start: 649400, End: 651780, Display: enTexts[0], Plain: enTexts[0]},
		{ID: 2, Start: 651780, End: 653070, Display: enTexts[1], Plain: enTexts[1]},
	}
	zh := []Cue{{ID: 1, Start: 649400, End: 653070, Plain: zhPlain, Display: strings.ReplaceAll(zhPlain, "\n", " "), MultiSpeaker: true}}
	return fixtureBundle(en, zh), []Group{{ENIDs: []int{1, 2}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue}}
}

// zhPart returns the Chinese text of an event without override tags.
func zhPart(text string) string {
	if i := strings.Index(text, `{\rEng}`); i >= 0 {
		text = strings.TrimSuffix(text[:i], `\N`)
	}
	return stripOverrideTags(text)
}

func renderSpeakerSplit(t *testing.T, bundle Bundle, mapping Mapping) []dialogue {
	t.Helper()
	noWidthMeasurer(t)
	out, _, err := renderWithAudits(bundle, mapping, fixtureConfig(), fixtureStyle(), 1920, 1080, nil)
	if err != nil {
		t.Fatal(err)
	}
	return parseDialogues(t, string(out))
}

func TestTwoSpeakerChineseFollowsEnglishCues(t *testing.T) {
	bundle, groups := speakerSplitFixture("- 让里斯和他的队员得了脑瘤\n- 好吧 但这里是美国",
		"gave Reece and his men brain tumors.", "Okay, but this is America.")
	mapping := confirmAllSplits(t, bundle, fixtureMapping(bundle, groups), fixtureConfig())
	want := []SplitCheck{{
		ZH:   []string{"让里斯和他的队员得了脑瘤", "好吧 但这里是美国"},
		EN:   []string{"gave Reece and his men brain tumors.", "Okay, but this is America."},
		Same: true,
	}}
	if !reflect.DeepEqual(mapping.SplitChecks, want) {
		t.Fatalf("split checks = %+v, want %+v", mapping.SplitChecks, want)
	}
	events := renderSpeakerSplit(t, bundle, mapping)
	if len(events) != 2 {
		t.Fatalf("want 2 events, got %d: %+v", len(events), events)
	}
	for i, w := range []struct {
		start, end int
		zh, en     string
	}{
		{649400, 651780, "让里斯和他的队员得了脑瘤", "gave Reece"},
		{651780, 653070, "好吧 但这里是美国", "Okay, but"},
	} {
		e := events[i]
		if e.start != w.start || e.end != w.end || zhPart(e.text) != w.zh || !strings.Contains(e.text, w.en) {
			t.Errorf("event %d = %d-%d %q, want %d-%d with %q and %q", i, e.start, e.end, e.text, w.start, w.end, w.zh, w.en)
		}
	}
}

func TestTwoSpeakerChineseStaysWithoutConfirmedOrder(t *testing.T) {
	bundle, groups := speakerSplitFixture("- 让里斯和他的队员得了脑瘤\n- 好吧 但这里是美国",
		"gave Reece and his men brain tumors.", "Okay, but this is America.")
	events := renderSpeakerSplit(t, bundle, fixtureMapping(bundle, groups))
	if len(events) != 1 || !strings.Contains(events[0].text, "- 让里斯") || !strings.Contains(events[0].text, "- 好吧") {
		t.Fatalf("want one event with both speakers, got %+v", events)
	}
}

// Lines that fit side by side stay together: the source shows them at once.
func TestShortTwoSpeakerChineseStaysWithJoinedEnglish(t *testing.T) {
	bundle, groups := speakerSplitFixture("- 好吧\n- 走吧", "Okay.", "Let's go.")
	mapping := confirmAllSplits(t, bundle, fixtureMapping(bundle, groups), fixtureConfig())
	if len(mapping.SplitChecks) != 0 {
		t.Fatalf("no question expected, got %+v", mapping.SplitChecks)
	}
	events := renderSpeakerSplit(t, bundle, mapping)
	if len(events) != 1 || zhPart(events[0].text) != "- 好吧 - 走吧" {
		t.Fatalf("want one event with both speakers on one line, got %+v", events)
	}
}
