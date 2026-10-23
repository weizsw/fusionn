package subtitlellm

import (
	"reflect"
	"strings"
	"testing"

	"github.com/fusionn/internal/config"
)

func TestLongCueSplitsAndHoldsShortChinese(t *testing.T) {
	cfg := renderTestConfig()
	cfg.EnChars = 22
	bundle := makeBundle(
		[]Cue{{ID: 1, Start: 1000, End: 7000, Display: "We should leave now and return tomorrow.", Plain: "We should leave now\nand return tomorrow."}},
		[]Cue{{ID: 1, Start: 1000, End: 7000, Display: "我们该走了，明天再来"}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel}})
	words := WordTimings{1: {
		{Text: "We", StartMS: 1000, EndMS: 1200},
		{Text: "should", StartMS: 1250, EndMS: 1500},
		{Text: "leave", StartMS: 1550, EndMS: 1900},
		{Text: "now", StartMS: 2000, EndMS: 2400},
		{Text: "and", StartMS: 2800, EndMS: 3000},
		{Text: "return", StartMS: 3100, EndMS: 3500},
		{Text: "tomorrow", StartMS: 3600, EndMS: 4300},
	}}
	output, err := RenderWithWordTimings(bundle, mapping, cfg, renderTestStyle(), 1920, 1080, words)
	if err != nil {
		t.Fatal(err)
	}
	events := parseDialogues(t, string(output))
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	if events[0].start != 1000 || events[0].end != 2600 || events[1].start != 2600 || events[1].end != 7000 {
		t.Fatalf("unexpected word-aligned boundaries: %+v", events)
	}
	for i, event := range events {
		if strings.Count(event.text, `\N`) != 1 {
			t.Fatalf("event %d is not one Chinese plus one English line: %s", i, event.text)
		}
		if !strings.Contains(event.text, "我们该走了，明天再来") {
			t.Fatalf("event %d did not hold the short Chinese", i)
		}
	}
	assertGlyphs(t, events[0].text, "我们该走了，明天再来We should leave now")
	assertGlyphs(t, events[1].text, "我们该走了，明天再来and return tomorrow.")
}

func TestBadWordTimingFallsBackToProportion(t *testing.T) {
	cfg := renderTestConfig()
	cfg.EnChars = 22
	bundle := makeBundle(
		[]Cue{{ID: 1, Start: 1000, End: 7000, Display: "First part of this then the second part.", Plain: "First part of this\nthen the second part."}},
		[]Cue{{ID: 1, Start: 1000, End: 7000, Display: "保留中文"}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel}})
	baseline, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	for name, words := range map[string]WordTimings{
		"wrong words":          {1: {{Text: "invented", StartMS: 2000, EndMS: 3000}}},
		"outside cue":          {1: {{Text: "First part of this", StartMS: 0, EndMS: 50}, {Text: "then the second part", StartMS: 50, EndMS: 80}}},
		"zero duration at cut": {1: {{Text: "First part of this", StartMS: 2000, EndMS: 2000}, {Text: "then the second part", StartMS: 3000, EndMS: 4000}}},
	} {
		t.Run(name, func(t *testing.T) {
			output, err := RenderWithWordTimings(bundle, mapping, cfg, renderTestStyle(), 1920, 1080, words)
			if err != nil {
				t.Fatal(err)
			}
			if string(output) != string(baseline) {
				t.Fatal("invalid word timing did not use the proportional result")
			}
		})
	}
	if len(parseDialogues(t, string(baseline))) != 2 {
		t.Fatal("proportional baseline must actually split")
	}
}

func TestMultilineSourceWithinConfiguredLimitStaysWhole(t *testing.T) {
	bundle := makeBundle(
		[]Cue{{ID: 1, Start: 1000, End: 7000, Display: "First half and the second half.", Plain: "First half\nand the second half."}},
		[]Cue{{ID: 1, Start: 1000, End: 7000, Display: "完整一句"}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel}})
	output, err := Render(bundle, mapping, renderTestConfig(), renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	if events := parseDialogues(t, string(output)); len(events) != 1 {
		t.Fatalf("source line break triggered a split below the configured limit: %d", len(events))
	}
}

func TestMNSplitsOverLimitChineseUsingSourceBoundary(t *testing.T) {
	cfg := renderTestConfig()
	cfg.ZhChars = 8
	bundle := makeBundle(
		[]Cue{{ID: 1, Start: 1000, End: 4000, Display: "We keep laughing."}, {ID: 2, Start: 4000, End: 6500, Display: "The pile gets bigger."}},
		[]Cue{{ID: 1, Start: 900, End: 3900, Display: "看着那堆叶子"}, {ID: 2, Start: 4000, End: 6700, Display: "我们一直笑"}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1, 2}, ZHIDs: []int{1, 2}, Layer: LayerBottom, Origin: OriginModel}})
	output, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	events := parseDialogues(t, string(output))
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	assertGlyphs(t, events[0].text, "看着那堆叶子We keep laughing.")
	assertGlyphs(t, events[1].text, "我们一直笑The pile gets bigger.")
	if events[0].start != 1000 || events[0].end != 4000 || events[1].start != 4000 || events[1].end != 6500 {
		t.Fatalf("source cuts changed the English outer bounds: %+v", events)
	}
}

func TestBothLongLanguagesSwitchTogether(t *testing.T) {
	cfg := renderTestConfig()
	cfg.EnChars, cfg.ZhChars = 22, 8
	bundle := makeBundle(
		[]Cue{{ID: 1, Start: 1000, End: 7000, Display: "We should leave now and return tomorrow.", Plain: "We should leave now\nand return tomorrow."}},
		[]Cue{{ID: 1, Start: 1000, End: 7000, Display: "我们现在应该离开 明天还可以再来", Plain: "我们现在应该离开\n明天还可以再来"}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel}})
	mapping = confirmAllSplits(t, bundle, mapping, cfg)
	output, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	events := parseDialogues(t, string(output))
	if len(events) != 2 {
		t.Fatalf("both sides must share one boundary, got %d events", len(events))
	}
	for _, event := range events {
		if strings.Count(event.text, `\N`) != 1 {
			t.Fatalf("extra forced line: %s", event.text)
		}
	}
	assertGlyphs(t, events[0].text, "我们现在应该离开We should leave now")
	assertGlyphs(t, events[1].text, "明天还可以再来and return tomorrow.")
}

func TestLongChineseDoesNotRestartOnEveryEnglishSlice(t *testing.T) {
	cfg := renderTestConfig()
	cfg.ZhChars = 8
	bundle := makeBundle(
		[]Cue{{ID: 1, Start: 1000, End: 4000, Display: "First English."}, {ID: 2, Start: 4000, End: 7000, Display: "Second English."}},
		[]Cue{{ID: 1, Start: 1000, End: 7000, Display: "中文的第一部分 中文的第二部分", Plain: "中文的第一部分\n中文的第二部分"}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1, 2}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel}})
	mapping = confirmAllSplits(t, bundle, mapping, cfg)
	output, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	events := parseDialogues(t, string(output))
	if len(events) != 2 {
		t.Fatalf("held Chinese must split once across the group, got %d events", len(events))
	}
	assertGlyphs(t, events[0].text, "中文的第一部分First English.")
	assertGlyphs(t, events[1].text, "中文的第二部分Second English.")
}

func TestShortDurationAndMultipleSpeakersAreNotSplit(t *testing.T) {
	for name, cue := range map[string]Cue{
		"short duration":    {ID: 1, Start: 1000, End: 2200, Display: "A long subtitle that cannot be read in tiny fragments."},
		"multiple speakers": {ID: 1, Start: 1000, End: 7000, Display: "- First speaker with a long line.\n- Another speaker answers.", MultiSpeaker: true},
	} {
		t.Run(name, func(t *testing.T) {
			bundle := makeBundle([]Cue{cue}, []Cue{{ID: 1, Start: cue.Start, End: cue.End, Display: "保留原句"}})
			mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel}})
			cfg := renderTestConfig()
			cfg.EnChars = 22
			output, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
			if err != nil {
				t.Fatal(err)
			}
			if events := parseDialogues(t, string(output)); len(events) != 1 {
				t.Fatalf("expected original event, got %d", len(events))
			}
		})
	}
}

func TestWordAlignmentCandidatesUseConfiguredLimit(t *testing.T) {
	bundle := Bundle{EN: []Cue{
		{ID: 1, Start: 1000, End: 6000, Display: "A short sentence with a second part."},
		{ID: 2, Start: 7000, End: 12000, Display: "A second speaker says something long.", MultiSpeaker: true},
	}}
	cfg := config.LLMConfig{EnChars: 60}
	if cues := WordAlignmentCues(bundle, cfg); len(cues) != 0 {
		t.Fatalf("below limit produced %d alignment candidates", len(cues))
	}
	cfg.EnChars = 20
	if cues := WordAlignmentCues(bundle, cfg); len(cues) != 1 || cues[0].ID != 1 {
		t.Fatalf("configured limit or speaker protection ignored: %+v", cues)
	}
}

func TestOriginalSingleLineCanExceedConfiguredLimits(t *testing.T) {
	cfg := renderTestConfig()
	cfg.EnChars, cfg.ZhChars = 20, 8
	english := "Keep this original single line together even above the limit."
	chinese := "原字幕只有一行就允许保持完整"
	bundle := makeBundle(
		[]Cue{{ID: 1, Start: 1000, End: 7000, Display: english}},
		[]Cue{{ID: 1, Start: 1000, End: 7000, Display: chinese}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel}})
	output, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	events := parseDialogues(t, string(output))
	if len(events) != 1 || strings.Count(events[0].text, `\N`) != 1 {
		t.Fatalf("original single lines were split or wrapped: %+v", events)
	}
	assertGlyphs(t, events[0].text, chinese+english)
	if candidates := WordAlignmentCues(bundle, cfg); len(candidates) != 0 {
		t.Fatalf("original single line requested audio alignment: %+v", candidates)
	}
}

func TestSingleLineExceptionDoesNotPermitOverLimitJoins(t *testing.T) {
	cfg := renderTestConfig()
	cfg.ZhChars = 8
	bundle := makeBundle(
		[]Cue{{ID: 1, Start: 1000, End: 4000, Display: "First English."}, {ID: 2, Start: 4000, End: 7000, Display: "Second English."}},
		[]Cue{{ID: 1, Start: 1000, End: 4000, Display: "中文第一句话"}, {ID: 2, Start: 4000, End: 7000, Display: "中文第二句话"}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1, 2}, ZHIDs: []int{1, 2}, Layer: LayerBottom, Origin: OriginModel}})
	output, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	events := parseDialogues(t, string(output))
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2 source slices", len(events))
	}
	assertGlyphs(t, events[0].text, "中文第一句话First English.")
	assertGlyphs(t, events[1].text, "中文第二句话Second English.")
}

func TestChineseSourceBreakSurvivesMinimumReadingTime(t *testing.T) {
	cfg := renderTestConfig()
	cfg.ZhChars = 20
	english := "Tell me if it starts to weaken or get thready."
	bundle := makeBundle(
		[]Cue{{ID: 1, Start: 380964, End: 384176, Display: english, Plain: "Tell me if it starts to weaken\nor get thready."}},
		[]Cue{{ID: 1, Start: 380964, End: 384176, Display: "如果脉搏开始变弱 或者变得细弱难辨 马上告诉我", Plain: "如果脉搏开始变弱\n或者变得细弱难辨 马上告诉我"}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel}})
	output, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	events := parseDialogues(t, string(output))
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	assertGlyphs(t, events[0].text, "如果脉搏开始变弱"+english)
	assertGlyphs(t, events[1].text, "或者变得细弱难辨 马上告诉我"+english)
	if events[0].end != events[1].start || events[0].end-events[0].start < minFallbackFragmentMS {
		t.Fatalf("source cut did not receive the minimum reading time: %+v", events)
	}
}

func TestChineseSourceBreakWithWordTimingSyncSplits(t *testing.T) {
	cfg := renderTestConfig()
	cfg.ZhChars = 20
	bundle := makeBundle(
		[]Cue{{ID: 1, Start: 380964, End: 384176, Display: "Tell me if it starts to weaken or get thready.", Plain: "Tell me if it starts to weaken\nor get thready."}},
		[]Cue{{ID: 1, Start: 380964, End: 384176, Display: "如果脉搏开始变弱 或者变得细弱难辨 马上告诉我", Plain: "如果脉搏开始变弱\n或者变得细弱难辨 马上告诉我"}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel}})
	// Word timings providing qualified cut >= 1.2s on both sides
	words := WordTimings{1: {
		{Text: "Tell", StartMS: 380964, EndMS: 381200},
		{Text: "me", StartMS: 381200, EndMS: 381400},
		{Text: "if", StartMS: 381400, EndMS: 381600},
		{Text: "it", StartMS: 381600, EndMS: 381800},
		{Text: "starts", StartMS: 381800, EndMS: 382200},
		{Text: "to", StartMS: 382200, EndMS: 382400},
		{Text: "weaken", StartMS: 382400, EndMS: 382700},
		{Text: "or", StartMS: 382800, EndMS: 383000},
		{Text: "get", StartMS: 383000, EndMS: 383300},
		{Text: "thready.", StartMS: 383300, EndMS: 384176},
	}}
	mapping = confirmAllSplits(t, bundle, mapping, cfg)
	output, err := RenderWithWordTimings(bundle, mapping, cfg, renderTestStyle(), 1920, 1080, words)
	if err != nil {
		t.Fatal(err)
	}
	events := parseDialogues(t, string(output))
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	assertGlyphs(t, events[0].text, "如果脉搏开始变弱Tell me if it starts to weaken")
	assertGlyphs(t, events[1].text, "或者变得细弱难辨 马上告诉我or get thready.")
}

func TestChineseSourceBreakIsNotForcedBelowTotalReadingTime(t *testing.T) {
	cfg := renderTestConfig()
	cfg.ZhChars = 20
	chinese := "如果脉搏开始变弱 或者变得细弱难辨 马上告诉我"
	bundle := makeBundle(
		[]Cue{{ID: 1, Start: 1000, End: 2500, Display: "Watch the pulse."}},
		[]Cue{{ID: 1, Start: 1000, End: 2500, Display: chinese, Plain: "如果脉搏开始变弱\n或者变得细弱难辨 马上告诉我"}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel}})
	output, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	events := parseDialogues(t, string(output))
	if len(events) != 1 {
		t.Fatalf("unreadably short source fragments were forced: %+v", events)
	}
	assertGlyphs(t, events[0].text, chinese+"Watch the pulse.")
}

func TestSpeakerWrappingKeepsOriginalLineBreaks(t *testing.T) {
	cfg := renderTestConfig()
	cfg.ZhChars = 12 // the flat line counts 15; each source line fits
	plain := "- 什么？\n- 一个亚历克斯、一个瑞安"
	bundle := makeBundle(
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "- What? - An Alex, a Ryan.", Plain: "- What?\n- An Alex, a Ryan.", MultiSpeaker: true}},
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "- 什么？ - 一个亚历克斯、一个瑞安", Plain: plain, MultiSpeaker: true}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel}})
	output, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	events := parseDialogues(t, string(output))
	if len(events) != 1 || !strings.Contains(events[0].text, strings.ReplaceAll(plain, "\n", `\N`)) {
		t.Fatalf("speaker line was mechanically cut instead of restoring the source break: %+v", events)
	}
}

func TestSplitChineseSourcesRollsBackFlashbackAfterGap(t *testing.T) {
	type span struct{ start, end, zh int }
	tests := []struct {
		name    string
		windows [][2]int
		zh      []Cue
		want    []span
	}{
		{
			// Recorded case: the first Chinese cue ends 40ms after the
			// English gap starts and the second starts 120ms after the gap.
			// The first Chinese line used to flash back for 120ms.
			name:    "cut just after a gap",
			windows: [][2]int{{2646770, 2648270}, {2649900, 2651980}},
			zh: []Cue{
				{ID: 452, Start: 2646940, End: 2648310, Display: "在这张照片里的男人"},
				{ID: 453, Start: 2650020, End: 2651900, Display: "除了四个以外 其他的都已确认死亡"},
			},
			want: []span{{2646770, 2648270, 452}, {2649900, 2651980, 453}},
		},
		{
			// A Chinese cue that runs well into the gap is an intentional
			// hold and keeps its source cut.
			name:    "intentional hold over a gap",
			windows: [][2]int{{1000, 3000}, {5000, 8000}},
			zh: []Cue{
				{ID: 1, Start: 1000, End: 4500, Display: "延伸入间隙中文"},
				{ID: 2, Start: 5900, End: 8000, Display: "后部第二句中文"},
			},
			want: []span{{1000, 3000, 1}, {5000, 5900, 1}, {5900, 8000, 2}},
		},
		{
			name:    "distant cut keeps source timing",
			windows: [][2]int{{0, 2000}, {2000, 4000}},
			zh: []Cue{
				{ID: 1, Start: 0, End: 1000, Display: "第一句话"},
				{ID: 2, Start: 1000, End: 4000, Display: "第二句话"},
			},
			want: []span{{0, 1000, 1}, {1000, 2000, 2}, {2000, 4000, 2}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var slices []block
			for i, w := range tt.windows {
				slices = append(slices, block{start: w[0], end: w[1], enIDs: []int{i + 1}, zhIDs: cueIDs(tt.zh)})
			}
			var got []span
			for _, b := range splitChineseSources(slices, tt.zh, nil, nil) {
				if len(b.zhIDs) != 1 {
					t.Fatalf("expected one Chinese cue per slice, got %v", b.zhIDs)
				}
				got = append(got, span{b.start, b.end, b.zhIDs[0]})
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("slices = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCountedLength(t *testing.T) {
	for in, want := range map[string]int{
		"你好，世界！":            4,
		"- A long - B line": 15,
		"Hello, world.":     11,
		"♪ la la ♪":         7,
		"$20 million":       10,
		"":                  0,
	} {
		if got := countedLength(in); got != want {
			t.Errorf("countedLength(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestLimitIndex(t *testing.T) {
	runes := []rune("你好，世界！再见")
	if got := limitIndex(runes, 4); string(runes[:got]) != "你好，世界！" {
		t.Fatalf("limitIndex cut %q, want punctuation kept with the last counted character", string(runes[:got]))
	}
	if got := limitIndex(runes, 10); got != len(runes) {
		t.Fatalf("limitIndex = %d, want whole text %d", got, len(runes))
	}
}

// A line break never leaves a space at either end of a wrapped line.
func TestWrapDropsSpacesAtLineBreaks(t *testing.T) {
	for _, lines := range [][]string{
		wrapBalancedZH("同意 - 而且阿尔-纳瓦兹改变了… - 女士 我们有线索", 20),
		wrapLine("一二三四五六七八九十 一二三四五", "zh", 10),
	} {
		if len(lines) < 2 {
			t.Fatalf("expected a wrapped line, got %q", lines)
		}
		for _, line := range lines {
			if line != strings.TrimSpace(line) {
				t.Fatalf("wrapped line keeps a space at a line break: %q in %q", line, lines)
			}
		}
	}
}
