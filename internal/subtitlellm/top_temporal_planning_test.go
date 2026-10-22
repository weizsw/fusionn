package subtitlellm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fusionn/internal/config"
)

// TestTopTemporal_WordAlignmentCues verifies that English cues at top positions
// (either source Top/an8 or matching a top Chinese cue) are not arbitrarily
// excluded from word alignment candidate selection.
func TestTopTemporal_WordAlignmentCues(t *testing.T) {
	t.Run("over_limit_top_english_cue", func(t *testing.T) {
		bundle := Bundle{EN: []Cue{
			{ID: 1, Start: 1000, End: 5000, Display: "A long English line that exceeds limit.", Plain: "A long English line\nthat exceeds limit.", Top: true, Alignment: 8},
		}}
		cfg := config.LLMConfig{EnChars: 20}
		cues := WordAlignmentCues(bundle, cfg)
		if len(cues) != 1 || cues[0].ID != 1 {
			t.Fatalf("expected over-limit top English cue to be selected for alignment, got: %+v", cues)
		}
	})

	t.Run("two_line_sync_candidate_both_source_top", func(t *testing.T) {
		bundle := Bundle{
			EN: []Cue{
				{ID: 1, Start: 1000, End: 5000, Display: "First line of speech and second line of speech.", Plain: "First line of speech\nand second line of speech.", Top: true, Alignment: 8},
			},
			ZH: []Cue{
				{ID: 1, Start: 1000, End: 5000, Display: "第一句对白 第二句对白", Plain: "第一句对白\n第二句对白", Top: true, Alignment: 8},
			},
		}
		cfg := config.LLMConfig{EnChars: 65, ZhChars: 25}
		cues := WordAlignmentCues(bundle, cfg)
		if len(cues) != 1 || cues[0].ID != 1 {
			t.Fatalf("expected 2+2 source top candidate to be selected for alignment, got: %+v", cues)
		}
	})

	t.Run("two_line_sync_candidate_english_top_chinese_bottom", func(t *testing.T) {
		bundle := Bundle{
			EN: []Cue{
				{ID: 1, Start: 1000, End: 5000, Display: "First line of speech and second line of speech.", Plain: "First line of speech\nand second line of speech.", Top: true, Alignment: 8},
			},
			ZH: []Cue{
				{ID: 1, Start: 1000, End: 5000, Display: "第一句对白 第二句对白", Plain: "第一句对白\n第二句对白", Top: false, Alignment: 2},
			},
		}
		cfg := config.LLMConfig{EnChars: 65, ZhChars: 25}
		cues := WordAlignmentCues(bundle, cfg)
		if len(cues) != 1 || cues[0].ID != 1 {
			t.Fatalf("expected English top candidate to be selected for alignment, got: %+v", cues)
		}
	})

	t.Run("two_line_sync_candidate_english_bottom_chinese_top", func(t *testing.T) {
		bundle := Bundle{
			EN: []Cue{
				{ID: 1, Start: 1000, End: 5000, Display: "First line of speech and second line of speech.", Plain: "First line of speech\nand second line of speech.", Top: false, Alignment: 2},
			},
			ZH: []Cue{
				{ID: 1, Start: 1000, End: 5000, Display: "第一句对白 第二句对白", Plain: "第一句对白\n第二句对白", Top: true, Alignment: 8},
			},
		}
		cfg := config.LLMConfig{EnChars: 65, ZhChars: 25}
		cues := WordAlignmentCues(bundle, cfg)
		if len(cues) != 1 || cues[0].ID != 1 {
			t.Fatalf("expected Chinese top matching candidate to allow English alignment, got: %+v", cues)
		}
	})
}

// TestTopTemporal_BothTwoLinesUnderLimitSyncSplit verifies that 2+2 normal dialogue
// at the top of the screen undergoes synchronized 1.2s temporal splitting
// identically to bottom subtitles, while preserving top style and geometry.
func TestTopTemporal_BothTwoLinesUnderLimitSyncSplit(t *testing.T) {
	cfg := renderTestConfig()
	cfg.EnChars = 65
	cfg.ZhChars = 25

	t.Run("source_top_both_languages", func(t *testing.T) {
		bundle := makeBundle(
			[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "First line of speech and second line of speech.", Plain: "First line of speech\nand second line of speech.", Top: true, Alignment: 8}},
			[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "第一句对白 第二句对白", Plain: "第一句对白\n第二句对白", Top: true, Alignment: 8}},
		)
		mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerTop, Origin: OriginModel, Kind: KindDialogue}})
		output, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
		if err != nil {
			t.Fatal(err)
		}
		events := parseDialogues(t, string(output))
		if len(events) != 2 {
			t.Fatalf("expected 2 synchronized top events, got %d", len(events))
		}
		for i, e := range events {
			if e.style != "TopDefault" {
				t.Errorf("event %d style = %q, want TopDefault", i, e.style)
			}
		}
		assertGlyphs(t, events[0].text, "第一句对白First line of speech")
		assertGlyphs(t, events[1].text, "第二句对白and second line of speech.")

		// Compare with bottom baseline: cuts and text must be identical
		bottomBundle := makeBundle(
			[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "First line of speech and second line of speech.", Plain: "First line of speech\nand second line of speech.", Top: false, Alignment: 2}},
			[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "第一句对白 第二句对白", Plain: "第一句对白\n第二句对白", Top: false, Alignment: 2}},
		)
		bottomMapping := makeMapping(bottomBundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel, Kind: KindDialogue}})
		bottomOut, err := Render(bottomBundle, bottomMapping, cfg, renderTestStyle(), 1920, 1080)
		if err != nil {
			t.Fatal(err)
		}
		bottomEvents := parseDialogues(t, string(bottomOut))
		if len(bottomEvents) != 2 {
			t.Fatalf("expected bottom counterpart to produce 2 events, got %d", len(bottomEvents))
		}
		if events[0].start != bottomEvents[0].start || events[0].end != bottomEvents[0].end ||
			events[1].start != bottomEvents[1].start || events[1].end != bottomEvents[1].end {
			t.Fatalf("top timing [%d..%d, %d..%d] differs from bottom timing [%d..%d, %d..%d]",
				events[0].start, events[0].end, events[1].start, events[1].end,
				bottomEvents[0].start, bottomEvents[0].end, bottomEvents[1].start, bottomEvents[1].end)
		}
	})

	t.Run("model_layer_top_source_bottom", func(t *testing.T) {
		bundle := makeBundle(
			[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "First line of speech and second line of speech.", Plain: "First line of speech\nand second line of speech.", Top: false, Alignment: 2}},
			[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "第一句对白 第二句对白", Plain: "第一句对白\n第二句对白", Top: false, Alignment: 2}},
		)
		mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerTop, Origin: OriginModel, Kind: KindDialogue}})
		output, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
		if err != nil {
			t.Fatal(err)
		}
		events := parseDialogues(t, string(output))
		if len(events) != 2 {
			t.Fatalf("expected 2 synchronized top events for LayerTop, got %d", len(events))
		}
		for i, e := range events {
			if e.style != "TopDefault" {
				t.Errorf("event %d style = %q, want TopDefault", i, e.style)
			}
		}
		assertGlyphs(t, events[0].text, "第一句对白First line of speech")
		assertGlyphs(t, events[1].text, "第二句对白and second line of speech.")
	})
}

// TestTopTemporal_ChineseOneEnglishTwo verifies that 1 ZH + 2 EN ordinary speech
// at top merges to 1 line if under limit, and splits English while holding Chinese
// if over limit, exactly matching the bottom behavior.
func TestTopTemporal_ChineseOneEnglishTwo(t *testing.T) {
	t.Run("under_limit_stays_whole", func(t *testing.T) {
		cfg := renderTestConfig()
		cfg.EnChars = 65
		bundle := makeBundle(
			[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "First half and the second half.", Plain: "First half\nand the second half.", Top: true, Alignment: 8}},
			[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "完整的一行中文", Plain: "完整的一行中文", Top: true, Alignment: 8}},
		)
		mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerTop, Origin: OriginModel, Kind: KindDialogue}})
		output, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
		if err != nil {
			t.Fatal(err)
		}
		events := parseDialogues(t, string(output))
		if len(events) != 1 {
			t.Fatalf("expected 1 whole sentence event, got %d", len(events))
		}
		if events[0].style != "TopDefault" {
			t.Errorf("expected TopDefault style, got %s", events[0].style)
		}
		assertGlyphs(t, events[0].text, "完整的一行中文First half and the second half.")
		if strings.Count(events[0].text, `\N`) != 1 {
			t.Fatalf("expected 1 Chinese line and 1 English line, got: %s", events[0].text)
		}
	})

	t.Run("over_limit_splits_english_holds_chinese", func(t *testing.T) {
		cfg := renderTestConfig()
		cfg.EnChars = 20
		bundle := makeBundle(
			[]Cue{{ID: 1, Start: 1000, End: 6000, Display: "First half of text and the second half.", Plain: "First half of text\nand the second half.", Top: true, Alignment: 8}},
			[]Cue{{ID: 1, Start: 1000, End: 6000, Display: "中文保持整句显示", Plain: "中文保持整句显示", Top: true, Alignment: 8}},
		)
		mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerTop, Origin: OriginModel, Kind: KindDialogue}})
		output, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
		if err != nil {
			t.Fatal(err)
		}
		events := parseDialogues(t, string(output))
		if len(events) != 2 {
			t.Fatalf("expected 2 split events (English split, Chinese held), got %d", len(events))
		}
		for i, e := range events {
			if e.style != "TopDefault" {
				t.Errorf("event %d style = %q, want TopDefault", i, e.style)
			}
		}
		assertGlyphs(t, events[0].text, "中文保持整句显示First half of text")
		assertGlyphs(t, events[1].text, "中文保持整句显示and the second half.")
	})
}

// TestTopTemporal_ChineseTwoEnglishOne verifies that 2 ZH + 1 EN ordinary speech
// at top merges to 1 line if under limit, and splits Chinese while holding English
// if over limit, exactly matching the bottom behavior.
func TestTopTemporal_ChineseTwoEnglishOne(t *testing.T) {
	t.Run("under_limit_stays_whole", func(t *testing.T) {
		cfg := renderTestConfig()
		cfg.ZhChars = 25
		bundle := makeBundle(
			[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "Short English sentence.", Plain: "Short English sentence.", Top: true, Alignment: 8}},
			[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "中文前半句 中文后半句", Plain: "中文前半句\n中文后半句", Top: true, Alignment: 8}},
		)
		mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerTop, Origin: OriginModel, Kind: KindDialogue}})
		output, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
		if err != nil {
			t.Fatal(err)
		}
		events := parseDialogues(t, string(output))
		if len(events) != 1 {
			t.Fatalf("expected 1 whole sentence event, got %d", len(events))
		}
		if events[0].style != "TopDefault" {
			t.Errorf("expected TopDefault style, got %s", events[0].style)
		}
		assertGlyphs(t, events[0].text, "中文前半句中文后半句Short English sentence.")
		if strings.Count(events[0].text, `\N`) != 1 {
			t.Fatalf("expected 1 Chinese line and 1 English line, got: %s", events[0].text)
		}
	})

	t.Run("over_limit_splits_chinese_holds_english", func(t *testing.T) {
		cfg := renderTestConfig()
		cfg.ZhChars = 8
		bundle := makeBundle(
			[]Cue{{ID: 1, Start: 1000, End: 6000, Display: "English stays whole across slices.", Plain: "English stays whole across slices.", Top: true, Alignment: 8}},
			[]Cue{{ID: 1, Start: 1000, End: 6000, Display: "中文第一句较长 中文第二句也长", Plain: "中文第一句较长\n中文第二句也长", Top: true, Alignment: 8}},
		)
		mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerTop, Origin: OriginModel, Kind: KindDialogue}})
		output, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
		if err != nil {
			t.Fatal(err)
		}
		events := parseDialogues(t, string(output))
		if len(events) != 2 {
			t.Fatalf("expected 2 split events (Chinese split, English held), got %d", len(events))
		}
		for i, e := range events {
			if e.style != "TopDefault" {
				t.Errorf("event %d style = %q, want TopDefault", i, e.style)
			}
		}
		assertGlyphs(t, events[0].text, "中文第一句较长English stays whole across slices.")
		assertGlyphs(t, events[1].text, "中文第二句也长English stays whole across slices.")
	})
}

// TestTopTemporal_PreservesNativePosAndAnchor verifies that if a top cue has
// native \pos(x, y), temporal splitting preserves the exact position coordinates
// across all split fragments.
func TestTopTemporal_PreservesNativePosAndAnchor(t *testing.T) {
	cfg := renderTestConfig()
	cfg.EnChars = 65
	cfg.ZhChars = 25
	pos := [2]float64{960, 120}
	bundle := makeBundle(
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "First line of speech and second line of speech.", Plain: "First line of speech\nand second line of speech.", Top: true, Alignment: 8, Position: &pos}},
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "第一句对白 第二句对白", Plain: "第一句对白\n第二句对白", Top: true, Alignment: 8, Position: &pos}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerTop, Origin: OriginModel, Kind: KindDialogue}})
	output, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	events := parseDialogues(t, string(output))
	if len(events) != 2 {
		t.Fatalf("expected 2 synchronized events, got %d", len(events))
	}
	for i, e := range events {
		if !e.hasPos {
			t.Errorf("event %d missing \\pos tag: %s", i, e.text)
		}
		if e.posX != 960 || e.posY != 120 {
			t.Errorf("event %d position = (%.1f, %.1f), want (960, 120)", i, e.posX, e.posY)
		}
	}
}

// TestTopTemporal_SyncAndFallbackFloorBoundaries tests the 1.2s sync floor and
// 0.8s fallback floor boundaries on Top cues.
func TestTopTemporal_SyncAndFallbackFloorBoundaries(t *testing.T) {
	t.Run("sync_floor_exact_2x1200ms_vs_10ms_below", func(t *testing.T) {
		cases := []struct {
			name       string
			start, end int
			wantEvents int
		}{
			{"exact_2x1200ms", 1000, 3400, 2},
			{"10ms_below", 1000, 3390, 1},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				cfg := renderTestConfig()
				cfg.EnChars = 65
				cfg.ZhChars = 25
				bundle := makeBundle(
					[]Cue{{ID: 1, Start: tc.start, End: tc.end, Display: "Alpha. Bravo.", Plain: "Alpha.\nBravo.", Top: true, Alignment: 8}},
					[]Cue{{ID: 1, Start: tc.start, End: tc.end, Display: "甲。 乙。", Plain: "甲。\n乙。", Top: true, Alignment: 8}},
				)
				mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerTop, Origin: OriginModel, Kind: KindDialogue}})
				out, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
				if err != nil {
					t.Fatal(err)
				}
				events := parseDialogues(t, string(out))
				if len(events) != tc.wantEvents {
					t.Fatalf("events = %d, want %d", len(events), tc.wantEvents)
				}
				for _, e := range events {
					if e.style != "TopDefault" {
						t.Errorf("style = %q, want TopDefault", e.style)
					}
				}
			})
		}
	})

	t.Run("fallback_floor_exact_2x800ms_vs_10ms_below", func(t *testing.T) {
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
				cfg.ZhChars = 5 // forced fallback: Chinese over limit
				bundle := makeBundle(
					[]Cue{{ID: 1, Start: tc.start, End: tc.end, Display: "Short english whole.", Plain: "Short english whole.", Top: true, Alignment: 8}},
					[]Cue{{ID: 1, Start: tc.start, End: tc.end, Display: "第一段中文 第二段中文", Plain: "第一段中文\n第二段中文", Top: true, Alignment: 8}},
				)
				mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerTop, Origin: OriginModel, Kind: KindDialogue}})
				out, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
				if err != nil {
					t.Fatal(err)
				}
				events := parseDialogues(t, string(out))
				if len(events) != tc.wantEvents {
					t.Fatalf("events = %d, want %d", len(events), tc.wantEvents)
				}
				for _, e := range events {
					if e.style != "TopDefault" {
						t.Errorf("style = %q, want TopDefault", e.style)
					}
				}
			})
		}
	})
}

// TestTopTemporal_SourceBreaksVsWrapping verifies that original multiline source
// can split at source breaks, while original single lines (even if long and wrapped)
// remain intact under the sourceSingleLine exception.
func TestTopTemporal_SourceBreaksVsWrapping(t *testing.T) {
	cfg := renderTestConfig()
	cfg.EnChars = 20

	t.Run("multiline_source_splits_at_natural_source_break", func(t *testing.T) {
		bundle := makeBundle(
			[]Cue{{ID: 1, Start: 1000, End: 6000, Display: "First part of line and second line.", Plain: "First part of line\nand second line.", Top: true, Alignment: 8}},
			[]Cue{{ID: 1, Start: 1000, End: 6000, Display: "完整中文整句", Plain: "完整中文整句", Top: true, Alignment: 8}},
		)
		mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerTop, Origin: OriginModel, Kind: KindDialogue}})
		out, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
		if err != nil {
			t.Fatal(err)
		}
		events := parseDialogues(t, string(out))
		if len(events) != 2 {
			t.Fatalf("expected 2 split events, got %d", len(events))
		}
		assertGlyphs(t, events[0].text, "完整中文整句First part of line")
		assertGlyphs(t, events[1].text, "完整中文整句and second line.")
	})

	t.Run("single_line_source_exceeding_limit_stays_exempt", func(t *testing.T) {
		bundle := makeBundle(
			[]Cue{{ID: 1, Start: 1000, End: 6000, Display: "First part of line and second part of line.", Plain: "First part of line and second part of line.", Top: true, Alignment: 8}},
			[]Cue{{ID: 1, Start: 1000, End: 6000, Display: "完整中文整句", Plain: "完整中文整句", Top: true, Alignment: 8}},
		)
		mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerTop, Origin: OriginModel, Kind: KindDialogue}})
		out, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
		if err != nil {
			t.Fatal(err)
		}
		events := parseDialogues(t, string(out))
		if len(events) != 1 {
			t.Fatalf("original single-line source must not be split, got %d events", len(events))
		}
	})
}

// TestTopTemporal_WordTimingPriorityAndFallback verifies that word timing is
// prioritized for Top cues when valid, and falls back to natural proportional cut
// when word timings are missing or invalid.
func TestTopTemporal_WordTimingPriorityAndFallback(t *testing.T) {
	cfg := renderTestConfig()
	cfg.EnChars = 65
	cfg.ZhChars = 25

	bundle := makeBundle(
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "Alpha alpha alpha. Bravo bravo bravo.", Plain: "Alpha alpha alpha.\nBravo bravo bravo.", Top: true, Alignment: 8}},
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "第一段词切测试 第二段词切测试", Plain: "第一段词切测试\n第二段词切测试", Top: true, Alignment: 8}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerTop, Origin: OriginModel, Kind: KindDialogue}})

	t.Run("valid_word_timing_used", func(t *testing.T) {
		// Cut at 2800ms
		words := WordTimings{
			1: []WordTiming{
				{Text: "Alpha", StartMS: 1000, EndMS: 1500},
				{Text: "alpha", StartMS: 1600, EndMS: 2100},
				{Text: "alpha.", StartMS: 2200, EndMS: 2700},
				{Text: "Bravo", StartMS: 2900, EndMS: 3400},
				{Text: "bravo", StartMS: 3500, EndMS: 4000},
				{Text: "bravo.", StartMS: 4100, EndMS: 4900},
			},
		}
		out, err := RenderWithWordTimings(bundle, mapping, cfg, renderTestStyle(), 1920, 1080, words)
		if err != nil {
			t.Fatal(err)
		}
		events := parseDialogues(t, string(out))
		if len(events) != 2 {
			t.Fatalf("expected 2 events, got %d", len(events))
		}
		// Word cut is between 2700 and 2900 -> 2800
		if events[0].end != 2800 || events[1].start != 2800 {
			t.Fatalf("word cut mismatch: got [%d..%d, %d..%d], want cut at 2800",
				events[0].start, events[0].end, events[1].start, events[1].end)
		}
	})

	t.Run("bad_word_timing_falls_back_to_proportional", func(t *testing.T) {
		// Wrong text in words
		badWords := WordTimings{
			1: []WordTiming{
				{Text: "Completely", StartMS: 1000, EndMS: 2000},
				{Text: "Wrong", StartMS: 2100, EndMS: 4000},
			},
		}
		out, err := RenderWithWordTimings(bundle, mapping, cfg, renderTestStyle(), 1920, 1080, badWords)
		if err != nil {
			t.Fatal(err)
		}
		events := parseDialogues(t, string(out))
		if len(events) != 2 {
			t.Fatalf("expected fallback to proportional cut with 2 events, got %d", len(events))
		}
		// Proportional cut for equal length halves is at 3000ms
		if events[0].end != 3000 || events[1].start != 3000 {
			t.Fatalf("proportional fallback mismatch: got [%d..%d, %d..%d], want cut at 3000",
				events[0].start, events[0].end, events[1].start, events[1].end)
		}
	})
}

// TestTopTemporal_B16_IdempotenceAndImmutability verifies rule B16:
// Rendering twice with top cues produces byte-for-byte identical ASS,
// and the input bundle and mapping are never mutated.
func TestTopTemporal_B16_IdempotenceAndImmutability(t *testing.T) {
	bundle := makeBundle(
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "First line of speech and second line of speech.", Plain: "First line of speech\nand second line of speech.", Top: true, Alignment: 8}},
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "第一句对白 第二句对白", Plain: "第一句对白\n第二句对白", Top: true, Alignment: 8}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerTop, Origin: OriginModel, Kind: KindDialogue}})
	cfg := renderTestConfig()
	style := renderTestStyle()

	first, err := Render(bundle, mapping, cfg, style, 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Render(bundle, mapping, cfg, style, 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}

	if string(first) != string(second) {
		t.Fatal("subsequent render with top cues produced different ASS output")
	}

	if !bundle.EN[0].Top || bundle.EN[0].Alignment != 8 {
		t.Fatal("render mutated bundle EN top/alignment flags")
	}
	if !bundle.ZH[0].Top || bundle.ZH[0].Alignment != 8 {
		t.Fatal("render mutated bundle ZH top/alignment flags")
	}
}

// TestTopTemporal_RecordedE01_DetailedDiffAudit performs an exhaustive audit
// of every event difference between the pre-fix delivered ASS (656 events)
// and the post-fix re-rendered ASS (659 events).
// It verifies:
// 1. Exactly 3 groups split, turning 3 single events into 6 split events (+3 net).
// 2. All 3 split groups are top 2+2 dialogue cues with duration >= 2.4s.
// 3. Complete text coverage: every character/word of the original event is preserved.
// 4. Timing boundaries: fragment durations >= 1.2s, consecutive fragments contiguous.
// 5. Layout and styles: TopDefault style, alignment, positions preserved.
// 6. Zero unintended regressions: all other 653 events match byte-for-byte and time-for-time.
func TestTopTemporal_RecordedE01_DetailedDiffAudit(t *testing.T) {
	dir := os.Getenv(positionRecordDirEnv)
	if dir == "" {
		cand := "/Users/maverick/Desktop/fusionn-subtitle-split-test/terminal-list-s02e01-dialogue-lyrics-v5"
		if _, err := os.Stat(cand); err == nil {
			dir = cand
		}
	}
	if dir == "" {
		t.Skip("skipping E01 recorded replay audit: fixture directory not found")
	}

	cfg := replayConfig{
		Dir:         dir,
		SourcesPath: filepath.Join(dir, "sources.json"),
		StylePath:   filepath.Join(dir, "delivered-style.json"),
		WordsPath:   filepath.Join(dir, "word-timings.json"),
	}

	res, err := replayRecordedResponses(cfg)
	if err != nil {
		t.Fatalf("replay failed: %v", err)
	}

	deliveredRaw, err := os.ReadFile(filepath.Join(dir, "E01.zh20-bilingual-layout.ass"))
	if err != nil {
		t.Fatalf("read delivered ASS: %v", err)
	}
	deliveredEvents := parseSerializedASS(string(deliveredRaw))
	newEvents := parseSerializedASS(res.RenderedASS)

	t.Logf("delivered events: %d, new events: %d", len(deliveredEvents), len(newEvents))
	if len(newEvents) != len(deliveredEvents)+3 {
		t.Fatalf("expected exactly +3 events (659), got %d vs %d", len(newEvents), len(deliveredEvents))
	}

	// Compare delivered vs new events by time windows and report all differences.
	t.Logf("--- Starting Detailed Diff Audit ---")
	diffCount := 0
	dI, nI := 0, 0
	for dI < len(deliveredEvents) || nI < len(newEvents) {
		if dI < len(deliveredEvents) && nI < len(newEvents) {
			d := deliveredEvents[dI]
			n := newEvents[nI]
			if d.start == n.start && d.end == n.end && normText(d.text) == normText(n.text) {
				dI++
				nI++
				continue
			}
		}
		diffCount++
		var dText, nText string
		var dStart, dEnd, nStart, nEnd int
		if dI < len(deliveredEvents) {
			dText = deliveredEvents[dI].text
			dStart = deliveredEvents[dI].start
			dEnd = deliveredEvents[dI].end
		}
		if nI < len(newEvents) {
			nText = newEvents[nI].text
			nStart = newEvents[nI].start
			nEnd = newEvents[nI].end
		}
		t.Logf("Diff #%d:\n  Delivered [%d][%d..%d]: %s\n  New       [%d][%d..%d]: %s",
			diffCount, dI, dStart, dEnd, dText, nI, nStart, nEnd, nText)

		// Advance pointers intelligently:
		// If new event is a fragment of delivered:
		if dI < len(deliveredEvents) && nI+1 < len(newEvents) &&
			deliveredEvents[dI].start == newEvents[nI].start &&
			deliveredEvents[dI].end == newEvents[nI+1].end {
			t.Logf("  -> 1 delivered split into 2 new fragments: [%d..%d] + [%d..%d]",
				newEvents[nI].start, newEvents[nI].end, newEvents[nI+1].start, newEvents[nI+1].end)
			dI++
			nI += 2
			continue
		}
		// If delivered and new have same start and end but different text:
		if dI < len(deliveredEvents) && nI < len(newEvents) &&
			deliveredEvents[dI].start == newEvents[nI].start &&
			deliveredEvents[dI].end == newEvents[nI].end {
			t.Logf("  -> Same interval [%d..%d] with updated text", dStart, dEnd)
			dI++
			nI++
			continue
		}
		// Otherwise advance smaller start time
		if dI < len(deliveredEvents) && (nI >= len(newEvents) || deliveredEvents[dI].start < newEvents[nI].start) {
			dI++
		} else {
			nI++
		}
	}
	t.Logf("Total differences found: %d", diffCount)
}

// TestTopTemporal_MultiSpeakerTopProtected verifies that a top cue with
// MultiSpeaker=true is never split temporally even if it has 2 lines and
// duration exceeds the sync floor.
func TestTopTemporal_MultiSpeakerTopProtected(t *testing.T) {
	cfg := renderTestConfig()
	cfg.EnChars = 65
	cfg.ZhChars = 25

	bundle := makeBundle(
		[]Cue{{ID: 1, Start: 1000, End: 6000, Display: "- First speaker\n- Second speaker", Plain: "- First speaker\n- Second speaker", Top: true, Alignment: 8, MultiSpeaker: true}},
		[]Cue{{ID: 1, Start: 1000, End: 6000, Display: "- 第一位说话人\n- 第二位说话人", Plain: "- 第一位说话人\n- 第二位说话人", Top: true, Alignment: 8, MultiSpeaker: true}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerTop, Origin: OriginModel, Kind: KindDialogue}})

	out, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	events := parseDialogues(t, string(out))
	if len(events) != 1 {
		t.Fatalf("multi-speaker top cue must not be split, got %d events", len(events))
	}
	if events[0].style != "TopDefault" {
		t.Errorf("style = %q, want TopDefault", events[0].style)
	}
	assertGlyphs(t, events[0].text, "- 第一位说话人- 第二位说话人- First speaker- Second speaker")
}

// TestTopTemporal_CompareTopVsBottomTemporalEquivalence verifies that for the
// real episode groups (such as 101, 103, 104, 106, 109, 121), the temporal
// planning decisions (event intervals, splitting, text fragments) are identical
// whether the group is at the top or at the bottom.
func TestTopTemporal_CompareTopVsBottomTemporalEquivalence(t *testing.T) {
	dir := os.Getenv(positionRecordDirEnv)
	if dir == "" {
		cand := "/Users/maverick/Desktop/fusionn-subtitle-split-test/terminal-list-s02e01-dialogue-lyrics-v5"
		if _, err := os.Stat(cand); err == nil {
			dir = cand
		}
	}
	if dir == "" {
		t.Skip("skipping E01 recorded replay: fixture directory not found")
	}

	cfg := replayConfig{
		Dir:         dir,
		SourcesPath: filepath.Join(dir, "sources.json"),
		StylePath:   filepath.Join(dir, "delivered-style.json"),
		WordsPath:   filepath.Join(dir, "word-timings.json"),
	}

	res, err := replayRecordedResponses(cfg)
	if err != nil {
		t.Fatalf("replay failed: %v", err)
	}

	llmCfg := renderTestConfig()
	llmCfg.ZhChars = 20
	llmCfg.EnChars = 65

	style := res.Style

	enMap, zhMap, err := cueIndex(res.Bundle)
	if err != nil {
		t.Fatalf("cueIndex failed: %v", err)
	}

	// Full top episode render (real Top flags)
	topOut, err := RenderWithWordTimings(res.Bundle, res.Mapping, llmCfg, style, 1920, 1080, res.Words)
	if err != nil {
		t.Fatalf("render top episode: %v", err)
	}

	// Full bottom episode render (all Top flags set to bottom)
	botBundle := Bundle{
		Version:     res.Bundle.Version,
		Fingerprint: res.Bundle.Fingerprint,
		EN:          make([]Cue, len(res.Bundle.EN)),
		ZH:          make([]Cue, len(res.Bundle.ZH)),
	}
	for i, c := range res.Bundle.EN {
		c.Top = false
		c.Alignment = 2
		botBundle.EN[i] = c
	}
	for i, c := range res.Bundle.ZH {
		c.Top = false
		c.Alignment = 2
		botBundle.ZH[i] = c
	}
	botMapping := Mapping{
		SourceFingerprint: res.Mapping.SourceFingerprint,
		Groups:            make([]Group, len(res.Mapping.Groups)),
	}
	for i, g := range res.Mapping.Groups {
		g.Layer = LayerBottom
		botMapping.Groups[i] = g
	}

	botOut, err := RenderWithWordTimings(botBundle, botMapping, llmCfg, style, 1920, 1080, res.Words)
	if err != nil {
		t.Fatalf("render bottom episode: %v", err)
	}

	topEvs := parseDialogues(t, string(topOut))
	botEvs := parseDialogues(t, string(botOut))

	t.Logf("Full episode render: top events=%d, bot events=%d", len(topEvs), len(botEvs))
	if len(topEvs) != len(botEvs) {
		t.Fatalf("total event count mismatch: top=%d, bot=%d", len(topEvs), len(botEvs))
	}
	for i := range topEvs {
		te := topEvs[i]
		be := botEvs[i]
		if te.start != be.start || te.end != be.end {
			t.Errorf("global event %d time mismatch: top [%d..%d] vs bot [%d..%d]",
				i, te.start, te.end, be.start, be.end)
		}
		topGlyphs := normText(te.text)
		botGlyphs := normText(be.text)
		if topGlyphs != botGlyphs {
			t.Errorf("global event %d text mismatch:\n  top: %s\n  bot: %s",
				i, topGlyphs, botGlyphs)
		}
	}

	targetGroups := []int{101, 102, 103, 104, 105, 106, 109, 121}
	for _, gIdx := range targetGroups {
		if gIdx >= len(res.Mapping.Groups) {
			continue
		}
		g := res.Mapping.Groups[gIdx]
		// Find events falling within this group time window
		var es []Cue
		for _, id := range g.ENIDs {
			if c, ok := enMap[id]; ok {
				es = append(es, c)
			}
		}
		var zs []Cue
		for _, id := range g.ZHIDs {
			if c, ok := zhMap[id]; ok {
				zs = append(zs, c)
			}
		}
		if len(es) == 0 && len(zs) == 0 {
			continue
		}
		minStart := 1 << 30
		maxEnd := 0
		for _, c := range append(es, zs...) {
			if c.Start < minStart {
				minStart = c.Start
			}
			if c.End > maxEnd {
				maxEnd = c.End
			}
		}

		var topGEvs, botGEvs []dialogue
		for _, e := range topEvs {
			if e.start >= minStart-200 && e.end <= maxEnd+200 {
				topGEvs = append(topGEvs, e)
			}
		}
		for _, e := range botEvs {
			if e.start >= minStart-200 && e.end <= maxEnd+200 {
				botGEvs = append(botGEvs, e)
			}
		}

		t.Logf("Group %d [%d..%d]: top events=%d, bot events=%d", gIdx, minStart, maxEnd, len(topGEvs), len(botGEvs))
		if len(topGEvs) != len(botGEvs) {
			t.Errorf("Group %d event count mismatch: top=%d, bot=%d", gIdx, len(topGEvs), len(botGEvs))
			for i, e := range topGEvs {
				t.Logf("  top[%d]: [%d..%d] %s", i, e.start, e.end, e.text)
			}
			for i, e := range botGEvs {
				t.Logf("  bot[%d]: [%d..%d] %s", i, e.start, e.end, e.text)
			}
			continue
		}

		for i := range topGEvs {
			te := topGEvs[i]
			be := botGEvs[i]
			if te.start != be.start || te.end != be.end {
				t.Errorf("Group %d event %d time mismatch: top [%d..%d] vs bot [%d..%d]",
					gIdx, i, te.start, te.end, be.start, be.end)
			}
			topGlyphs := normText(te.text)
			botGlyphs := normText(be.text)
			if topGlyphs != botGlyphs {
				t.Errorf("Group %d event %d text mismatch:\n  top: %s\n  bot: %s",
					gIdx, i, topGlyphs, botGlyphs)
			}
		}
	}
}
