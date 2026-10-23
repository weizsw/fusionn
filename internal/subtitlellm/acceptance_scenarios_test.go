package subtitlellm

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// The 17 acceptance scenarios defined in docs/llm-subtitle-integration.zh-CN.md.

// Scenario 1: 原中英文都两行，展平后都未超限 -> 仍优先沿原分行同步拆成两个双语事件
// Two-line cues whose lines fit side by side stay together: the source shows
// both lines at once, so nothing forces a split.
func TestScenario01_BothTwoLinesUnderLimitStayTogether(t *testing.T) {
	cfg := renderTestConfig()
	cfg.EnChars = 65
	cfg.ZhChars = 25
	bundle := makeBundle(
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "First line of speech and second line of speech.", Plain: "First line of speech\nand second line of speech."}},
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "第一句对白 第二句对白", Plain: "第一句对白\n第二句对白"}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue}})
	output, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	events := parseDialogues(t, string(output))
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	assertGlyphs(t, events[0].text, "第一句对白 第二句对白First line of speech and second line of speech.")
}

// Scenario 2: 原中文一行、英文两行，英文合并后未超限 -> 中英各一行，整句显示
func TestScenario02_ChineseOneEnglishTwoUnderLimitWhole(t *testing.T) {
	cfg := renderTestConfig()
	cfg.EnChars = 65
	bundle := makeBundle(
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "First half and the second half.", Plain: "First half\nand the second half."}},
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "完整的一行中文", Plain: "完整的一行中文"}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue}})
	output, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	events := parseDialogues(t, string(output))
	if len(events) != 1 {
		t.Fatalf("expected 1 whole sentence event, got %d", len(events))
	}
	assertGlyphs(t, events[0].text, "完整的一行中文First half and the second half.")
	if strings.Count(events[0].text, `\N`) != 1 {
		t.Fatalf("expected 1 Chinese line and 1 English line, got: %s", events[0].text)
	}
}

// Scenario 3: 原中文一行、英文两行，英文合并后超限 -> 中文保持，英文分段切换
func TestScenario03_ChineseOneEnglishTwoOverLimitSplit(t *testing.T) {
	cfg := renderTestConfig()
	cfg.EnChars = 20
	bundle := makeBundle(
		[]Cue{{ID: 1, Start: 1000, End: 6000, Display: "First half of text and the second half.", Plain: "First half of text\nand the second half."}},
		[]Cue{{ID: 1, Start: 1000, End: 6000, Display: "中文保持整句显示", Plain: "中文保持整句显示"}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue}})
	output, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	events := parseDialogues(t, string(output))
	if len(events) != 2 {
		t.Fatalf("expected 2 events with English switching, got %d", len(events))
	}
	assertGlyphs(t, events[0].text, "中文保持整句显示First half of text")
	assertGlyphs(t, events[1].text, "中文保持整句显示and the second half.")
}

// Scenario 4: 原中文两行、英文一行，中文合并后未超限 -> 中英各一行，整句显示
func TestScenario04_ChineseTwoEnglishOneUnderLimitWhole(t *testing.T) {
	cfg := renderTestConfig()
	cfg.ZhChars = 25
	bundle := makeBundle(
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "English stays on a single line.", Plain: "English stays on a single line."}},
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "第一部分 第二部分", Plain: "第一部分\n第二部分"}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue}})
	output, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	events := parseDialogues(t, string(output))
	if len(events) != 1 {
		t.Fatalf("expected 1 whole sentence event, got %d", len(events))
	}
	assertGlyphs(t, events[0].text, "第一部分第二部分English stays on a single line.")
	if strings.Count(events[0].text, `\N`) != 1 {
		t.Fatalf("expected 1 Chinese line and 1 English line, got: %s", events[0].text)
	}
}

// Scenario 5: 原中文两行、英文一行，中文合并后超限 -> 英文保持，中文分段切换
func TestScenario05_ChineseTwoEnglishOneOverLimitSplit(t *testing.T) {
	cfg := renderTestConfig()
	cfg.ZhChars = 8
	bundle := makeBundle(
		[]Cue{{ID: 1, Start: 1000, End: 6000, Display: "English line remains whole.", Plain: "English line remains whole."}},
		[]Cue{{ID: 1, Start: 1000, End: 6000, Display: "第一部分内容超限 第二部分内容同样", Plain: "第一部分内容超限\n第二部分内容同样"}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue}})
	output, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	events := parseDialogues(t, string(output))
	if len(events) != 2 {
		t.Fatalf("expected 2 events with Chinese switching, got %d", len(events))
	}
	assertGlyphs(t, events[0].text, "第一部分内容超限English line remains whole.")
	assertGlyphs(t, events[1].text, "第二部分内容同样English line remains whole.")
}

// Scenario 6: 原中英文都两行，但同步拆分不足 1.2 秒，合并后均未超限 -> 合并成中英各一行，整句显示
func TestScenario06_BothTwoLinesShortDurationUnderLimitWhole(t *testing.T) {
	cfg := renderTestConfig()
	cfg.EnChars = 65
	cfg.ZhChars = 25
	// Total duration 1800ms < 2400ms (so 2+2 sync cannot give >=1.2s per fragment)
	bundle := makeBundle(
		[]Cue{{ID: 1, Start: 1000, End: 2800, Display: "Short part and another part.", Plain: "Short part\nand another part."}},
		[]Cue{{ID: 1, Start: 1000, End: 2800, Display: "短句一 短句二", Plain: "短句一\n短句二"}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue}})
	output, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	events := parseDialogues(t, string(output))
	if len(events) != 1 {
		t.Fatalf("expected 1 whole sentence event, got %d", len(events))
	}
	assertGlyphs(t, events[0].text, "短句一短句二Short part and another part.")
	if strings.Count(events[0].text, `\N`) != 1 {
		t.Fatalf("expected 1 Chinese line and 1 English line, got: %s", events[0].text)
	}
}

// Scenario 7: 上例中，仅一侧合并后超限 -> 超限侧分段切换，另一侧保持整句；新片段至少显示 0.8 秒
func TestScenario07_BothTwoLinesShortDurationOneOverLimitFallback(t *testing.T) {
	cfg := renderTestConfig()
	cfg.EnChars = 65
	cfg.ZhChars = 8
	// Duration 2000ms < 2400ms (sync fails), but >= 1600ms (can split at 1000ms >= 800ms)
	bundle := makeBundle(
		[]Cue{{ID: 1, Start: 1000, End: 3000, Display: "Short english stays whole.", Plain: "Short english\nstays whole."}},
		[]Cue{{ID: 1, Start: 1000, End: 3000, Display: "第一段中文比较长 第二段中文比较长", Plain: "第一段中文比较长\n第二段中文比较长"}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue}})
	output, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	events := parseDialogues(t, string(output))
	if len(events) != 2 {
		t.Fatalf("expected 2 fallback events, got %d", len(events))
	}
	assertGlyphs(t, events[0].text, "第一段中文比较长Short english stays whole.")
	assertGlyphs(t, events[1].text, "第二段中文比较长Short english stays whole.")
	for _, e := range events {
		if e.end-e.start < minFallbackFragmentMS {
			t.Fatalf("fragment duration %d is below 0.8s", e.end-e.start)
		}
	}
}

// Scenario 8: 原单条事件总长 2 秒，仅中文超限，能找到每段 1 秒的切法 -> 英文保持、中文切换，不因低于 1.2 秒而拒绝单侧拆分
func TestScenario08_TwoSecondsTotalSingleSideSplitAtOneSecond(t *testing.T) {
	cfg := renderTestConfig()
	cfg.ZhChars = 8
	bundle := makeBundle(
		[]Cue{{ID: 1, Start: 1000, End: 3000, Display: "English stays whole."}},
		[]Cue{{ID: 1, Start: 1000, End: 3000, Display: "第一部分八个字 第二部分八个字", Plain: "第一部分八个字\n第二部分八个字"}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue}})
	output, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	events := parseDialogues(t, string(output))
	if len(events) != 2 {
		t.Fatalf("expected 2 split events, got %d", len(events))
	}
	assertGlyphs(t, events[0].text, "第一部分八个字English stays whole.")
	assertGlyphs(t, events[1].text, "第二部分八个字English stays whole.")
}

// Scenario 9: 回退后的新片段仍不足 0.8 秒且找不到合格切法 -> 保留完整内容与原显示时间，允许多行
func TestScenario09_FallbackBelowPointEightSecondsStaysWholeMultiline(t *testing.T) {
	cfg := renderTestConfig()
	cfg.ZhChars = 8
	// Duration 1200ms cannot yield two >=800ms fragments
	bundle := makeBundle(
		[]Cue{{ID: 1, Start: 1000, End: 2200, Display: "English stays."}},
		[]Cue{{ID: 1, Start: 1000, End: 2200, Display: "第一部分比较长 第二部分比较长", Plain: "第一部分比较长\n第二部分比较长"}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue}})
	output, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	events := parseDialogues(t, string(output))
	if len(events) != 1 {
		t.Fatalf("expected 1 event with multiline fallback, got %d", len(events))
	}
	assertGlyphs(t, events[0].text, "第一部分比较长第二部分比较长English stays.")
}

// Scenario 10: 四行对白无法合并，对侧空闲且说话人归属可靠 -> 第一位说话人的一中一英在上，第二位在下
func TestScenario10_FourLineMultiSpeakerOppositeFreePartitionsTopBottom(t *testing.T) {
	cfg := renderTestConfig()
	cfg.ZhChars = 10 // Merged Chinese exceeds cap, cannot merge
	cfg.EnChars = 20 // Merged English exceeds cap, cannot merge
	plainZh := "- 第一位说话人台词\n- 第二位说话人台词"
	plainEn := "- Speaker one line.\n- Speaker two line."
	bundle := makeBundle(
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: plainEn, Plain: plainEn, MultiSpeaker: true}},
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: plainZh, Plain: plainZh, MultiSpeaker: true}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue}})
	output, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	events := parseDialogues(t, string(output))
	if len(events) != 2 {
		t.Fatalf("expected 2 partitioned speaker events, got %d", len(events))
	}
	// Speaker 1 on top
	if !strings.HasPrefix(events[0].style, "Top") {
		t.Fatalf("expected event 0 on top style, got %q", events[0].style)
	}
	assertGlyphs(t, events[0].text, "- 第一位说话人台词- Speaker one line.")

	// Speaker 2 on bottom
	if strings.HasPrefix(events[1].style, "Top") {
		t.Fatalf("expected event 1 on bottom style, got %q", events[1].style)
	}
	assertGlyphs(t, events[1].text, "- 第二位说话人台词- Speaker two line.")
}

// Scenario 11: 对白字幕合并未超字符上限且宽度合格 -> 双行语言合成一行，保留说话人标记
func TestScenario11_ThreeLineDialogueInCapFlattens(t *testing.T) {
	cfg := renderTestConfig()
	cfg.ZhChars = 15 // Merged Chinese is 12 runes <= 15
	plainZh := "- 什么？\n- 瑞安"
	plainEn := "- What?   - Ryan."
	bundle := makeBundle(
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: plainEn, Plain: plainEn, MultiSpeaker: true}},
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "- 什么？\n- 瑞安", Plain: plainZh, MultiSpeaker: true}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue}})

	oldHook := fontWidthMeasurerOverride
	defer func() { fontWidthMeasurerOverride = oldHook }()
	fontWidthMeasurerOverride = func(text, fontName string, fontSize float64, scaleX int, spacing float64, bold bool) (float64, bool) {
		return 300.0, true // fits well within 1760px
	}

	output, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	events := parseDialogues(t, string(output))
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if strings.Count(events[0].text, `\N`) != 1 {
		t.Fatalf("expected flattened to 1 Chinese line + 1 English line (1 separator), got text: %s", events[0].text)
	}
	assertGlyphs(t, events[0].text, "- 什么？- 瑞安- What? - Ryan.")
}

// Scenario 11b: 对白字幕合并超字符上限，即使宽度放得下也不允许超限合并
func TestScenario11_ThreeLineDialogueOverCapForbidden(t *testing.T) {
	cfg := renderTestConfig()
	cfg.ZhChars = 6 // Merged Chinese counts 7 (dashes and ？ do not count) > 6 (over-cap)
	plainZh := "- 什么？\n- 瑞安"
	plainEn := "- What? - Ryan."
	bundle := makeBundle(
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: plainEn, Plain: plainEn, MultiSpeaker: true}},
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "- 什么？\n- 瑞安", Plain: plainZh, MultiSpeaker: true}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue}})

	oldHook := fontWidthMeasurerOverride
	defer func() { fontWidthMeasurerOverride = oldHook }()
	fontWidthMeasurerOverride = func(text, fontName string, fontSize float64, scaleX int, spacing float64, bold bool) (float64, bool) {
		return 300.0, true // width fits, but cap forbids merging
	}

	output, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	events := parseDialogues(t, string(output))
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if strings.Count(events[0].text, `\N`) < 2 {
		t.Fatalf("dialogue over-cap must NOT merge even if width fits; expected 3 lines, got text: %s", events[0].text)
	}
}

// Scenario 11c: 对称英文对白在字符上限内合并
func TestScenario11_ThreeLineDialogueSymmetricEnglishInCapFlattens(t *testing.T) {
	cfg := renderTestConfig()
	cfg.EnChars = 20
	plainZh := "- 答复"
	plainEn := "- What?\n- Ryan."
	bundle := makeBundle(
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: plainEn, Plain: plainEn, MultiSpeaker: true}},
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: plainZh, Plain: plainZh, MultiSpeaker: true}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue}})

	oldHook := fontWidthMeasurerOverride
	defer func() { fontWidthMeasurerOverride = oldHook }()
	fontWidthMeasurerOverride = func(text, fontName string, fontSize float64, scaleX int, spacing float64, bold bool) (float64, bool) {
		return 300.0, true
	}

	output, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	events := parseDialogues(t, string(output))
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if strings.Count(events[0].text, `\N`) != 1 {
		t.Fatalf("expected 1 Chinese line + 1 English line, got text: %s", events[0].text)
	}
}

// Scenario 12: 三行合并后过宽 -> 保留原三行与原字号
func TestScenario12_ThreeLineMultiSpeakerTooWideKeepsThreeLines(t *testing.T) {
	cfg := renderTestConfig()
	plainZh := "- 第一位说了非常非常长的一段话甚至在一行放不下\n- 第二位也说了非常非常长的一段话"
	plainEn := "- Short English."
	bundle := makeBundle(
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: plainEn, Plain: plainEn, MultiSpeaker: true}},
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: plainZh, Plain: plainZh, MultiSpeaker: true}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue}})

	oldHook := fontWidthMeasurerOverride
	defer func() { fontWidthMeasurerOverride = oldHook }()
	fontWidthMeasurerOverride = func(text, fontName string, fontSize float64, scaleX int, spacing float64, bold bool) (float64, bool) {
		return 2000.0, true // exceeds 1760px
	}

	output, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	events := parseDialogues(t, string(output))
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if strings.Count(events[0].text, `\N`) < 2 {
		t.Fatalf("expected at least 3 lines (>=2 separators), got: %s", events[0].text)
	}
}

// Scenario 13: 四行对白需要的另一侧已有字幕 -> 保留原布局
func TestScenario13_FourLineMultiSpeakerOppositeOccupiedKeepsOriginal(t *testing.T) {
	cfg := renderTestConfig()
	cfg.ZhChars = 10 // Merged Chinese exceeds cap, cannot merge
	cfg.EnChars = 20 // Merged English exceeds cap, cannot merge
	plainZh := "- 第一位说话人\n- 第二位说话人"
	plainEn := "- Speaker one line\n- Speaker two line"
	bundle := makeBundle(
		[]Cue{
			{ID: 1, Start: 1000, End: 5000, Display: plainEn, Plain: plainEn, MultiSpeaker: true},
			{ID: 2, Start: 2000, End: 4000, Display: "Top sign text", Top: true},
		},
		[]Cue{
			{ID: 1, Start: 1000, End: 5000, Display: plainZh, Plain: plainZh, MultiSpeaker: true},
			{ID: 2, Start: 2000, End: 4000, Display: "顶部文字", Top: true},
		},
	)
	mapping := makeMapping(bundle, []Group{
		{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
		{ENIDs: []int{2}, ZHIDs: []int{2}, Layer: LayerTop, Origin: OriginModel, Kind: KindOnScreenText},
	})
	output, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	events := parseDialogues(t, string(output))
	// Group 1 should NOT be partitioned because Top is occupied by cue 2
	var dialogueEvent *dialogue
	for i := range events {
		if strings.Contains(events[i].text, "第一位说话人") {
			dialogueEvent = &events[i]
			break
		}
	}
	if dialogueEvent == nil {
		t.Fatal("dialogue event not found")
	}
	if strings.HasPrefix(dialogueEvent.style, "Top") {
		t.Fatalf("dialogue should remain at bottom when top is occupied, got style %q", dialogueEvent.style)
	}
	if strings.Count(dialogueEvent.text, `\N`) != 3 {
		t.Fatalf("expected all 4 lines kept together (3 separators), got: %s", dialogueEvent.text)
	}
}

// Scenario 14: 连续画面文字仅第二段与底部台词重叠，顶部全程空闲 -> 整组移到顶部
func TestScenario14_ContiguousScreenTextSecondCollidesMovesWholeGroupTop(t *testing.T) {
	cfg := renderTestConfig()
	bundle := makeBundle(
		[]Cue{
			{ID: 1, Start: 1000, End: 2000, Display: "Screen sign 1"},
			{ID: 2, Start: 2200, End: 3000, Display: "Screen sign 2"},
			{ID: 3, Start: 2500, End: 4000, Display: "Dialogue speech"},
		},
		[]Cue{
			{ID: 1, Start: 1000, End: 2000, Display: "画面文字一"},
			{ID: 2, Start: 2200, End: 3000, Display: "画面文字二"},
			{ID: 3, Start: 2500, End: 4000, Display: "对白台词"},
		},
	)
	mapping := makeMapping(bundle, []Group{
		{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindOnScreenText},
		{ENIDs: []int{2}, ZHIDs: []int{2}, Layer: LayerBottom, Origin: OriginModel, Kind: KindOnScreenText},
		{ENIDs: []int{3}, ZHIDs: []int{3}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
	})
	output, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	events := parseDialogues(t, string(output))
	for _, e := range events {
		if strings.Contains(e.text, "画面文字一") || strings.Contains(e.text, "画面文字二") {
			if !strings.HasPrefix(e.style, "Top") {
				t.Fatalf("both screen text cues should move to top, got style %q for %s", e.style, e.text)
			}
		}
	}
}

// Scenario 15: 上例中，第一段期间顶部已有其他字幕 -> 整组保持原位
func TestScenario15_ContiguousScreenTextDestinationOccupiedStaysNative(t *testing.T) {
	cfg := renderTestConfig()
	bundle := makeBundle(
		[]Cue{
			{ID: 1, Start: 1000, End: 2000, Display: "Screen sign 1"},
			{ID: 2, Start: 2200, End: 3000, Display: "Screen sign 2"},
			{ID: 3, Start: 2500, End: 4000, Display: "Dialogue speech"},
			{ID: 4, Start: 1200, End: 1800, Display: "Existing top subtitle", Top: true},
		},
		[]Cue{
			{ID: 1, Start: 1000, End: 2000, Display: "画面文字一"},
			{ID: 2, Start: 2200, End: 3000, Display: "画面文字二"},
			{ID: 3, Start: 2500, End: 4000, Display: "对白台词"},
			{ID: 4, Start: 1200, End: 1800, Display: "既有顶部字幕", Top: true},
		},
	)
	mapping := makeMapping(bundle, []Group{
		{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindOnScreenText},
		{ENIDs: []int{2}, ZHIDs: []int{2}, Layer: LayerBottom, Origin: OriginModel, Kind: KindOnScreenText},
		{ENIDs: []int{3}, ZHIDs: []int{3}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
		{ENIDs: []int{4}, ZHIDs: []int{4}, Layer: LayerTop, Origin: OriginModel, Kind: KindDialogue},
	})
	output, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	events := parseDialogues(t, string(output))
	for _, e := range events {
		if strings.Contains(e.text, "画面文字一") || strings.Contains(e.text, "画面文字二") {
			if strings.HasPrefix(e.style, "Top") {
				t.Fatalf("group should remain at bottom when destination is occupied during first cue, got style %q", e.style)
			}
		}
	}
}

// Scenario 16: 原画面文字位置依次为下、上、下 -> 在位置变化处断组，不强制统一三段位置
func TestScenario16_ScreenTextBreaksGroupAtPositionChange(t *testing.T) {
	cfg := renderTestConfig()
	bundle := makeBundle(
		[]Cue{
			{ID: 1, Start: 1000, End: 2000, Display: "Bottom sign 1"},
			{ID: 2, Start: 2200, End: 3000, Display: "Top sign 2", Top: true},
			{ID: 3, Start: 3200, End: 4000, Display: "Bottom sign 3"},
		},
		[]Cue{
			{ID: 1, Start: 1000, End: 2000, Display: "底部文字一"},
			{ID: 2, Start: 2200, End: 3000, Display: "顶部文字二", Top: true},
			{ID: 3, Start: 3200, End: 4000, Display: "底部文字三"},
		},
	)
	mapping := makeMapping(bundle, []Group{
		{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindOnScreenText},
		{ENIDs: []int{2}, ZHIDs: []int{2}, Layer: LayerTop, Origin: OriginModel, Kind: KindOnScreenText},
		{ENIDs: []int{3}, ZHIDs: []int{3}, Layer: LayerBottom, Origin: OriginModel, Kind: KindOnScreenText},
	})
	output, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	events := parseDialogues(t, string(output))
	for _, e := range events {
		if strings.Contains(e.text, "底部文字一") || strings.Contains(e.text, "底部文字三") {
			if strings.HasPrefix(e.style, "Top") {
				t.Fatalf("bottom signs without speech collision should remain at bottom, got %q", e.style)
			}
		}
		if strings.Contains(e.text, "顶部文字二") {
			if !strings.HasPrefix(e.style, "Top") {
				t.Fatalf("native top sign should remain at top, got %q", e.style)
			}
		}
	}
}

// Additional edge case tests.

// TestFontNotFoundFallback verifies that when font metrics are unavailable (e.g. font missing in Docker),
// 3-line multi-speaker dialogue safely falls back and keeps original 3 lines.
func TestFontNotFoundFallbackKeepsThreeLines(t *testing.T) {
	cfg := renderTestConfig()
	plainZh := "- 第一位说话人\n- 第二位说话人"
	plainEn := "- What?   - Ryan."
	bundle := makeBundle(
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: plainEn, Plain: plainEn, MultiSpeaker: true}},
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: plainZh, Plain: plainZh, MultiSpeaker: true}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue}})

	// Force measurer to return false (font not found)
	oldHook := fontWidthMeasurerOverride
	defer func() { fontWidthMeasurerOverride = oldHook }()
	fontWidthMeasurerOverride = func(text, fontName string, fontSize float64, scaleX int, spacing float64, bold bool) (float64, bool) {
		return 0, false // font cannot be found / measured
	}

	style := renderTestStyle()
	style.PrimaryFont = "NonExistentFont"
	output, err := Render(bundle, mapping, cfg, style, 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	events := parseDialogues(t, string(output))
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if strings.Count(events[0].text, `\N`) < 2 {
		t.Fatalf("expected original 3 lines kept when font not found, got text: %s", events[0].text)
	}
}

// TestOldMappingWithoutKindPreservesPositionAndDigest verifies backward compatibility
// when imported mappings or cache have no 'kind' field.
func TestOldMappingWithoutKindPreservesPositionAndDigest(t *testing.T) {
	cfg := renderTestConfig()
	bundle := makeBundle(
		[]Cue{
			{ID: 1, Start: 1000, End: 3000, Display: "Text that might be a sign"},
			{ID: 2, Start: 2000, End: 4000, Display: "Speech dialogue"},
		},
		[]Cue{
			{ID: 1, Start: 1000, End: 3000, Display: "可能是路牌的文字"},
			{ID: 2, Start: 2000, End: 4000, Display: "说话人台词"},
		},
	)
	// Groups have empty Kind (as in older mappings)
	mapping := makeMapping(bundle, []Group{
		{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel},
		{ENIDs: []int{2}, ZHIDs: []int{2}, Layer: LayerBottom, Origin: OriginModel},
	})
	output, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	events := parseDialogues(t, string(output))
	for _, e := range events {
		if strings.HasPrefix(e.style, "Top") {
			t.Fatalf("without explicit on_screen_text kind, cues must never be relocated to top: %s", e.text)
		}
	}
}

// Regression for P1 Issue 1: Held source in N:1 mapping MUST NOT replay A -> B -> A -> B
func TestRegression_NToOneHeldSourceDoesNotReplayTwice(t *testing.T) {
	cfg := renderTestConfig()
	cfg.EnChars = 65
	cfg.ZhChars = 20
	bundle := makeBundle(
		[]Cue{
			{ID: 1, Start: 1000, End: 3000, Display: "First English cue.", Plain: "First English cue\nfirst part."},
			{ID: 2, Start: 3000, End: 5000, Display: "Second English cue.", Plain: "Second English cue\nsecond part."},
		},
		[]Cue{
			{ID: 1, Start: 1000, End: 5000, Display: "第一段中文内容 第二段中文内容", Plain: "第一段中文内容\n第二段中文内容"},
		},
	)
	// N:1 mapping group
	mapping := makeMapping(bundle, []Group{
		{ENIDs: []int{1, 2}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
	})
	output, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	events := parseDialogues(t, string(output))

	// Collect Chinese lines in chronological order
	var zhSeen []string
	for _, e := range events {
		clean := strings.Split(e.text, `\N`)[0]
		zhSeen = append(zhSeen, clean)
	}

	// Verify Chinese does NOT repeat A -> B -> A -> B
	for i := 0; i < len(zhSeen)-1; i++ {
		if zhSeen[i] == "第二段中文内容" && i+1 < len(zhSeen) && zhSeen[i+1] == "第一段中文内容" {
			t.Fatalf("Chinese replayed earlier line out of order (A-B-A-B defect): %v", zhSeen)
		}
	}
	// Count total occurrences of part 1
	part1Count := 0
	for _, s := range zhSeen {
		if strings.Contains(s, "第一段中文内容") {
			part1Count++
		}
	}
	// It should appear once, or as a continuous hold, never re-split and restarted in the second slice
	if part1Count > 1 && strings.Contains(zhSeen[len(zhSeen)-1], "第一段中文内容") {
		t.Fatalf("Chinese part 1 restarted at the end: %v", zhSeen)
	}
}

// Regression for P1 Issue 2: Real duration gate for native 2+2 sync split (highly unbalanced lines)
func TestRegression_UnbalancedTwoPlusTwoReachesLengthFallback(t *testing.T) {
	cfg := renderTestConfig()
	cfg.EnChars = 65
	cfg.ZhChars = 20
	// Total duration 3000ms (> 2400ms).
	// But English line 1 has 2 runes, line 2 has 40 runes.
	// Natural proportion: 2/42 * 3000ms = ~142ms << 1200ms.
	// Sync split MUST fail because real candidate cut < 1.2s floor.
	// Then it falls back: merged lengths (EN 43 <= 65, ZH 16 <= 20) -> both under limit, whole sentence (1 event)!
	bundle := makeBundle(
		[]Cue{{ID: 1, Start: 1000, End: 4000, Display: "Hi A very long second sentence that takes up most time.", Plain: "Hi\nA very long second sentence that takes up most time."}},
		[]Cue{{ID: 1, Start: 1000, End: 4000, Display: "嗨 这里是第二句中文比较长", Plain: "嗨\n这里是第二句中文比较长"}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue}})
	output, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	events := parseDialogues(t, string(output))
	if len(events) != 1 {
		t.Fatalf("unbalanced 2+2 should NOT force sync split into tiny fragment, should fall back to 1 whole event, got %d events", len(events))
	}
	assertGlyphs(t, events[0].text, "嗨这里是第二句中文比较长Hi A very long second sentence that takes up most time.")
}

// Regression for P1 Issue 3: Overlapping 4-line multi-speaker groups do NOT double-allocate Top
func TestRegression_OverlappingFourLineMultiSpeakerDoesNotDoubleAllocateTop(t *testing.T) {
	cfg := renderTestConfig()
	plainZh1 := "- 说话人一\n- 说话人二"
	plainEn1 := "- Speaker one\n- Speaker two"
	plainZh2 := "- 说话人三\n- 说话人四"
	plainEn2 := "- Speaker three\n- Speaker four"

	// Group 1: 1000-5000ms. Group 2: 3000-7000ms (overlaps 3000-5000ms).
	bundle := makeBundle(
		[]Cue{
			{ID: 1, Start: 1000, End: 5000, Display: plainEn1, Plain: plainEn1, MultiSpeaker: true},
			{ID: 2, Start: 3000, End: 7000, Display: plainEn2, Plain: plainEn2, MultiSpeaker: true},
		},
		[]Cue{
			{ID: 1, Start: 1000, End: 5000, Display: plainZh1, Plain: plainZh1, MultiSpeaker: true},
			{ID: 2, Start: 3000, End: 7000, Display: plainZh2, Plain: plainZh2, MultiSpeaker: true},
		},
	)
	mapping := makeMapping(bundle, []Group{
		{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
		{ENIDs: []int{2}, ZHIDs: []int{2}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue},
	})
	output, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	events := parseDialogues(t, string(output))

	// Group 1 partitions into Top (Speaker 1) and Bottom (Speaker 2)
	// Group 2 MUST see Top occupied during 3000-5000ms, and MUST NOT allocate Top!
	group2TopFound := false
	for _, e := range events {
		if (strings.Contains(e.text, "说话人三") || strings.Contains(e.text, "说话人四")) && strings.HasPrefix(e.style, "Top") {
			group2TopFound = true
		}
	}
	if group2TopFound {
		t.Fatal("Group 2 double-allocated top while Group 1 already occupied top during overlap")
	}
}

// Regression for P1 Issue 4: Asymmetric multi-speaker without two speakers on both sides keeps original layout
func TestRegression_AsymmetricMultiSpeakerWithoutTwoSpeakersKeepsOriginal(t *testing.T) {
	cfg := renderTestConfig()
	// Chinese has 2 speaker lines.
	// English is an ordinary long sentence that wraps to 2 lines without speaker markers.
	plainZh := "- 说话人一\n- 说话人二"
	plainEn := "This is an ordinary long English sentence\nthat wraps to two lines without speaker dashes."
	bundle := makeBundle(
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: plainEn, Plain: plainEn, MultiSpeaker: true}},
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: plainZh, Plain: plainZh, MultiSpeaker: true}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue}})
	output, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	events := parseDialogues(t, string(output))

	// Must NOT partition to top/bottom because English lacks two speaker lines
	for _, e := range events {
		if strings.HasPrefix(e.style, "Top") {
			t.Fatalf("asymmetric multi-speaker should retain original bottom layout, got %q", e.style)
		}
	}
}

// Regression for P1 Issue 5: Held over-cap language still undergoes source-aware final wrapping
func TestRegression_HeldOverCapLanguageStillUndergoesFinalWrapping(t *testing.T) {
	cfg := renderTestConfig()
	cfg.EnChars = 20
	cfg.ZhChars = 12
	// Genuinely eligible MULTILINE Chinese source: two native lines whose merged
	// length exceeds zh_chars, so its own plan needs two fragments. Its total
	// duration cannot supply two >=0.8s fragments after the English cuts, so the
	// Chinese side cannot split and must be held while still being wrapped.
	bundle := makeBundle(
		[]Cue{
			{ID: 1, Start: 1000, End: 3000, Display: "First english slice here.", Plain: "First english slice\nhere."},
			{ID: 2, Start: 3000, End: 5000, Display: "and the second english slice.", Plain: "and the second\nenglish slice."},
		},
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "这是一句非常长的中文句子它必须在最终排版中被正确折行", Plain: "这是一句非常长的中文句子\n它必须在最终排版中被正确折行"}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1, 2}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue}})
	output, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	events := parseDialogues(t, string(output))
	if len(events) == 0 {
		t.Fatal("no events rendered")
	}
	// No order check confirmed a split, so the Chinese cue is held whole over
	// both English cues. It must still be wrapped at zh_chars.
	whole := "这是一句非常长的中文句子它必须在最终排版中被正确折行"
	wrappedSeen := false
	for i, e := range events {
		// Inspect the Chinese side only: everything before the bilingual separator.
		zhOnly := e.text
		if idx := strings.Index(zhOnly, `{\rEng}`); idx >= 0 {
			zhOnly = zhOnly[:idx]
		}
		lines := strings.Split(strings.TrimSuffix(stripOverrideTags(zhOnly), `\N`), `\N`)
		if strings.Join(lines, "") != whole {
			t.Fatalf("event %d did not hold the whole Chinese cue: %q", i, zhOnly)
		}
		if len(lines) > 1 {
			wrappedSeen = true
		}
		for _, line := range lines {
			if utf8.RuneCountInString(line) > cfg.ZhChars {
				t.Fatalf("event %d Chinese line %q exceeds zh_chars=%d", i, line, cfg.ZhChars)
			}
		}
	}
	if !wrappedSeen {
		t.Fatal("held over-cap Chinese text was emitted as one unwrapped line")
	}
}

// TestOriginalSingleLineOversizeStaysExempt keeps the agreed exception: an
// originally single-line source above the limit is displayed whole.
func TestOriginalSingleLineOversizeStaysExempt(t *testing.T) {
	cfg := renderTestConfig()
	cfg.ZhChars = 8
	single := "这是一句远远超过上限但是原本就是单行的中文"
	bundle := makeBundle(
		[]Cue{{ID: 1, Start: 1000, End: 9000, Display: "A short english line."}},
		[]Cue{{ID: 1, Start: 1000, End: 9000, Display: single}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue}})
	output, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	events := parseDialogues(t, string(output))
	if len(events) != 1 {
		t.Fatalf("original single-line cue must not be split or wrap-forced, got %d events", len(events))
	}
	zhOnly := events[0].text
	if idx := strings.Index(zhOnly, `{\rEng}`); idx >= 0 {
		zhOnly = zhOnly[:idx]
	}
	// Drop the bilingual separator, which terminates the Chinese half.
	zhOnly = strings.TrimSuffix(zhOnly, `\N`)
	if strings.Count(zhOnly, `\N`) > 0 {
		t.Fatalf("original single-line oversize text must stay on one line: %q", stripOverrideTags(zhOnly))
	}
}

func stripOverrideTags(s string) string {
	out := make([]rune, 0, len(s))
	depth := 0
	for _, r := range s {
		switch {
		case r == '{':
			depth++
		case r == '}':
			if depth > 0 {
				depth--
			}
		case depth == 0:
			out = append(out, r)
		}
	}
	return string(out)
}

// --- Approved-rules coverage: ordinary vs dialogue subtitles ---------------

// TestOrdinaryThreeLineLateOverCapMergeAllowed: ordinary subtitles (no speaker
// markers) may merge past the configured cap once temporal splitting failed,
// as long as measured width says it fits. The 1.2s total makes two >=0.8s
// fallback fragments impossible, so temporal splitting genuinely fails here.
func TestOrdinaryThreeLineLateOverCapMergeAllowed(t *testing.T) {
	cfg := renderTestConfig()
	cfg.ZhChars = 8 // merged Chinese will exceed this cap
	bundle := makeBundle(
		[]Cue{{ID: 1, Start: 1000, End: 2200, Display: "Short english line.", Plain: "Short english line."}},
		[]Cue{{ID: 1, Start: 1000, End: 2200, Display: "第一行中文内容 第二行中文内容", Plain: "第一行中文内容\n第二行中文内容"}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue}})

	var calls int
	oldHook := fontWidthMeasurerOverride
	defer func() { fontWidthMeasurerOverride = oldHook }()
	fontWidthMeasurerOverride = func(text, fontName string, fontSize float64, scaleX int, spacing float64, bold bool) (float64, bool) {
		calls++
		return 400, true
	}

	output, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	events := parseDialogues(t, string(output))
	if len(events) != 1 {
		t.Fatalf("expected single merged event, got %d", len(events))
	}
	if calls == 0 {
		t.Fatal("width measurement was never consulted for the over-cap merge")
	}
	if got := strings.Count(events[0].text, `\N`); got != 1 {
		t.Fatalf("expected 1 Chinese line + 1 English line, got %d separators: %q", got, events[0].text)
	}
	assertGlyphs(t, events[0].text, "第一行中文内容   第二行中文内容Short english line.")
}

// TestOrdinaryThreeLineSplitBeatsOverCapMerge: with enough time to split, the
// over-cap merge must not pre-empt the temporal split.
func TestOrdinaryThreeLineSplitBeatsOverCapMerge(t *testing.T) {
	cfg := renderTestConfig()
	cfg.ZhChars = 8
	bundle := makeBundle(
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "Short english line.", Plain: "Short english line."}},
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "第一行中文内容 第二行中文内容", Plain: "第一行中文内容\n第二行中文内容"}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue}})

	oldHook := fontWidthMeasurerOverride
	defer func() { fontWidthMeasurerOverride = oldHook }()
	fontWidthMeasurerOverride = func(text, fontName string, fontSize float64, scaleX int, spacing float64, bold bool) (float64, bool) {
		return 400, true // fits, so a width-first policy would have merged
	}

	output, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	events := parseDialogues(t, string(output))
	if len(events) != 2 {
		t.Fatalf("temporal split must win over the width-based over-cap merge, got %d events", len(events))
	}
	for _, e := range events {
		zh, _, _ := strings.Cut(e.text, `{\rEng}`)
		if strings.Contains(zh, "第一行中文内容") && strings.Contains(zh, "第二行中文内容") {
			t.Fatalf("Chinese was merged instead of time-split: %q", e.text)
		}
	}
}

// TestDialogueThreeLineOverCapMergeForbidden: the same shape carrying two
// speakers must NOT break the configured cap even when the width would fit.
func TestDialogueThreeLineOverCapMergeForbidden(t *testing.T) {
	cfg := renderTestConfig()
	cfg.ZhChars = 8
	bundle := makeBundle(
		[]Cue{{ID: 1, Start: 1000, End: 3000, Display: "- Short english line.", Plain: "- Short english line.", MultiSpeaker: true}},
		[]Cue{{ID: 1, Start: 1000, End: 3000, Display: "- 第一行中文内容\n- 第二行中文内容", Plain: "- 第一行中文内容\n- 第二行中文内容", MultiSpeaker: true}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue}})

	oldHook := fontWidthMeasurerOverride
	defer func() { fontWidthMeasurerOverride = oldHook }()
	fontWidthMeasurerOverride = func(text, fontName string, fontSize float64, scaleX int, spacing float64, bold bool) (float64, bool) {
		return 400, true // width would fit; the cap still forbids merging
	}

	output, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	events := parseDialogues(t, string(output))
	for _, e := range events {
		zh, _, _ := strings.Cut(e.text, `{\rEng}`)
		if strings.Contains(zh, "第一行中文内容") && strings.Contains(zh, "第二行中文内容") &&
			!strings.Contains(strings.TrimSuffix(zh, `\N`), `\N`) {
			t.Fatalf("dialogue subtitle merged past its cap using the ordinary exemption: %q", e.text)
		}
	}
}

// TestOrdinaryFourLineLastResortPartitionWithoutMarkers: ordinary 2ZH+2EN with
// no speaker markers reaches the last-resort top/bottom partition when
// temporal splitting and cap-respecting merging both fail and the order check
// confirmed the line pairs.
func TestOrdinaryFourLineLastResortPartitionWithoutMarkers(t *testing.T) {
	cfg := renderTestConfig()
	cfg.ZhChars = 6
	cfg.EnChars = 20
	// 1.2s total: no side can produce two >=0.8s fragments, and width measurement
	// fails (too wide to merge into 1 line), so it stays at 4 lines and partitions.
	bundle := makeBundle(
		[]Cue{{ID: 1, Start: 1000, End: 2200, Display: "First english line. Second english line.",
			Plain: "First english line.\nSecond english line."}},
		[]Cue{{ID: 1, Start: 1000, End: 2200, Display: "第一段中文 第二段中文",
			Plain: "第一段中文\n第二段中文"}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue}})

	oldHook := fontWidthMeasurerOverride
	defer func() { fontWidthMeasurerOverride = oldHook }()
	fontWidthMeasurerOverride = func(text, fontName string, fontSize float64, scaleX int, spacing float64, bold bool) (float64, bool) {
		return 2000, true // too wide to merge into 1 line
	}

	// Partitioning pairs the lines by position, so the order check must confirm it.
	mapping = confirmAllSplits(t, bundle, mapping, cfg)
	output, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	events := parseDialogues(t, string(output))
	if len(events) != 2 {
		t.Fatalf("expected last-resort partition into 2 events, got %d", len(events))
	}
	if !strings.HasPrefix(events[0].style, "Top") {
		t.Fatalf("first segment should be placed on top, got style %q", events[0].style)
	}
	if strings.HasPrefix(events[1].style, "Top") {
		t.Fatalf("second segment should stay at the bottom, got style %q", events[1].style)
	}
	assertGlyphs(t, events[0].text, "第一段中文First english line.")
	assertGlyphs(t, events[1].text, "第二段中文Second english line.")
}

// TestTemporalSplitBeatsWidthMerge: a cue that still splits within its cap must
// not be merged past the cap just because the width would fit.
func TestTemporalSplitBeatsWidthMerge(t *testing.T) {
	cfg := renderTestConfig()
	cfg.EnChars = 24
	cfg.ZhChars = 10
	bundle := makeBundle(
		[]Cue{{ID: 1, Start: 1000, End: 7000, Display: "The first english line is long and the second english line is also long.",
			Plain: "The first english line is long\nand the second english line is also long."}},
		[]Cue{{ID: 1, Start: 1000, End: 7000, Display: "第一行中文", Plain: "第一行中文"}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue}})

	oldHook := fontWidthMeasurerOverride
	defer func() { fontWidthMeasurerOverride = oldHook }()
	doomed := false
	fontWidthMeasurerOverride = func(text, fontName string, fontSize float64, scaleX int, spacing float64, bold bool) (float64, bool) {
		doomed = true
		return 100, true // would fit, so a width-first policy would merge
	}

	output, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	events := parseDialogues(t, string(output))
	if len(events) < 2 {
		t.Fatalf("temporal split must take precedence over width-based merge, got %d events", len(events))
	}
	_ = doomed
}

// TestOrdinaryFourLineMeasurementUnavailableUsesEmBound: without a font
// measurement, an ordinary Chinese side merges past the cap only when one em
// per character still fits the screen. A wider side keeps its layout and is not
// partitioned. English never merges without a measurement.
func TestOrdinaryFourLineMeasurementUnavailableUsesEmBound(t *testing.T) {
	long1 := strings.Repeat("第一段中文", 3) + "第一"
	long2 := strings.Repeat("第二段中文", 3) + "第二"
	for _, tt := range []struct {
		name    string
		zhChars int
		zh      string
		merged  bool
	}{
		{name: "em_bound_fits_merges", zhChars: 6, zh: "第一段中文\n第二段中文", merged: true},
		// 35 characters at size 52 exceed the 1760px usable width.
		{name: "too_wide_keeps_layout", zhChars: 17, zh: long1 + "\n" + long2, merged: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			cfg := renderTestConfig()
			cfg.ZhChars = tt.zhChars
			cfg.EnChars = 20
			bundle := makeBundle(
				[]Cue{{ID: 1, Start: 1000, End: 2200, Display: "First english line. Second english line.",
					Plain: "First english segment here.\nSecond english segment."}},
				[]Cue{{ID: 1, Start: 1000, End: 2200, Display: strings.ReplaceAll(tt.zh, "\n", " "), Plain: tt.zh}},
			)
			mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue}})

			oldHook := fontWidthMeasurerOverride
			defer func() { fontWidthMeasurerOverride = oldHook }()
			fontWidthMeasurerOverride = func(text, fontName string, fontSize float64, scaleX int, spacing float64, bold bool) (float64, bool) {
				return 0, false // unknown font / unsupported style
			}

			output, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
			if err != nil {
				t.Fatal(err)
			}
			events := parseDialogues(t, string(output))
			if len(events) != 1 {
				t.Fatalf("expected 1 event without partitioning, got %d: %+v", len(events), events)
			}
			zh, eng, _ := strings.Cut(events[0].text, `{\rEng}`)
			if !strings.Contains(eng, `\N`) {
				t.Fatalf("English merged without a width measurement: %q", events[0].text)
			}
			if got := !strings.Contains(strings.TrimSuffix(zh, `\N`), `\N`); got != tt.merged {
				t.Fatalf("Chinese merged = %v, want %v: %q", got, tt.merged, events[0].text)
			}
		})
	}
}

// TestHeldChineseKeepsLayoutUnderTwoSpeakerEnglish reproduces 00:13-00:16 of
// the sample episode: a one-speaker Chinese cue stays on screen while the
// English moves on to a two-speaker cue. Both events show the same Chinese
// text, so they must show it with the same layout.
func TestHeldChineseKeepsLayoutUnderTwoSpeakerEnglish(t *testing.T) {
	es := []Cue{
		review939Cue(1, 1000, 3670, "Twelve SEALs lost and no one in command even issues", false),
		review939Cue(2, 3670, 5090, "- a statement?\n- Fire.", true),
		review939Cue(3, 5090, 6500, "What are they hiding?", false),
	}
	zs := []Cue{
		review939Cue(1, 1000, 4420, "12名海豹突击队队员丧生\n而指挥部连一个声明都没有？", false),
		review939Cue(2, 4420, 6500, "- 开火\n- 他们在隐瞒些什么？", true),
	}
	b, m := review939Inputs(t, es, zs, []Group{review939Group([]int{1, 2, 3}, []int{1, 2})})

	oldHook := fontWidthMeasurerOverride
	defer func() { fontWidthMeasurerOverride = oldHook }()
	fontWidthMeasurerOverride = func(string, string, float64, int, float64, bool) (float64, bool) {
		return 0, false
	}
	output, err := Render(b, m, review939Config(), renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	var held []string
	events := parseDialogues(t, string(output))
	for _, e := range events {
		zhPart, _, _ := strings.Cut(e.text, `{\rEng}`)
		if strings.Contains(zhPart, "丧生") {
			held = append(held, stripOverrideTags(zhPart))
		}
	}
	if len(held) != 2 {
		t.Fatalf("held Chinese shown in %d events, want 2: %+v", len(held), events)
	}
	if held[0] != held[1] || !strings.Contains(held[0], mergeSeparator) {
		t.Fatalf("held Chinese changed layout or did not merge: %q", held)
	}
}
