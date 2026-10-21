package subtitlellm

import (
	"strings"
	"testing"
)

// The 17 acceptance scenarios defined in docs/llm-subtitle-integration.zh-CN.md.

// Scenario 1: 原中英文都两行，展平后都未超限 -> 仍优先沿原分行同步拆成两个双语事件
func TestScenario01_BothTwoLinesUnderLimitSyncSplit(t *testing.T) {
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
	if len(events) != 2 {
		t.Fatalf("expected 2 synchronized events, got %d", len(events))
	}
	assertGlyphs(t, events[0].text, "第一句对白First line of speech")
	assertGlyphs(t, events[1].text, "第二句对白and second line of speech.")
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

// Scenario 10: 四行双人对白，另一侧空闲 -> 第一位说话人的一中一英在上，第二位在下
func TestScenario10_FourLineMultiSpeakerOppositeFreePartitionsTopBottom(t *testing.T) {
	cfg := renderTestConfig()
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

// Scenario 11: 三行双人对白，合并后可放一行 -> 双行语言合成一行，允许超过该语言配置的字符上限
func TestScenario11_ThreeLineMultiSpeakerFitsWidthFlattensToOneLine(t *testing.T) {
	cfg := renderTestConfig()
	cfg.ZhChars = 10 // Merged Chinese will exceed 10 chars, but fits width
	plainZh := "- 什么？\n- 瑞安"
	plainEn := "- What?   - Ryan."
	bundle := makeBundle(
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: plainEn, Plain: plainEn, MultiSpeaker: true}},
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "- 什么？\n- 瑞安", Plain: plainZh, MultiSpeaker: true}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue}})

	// Override font measurer to confirm it fits width
	oldHook := fontWidthMeasurerOverride
	defer func() { fontWidthMeasurerOverride = oldHook }()
	fontWidthMeasurerOverride = func(text string, fontName string, fontSize float64, scaleX int, spacing float64) (float64, bool) {
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
	fontWidthMeasurerOverride = func(text string, fontName string, fontSize float64, scaleX int, spacing float64) (float64, bool) {
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
	plainZh := "- 第一位说话人\n- 第二位说话人"
	plainEn := "- Speaker one\n- Speaker two"
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
	fontWidthMeasurerOverride = func(text string, fontName string, fontSize float64, scaleX int, spacing float64) (float64, bool) {
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

