package subtitlellm

import (
	"encoding/json"
	"fmt"
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
// native \pos(x, y) and alignment, temporal splitting preserves the exact
// position coordinates and alignment tags across all split fragments.
func TestTopTemporal_PreservesNativePosAndAnchor(t *testing.T) {
	t.Run("an8_default_top_with_pos", func(t *testing.T) {
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
			if e.style != "TopDefault" {
				t.Errorf("event %d style = %q, want TopDefault", i, e.style)
			}
			if !e.hasPos {
				t.Errorf("event %d missing \\pos tag: %s", i, e.text)
			}
			if e.posX != 960 || e.posY != 120 {
				t.Errorf("event %d position = (%.1f, %.1f), want (960, 120)", i, e.posX, e.posY)
			}
		}
	})

	t.Run("an7_top_left_with_pos_and_an_tag", func(t *testing.T) {
		cfg := renderTestConfig()
		cfg.EnChars = 65
		cfg.ZhChars = 25
		pos := [2]float64{200, 100}
		bundle := makeBundle(
			[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "First line of speech and second line of speech.", Plain: "First line of speech\nand second line of speech.", Top: true, Alignment: 7, Position: &pos}},
			[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "第一句对白 第二句对白", Plain: "第一句对白\n第二句对白", Top: true, Alignment: 7, Position: &pos}},
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
			if e.posX != 200 || e.posY != 100 {
				t.Errorf("event %d position = (%.1f, %.1f), want (200, 100)", i, e.posX, e.posY)
			}
			if e.an != 7 {
				t.Errorf("event %d an = %d, want 7 (\\an7)", i, e.an)
			}
		}
	})
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

// TestTopTemporal_SharedCutsBoundaries locks the sharedCuts contract that the
// gapped-source fix relies on.
func TestTopTemporal_SharedCutsBoundaries(t *testing.T) {
	pc := func(start, end int) textPiece { return textPiece{start: start, end: end, text: "x"} }

	t.Run("contiguous_english_requires_matching_outer_bounds", func(t *testing.T) {
		eng := []textPiece{pc(1000, 2000), pc(2000, 3000)}
		if cuts := sharedCuts(eng, 2, 1000, 3000); len(cuts) != 1 || cuts[0] != 2000 {
			t.Fatalf("matching outer bounds: cuts = %v, want [2000]", cuts)
		}
		if cuts := sharedCuts(eng, 2, 900, 3000); cuts != nil {
			t.Fatalf("mismatched start bound must be rejected, got %v", cuts)
		}
		if cuts := sharedCuts(eng, 2, 1000, 3100); cuts != nil {
			t.Fatalf("mismatched end bound must be rejected, got %v", cuts)
		}
	})

	t.Run("real_gap_allows_existing_source_boundary", func(t *testing.T) {
		eng := []textPiece{pc(1000, 2000), pc(2500, 3500)}
		if cuts := sharedCuts(eng, 2, 900, 3500); len(cuts) != 1 || cuts[0] != 2500 {
			t.Fatalf("gapped slices: cuts = %v, want [2500]", cuts)
		}
	})

	t.Run("overlap_is_rejected", func(t *testing.T) {
		eng := []textPiece{pc(1000, 2600), pc(2500, 3500)}
		if cuts := sharedCuts(eng, 2, 1000, 3500); cuts != nil {
			t.Fatalf("overlapping slices must be rejected, got %v", cuts)
		}
	})

	t.Run("count_mismatch_is_rejected", func(t *testing.T) {
		eng := []textPiece{pc(1000, 2000), pc(2500, 3500)}
		if cuts := sharedCuts(eng, 3, 1000, 3500); cuts != nil {
			t.Fatalf("count mismatch must be rejected, got %v", cuts)
		}
	})

	t.Run("cut_outside_plan_range_is_rejected", func(t *testing.T) {
		eng := []textPiece{pc(1000, 2000), pc(2500, 3500)}
		if cuts := sharedCuts(eng, 2, 2600, 3500); cuts != nil {
			t.Fatalf("cut before plan start must be rejected, got %v", cuts)
		}
		if cuts := sharedCuts(eng, 2, 1000, 2400); cuts != nil {
			t.Fatalf("cut after plan end must be rejected, got %v", cuts)
		}
	})
}

// TestTopTemporal_TwoEnglishOneChineseSharedGap reproduces the real group-119
// source cues separated by a real gap, and one Chinese cue whose Plain has two
// source lines that merge over the configured limit while each line fits.
//
// The Chinese planning must reuse the existing ordered, non-overlapping English
// source boundaries (including across a real gap) so each Chinese source line
// follows its English counterpart, and the first line must NOT replay inside the
// second English window.
func TestTopTemporal_TwoEnglishOneChineseSharedGap(t *testing.T) {
	cfg := renderTestConfig()
	cfg.EnChars = 65
	cfg.ZhChars = 20

	// Two English cues with a 1084ms real source gap, mirroring EN133/EN134.
	const (
		en1Start, en1End = 559476, 561395
		en2Start, en2End = 562479, 565106
		zhStart, zhEnd   = 559434, 564355
	)
	// Two Chinese source lines: 14 + 7 runes, merged 22 > ZhChars(20),
	// each line fits the limit.
	bundle := makeBundle(
		[]Cue{
			{ID: 1, Start: en1Start, End: en1End, Display: "Not going home at 1700. He is home.", Plain: "Not going home at 1700. He is home.", Top: true, Alignment: 8},
			{ID: 2, Start: en2Start, End: en2End, Display: "He only cares about the cause.", Plain: "He only cares about the cause.", Top: true, Alignment: 8},
		},
		[]Cue{{ID: 1, Start: zhStart, End: zhEnd, Display: "不在乎17点回家 他四处为家 他只关心大业”", Plain: "不在乎17点回家 他四处为家\n他只关心大业”", Top: true, Alignment: 8}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1, 2}, ZHIDs: []int{1}, Layer: LayerTop, Origin: OriginModel, Kind: KindDialogue}})

	out, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	events := parseDialogues(t, string(out))

	// The first Chinese source line must never be displayed inside the second
	// English window. Enumerate every event that carries the first line's glyphs.
	const firstLine = "不在乎17点回家 他四处为家"
	const secondLine = "他只关心大业”"
	var firstLineEvents, secondLineEvents []dialogue
	for _, e := range events {
		if e.style != "TopDefault" {
			continue
		}
		if strings.Contains(e.text, firstLine) {
			firstLineEvents = append(firstLineEvents, e)
		}
		if strings.Contains(e.text, secondLine) {
			secondLineEvents = append(secondLineEvents, e)
		}
	}

	for i, e := range firstLineEvents {
		if e.start >= en2Start {
			t.Errorf("first Chinese line replays inside the second English window: ev[%d] [%d..%d] %s",
				i, e.start, e.end, e.text)
		}
	}
	for i, e := range secondLineEvents {
		if e.end <= en1End {
			t.Errorf("second Chinese line appears before its English counterpart: ev[%d] [%d..%d] %s",
				i, e.start, e.end, e.text)
		}
	}

	// Each Chinese source line must be continuously visible for at least the
	// fallback floor, with no unexplained flash.
	firstTotal, secondTotal := 0, 0
	for _, e := range firstLineEvents {
		firstTotal += e.end - e.start
	}
	for _, e := range secondLineEvents {
		secondTotal += e.end - e.start
	}
	if firstTotal < minFallbackFragmentMS {
		t.Errorf("first Chinese line total visibility %dms < floor %dms", firstTotal, minFallbackFragmentMS)
	}
	if secondTotal < minFallbackFragmentMS {
		t.Errorf("second Chinese line total visibility %dms < floor %dms", secondTotal, minFallbackFragmentMS)
	}

	// The English source gap must stay empty: no top event may claim it.
	for _, e := range events {
		if e.style != "TopDefault" {
			continue
		}
		if e.start >= en1End && e.end <= en2Start {
			t.Errorf("top event fills the real English source gap: [%d..%d] %s", e.start, e.end, e.text)
		}
	}
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
// and the input bundle and mapping are never mutated (verified via deep JSON comparison).
func TestTopTemporal_B16_IdempotenceAndImmutability(t *testing.T) {
	bundle := makeBundle(
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "First line of speech and second line of speech.", Plain: "First line of speech\nand second line of speech.", Top: true, Alignment: 8}},
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "第一句对白 第二句对白", Plain: "第一句对白\n第二句对白", Top: true, Alignment: 8}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerTop, Origin: OriginModel, Kind: KindDialogue}})
	cfg := renderTestConfig()
	style := renderTestStyle()

	beforeBundleJSON, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	beforeMappingJSON, err := json.Marshal(mapping)
	if err != nil {
		t.Fatal(err)
	}

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

	afterBundleJSON, err := json.Marshal(bundle)
	if err != nil {
		t.Fatal(err)
	}
	afterMappingJSON, err := json.Marshal(mapping)
	if err != nil {
		t.Fatal(err)
	}

	if string(beforeBundleJSON) != string(afterBundleJSON) {
		t.Fatalf("render mutated input Bundle:\n  before: %s\n  after:  %s", beforeBundleJSON, afterBundleJSON)
	}
	if string(beforeMappingJSON) != string(afterMappingJSON) {
		t.Fatalf("render mutated input Mapping:\n  before: %s\n  after:  %s", beforeMappingJSON, afterMappingJSON)
	}
}

// TestTopTemporal_RecordedE01_DetailedDiffAudit audits the OLD stage snapshot
// fixture (dialogue-lyrics-v5), whose delivered ASS predates the top-position
// decoupling. It is supplementary evidence only and does NOT replace the target
// baseline (terminal-list-s02e01-gpt61-enhanced).
//
// Counts below were measured on the real files at
// 1e7ade0f62c826893d49902a7b1f91fd2df75481, not assumed:
//
//	delivered 656, re-rendered 658, net +2
//	matched 633, delivered-only 4, new-only 6, changed-in-place 19
//
// All diffs are attributed to exactly eight clusters; anything else fails.
func TestTopTemporal_RecordedE01_DetailedDiffAudit(t *testing.T) {
	dir := os.Getenv(positionRecordDirEnv)
	if dir == "" {
		t.Skip("skipping E01 recorded replay audit: set FUSIONN_POSITION_RECORD_DIR to run")
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

	// Cluster spans measured from the real files. Each is a top-position group
	// whose temporal planning was unblocked (or a text reflow inside the same
	// interval); nothing outside these spans may change.
	type cluster struct {
		lo, hi        int
		wantDelivered int
		wantNew       int
	}
	clusters := []cluster{
		{484570, 500330, 6, 8},   // groups 101/102/103 counterpart slicing + 2+2 split
		{506550, 518060, 4, 5},   // groups 106/107 splitChineseSources + slicing
		{559480, 565110, 2, 2},   // group 119 replay removal (text only, no count change)
		{565110, 568400, 2, 2},   // group 121 slicing
		{584000, 589670, 2, 2},   // group 128 slicing (two dialogue events)
		{794920, 798260, 1, 2},   // on-screen text split at a source line break
		{2047710, 2051550, 1, 2}, // top on-screen text split at a source line break
		{2727770, 2734190, 5, 2}, // English-side blocks reflowed into the same interval
	}
	clusterOf := func(start, end int) int {
		for i, c := range clusters {
			if start >= c.lo-100 && end <= c.hi+100 {
				return i
			}
		}
		return -1
	}

	var (
		matched   int
		oldOnly   int
		newOnly   int
		changed   int
		delPerWin = make([]int, len(clusters))
		newPerWin = make([]int, len(clusters))
	)

	dI, nI := 0, 0
	for dI < len(deliveredEvents) || nI < len(newEvents) {
		if dI < len(deliveredEvents) && nI < len(newEvents) {
			d := deliveredEvents[dI]
			n := newEvents[nI]
			if d.start == n.start && d.end == n.end && normText(d.text) == normText(n.text) {
				matched++
				dI++
				nI++
				continue
			}
		}

		dTime, nTime := 1<<62, 1<<62
		if dI < len(deliveredEvents) {
			dTime = deliveredEvents[dI].start
		}
		if nI < len(newEvents) {
			nTime = newEvents[nI].start
		}

		switch {
		case dTime < nTime:
			d := deliveredEvents[dI]
			wi := clusterOf(d.start, d.end)
			if wi < 0 {
				t.Fatalf("unattributable delivered-only change at [%d..%d]: %s", d.start, d.end, d.text)
			}
			delPerWin[wi]++
			oldOnly++
			dI++
		case nTime < dTime:
			n := newEvents[nI]
			wi := clusterOf(n.start, n.end)
			if wi < 0 {
				t.Fatalf("unattributable new-only change at [%d..%d]: %s", n.start, n.end, n.text)
			}
			newPerWin[wi]++
			newOnly++
			nI++
		default:
			d := deliveredEvents[dI]
			n := newEvents[nI]
			wi := clusterOf(d.start, d.end)
			if wi < 0 {
				t.Fatalf("unattributable changed delivered event at [%d..%d]: %s", d.start, d.end, d.text)
			}
			wj := clusterOf(n.start, n.end)
			if wj < 0 {
				t.Fatalf("unattributable changed new event at [%d..%d]: %s", n.start, n.end, n.text)
			}
			delPerWin[wi]++
			newPerWin[wj]++
			changed++
			dI++
			nI++
		}
	}

	// Exact, measured totals. Any drift fails.
	if matched != 633 {
		t.Errorf("matched identical events = %d, want 633", matched)
	}
	if oldOnly != 4 {
		t.Errorf("delivered-only events = %d, want 4", oldOnly)
	}
	if newOnly != 6 {
		t.Errorf("new-only events = %d, want 6", newOnly)
	}
	if changed != 19 {
		t.Errorf("changed-in-place events = %d, want 19", changed)
	}
	if len(newEvents)-len(deliveredEvents) != 2 {
		t.Errorf("net event change = %d, want +2", len(newEvents)-len(deliveredEvents))
	}
	// 656 - 4 - 19 = 633 and 658 - 6 - 19 = 633.
	if len(deliveredEvents)-oldOnly-changed != matched {
		t.Errorf("delivered side does not close: %d - %d - %d != %d",
			len(deliveredEvents), oldOnly, changed, matched)
	}
	if len(newEvents)-newOnly-changed != matched {
		t.Errorf("new side does not close: %d - %d - %d != %d",
			len(newEvents), newOnly, changed, matched)
	}

	// Per-cluster tallies: every diff is attributed to a named cluster.
	for i, c := range clusters {
		if delPerWin[i] != c.wantDelivered {
			t.Errorf("cluster [%d..%d]: delivered changed = %d, want %d",
				c.lo, c.hi, delPerWin[i], c.wantDelivered)
		}
		if newPerWin[i] != c.wantNew {
			t.Errorf("cluster [%d..%d]: new changed = %d, want %d",
				c.lo, c.hi, newPerWin[i], c.wantNew)
		}
	}

	// The group-119 replay must be gone in this fixture too: the first Chinese
	// line may not be displayed inside the second English window.
	for _, e := range newEvents {
		if strings.Contains(e.text, "他四处为家") && e.start >= 562479 && e.end <= 565110 {
			t.Errorf("group 119 replay survives in the old fixture: [%d..%d] %s", e.start, e.end, e.text)
		}
	}
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

// TestTopTemporal_EnhancedE01_DetailedAudit audits the real official baseline
// fixture terminal-list-s02e01-gpt61-enhanced (506 groups, 3840x1632) in read-only
// mode, explicitly checking 0-based groupIndex 101, 102, 103, 106, 107, 119,
// 121, 128, plus the bottom control group 300 and top on-screen text group 323.
//
// This test runs only when FUSIONN_ENHANCED_RECORD_DIR is explicitly set.
func TestTopTemporal_EnhancedE01_DetailedAudit(t *testing.T) {
	dir := os.Getenv("FUSIONN_ENHANCED_RECORD_DIR")
	if dir == "" {
		t.Skip("skipping enhanced E01 audit: set FUSIONN_ENHANCED_RECORD_DIR to run")
	}

	sourcesBytes, err := os.ReadFile(filepath.Join(dir, "sources.json"))
	if err != nil {
		t.Fatalf("read sources.json: %v", err)
	}
	var bundle Bundle
	if err := json.Unmarshal(sourcesBytes, &bundle); err != nil {
		t.Fatalf("unmarshal sources: %v", err)
	}

	mappingBytes, err := os.ReadFile(filepath.Join(dir, "alignment", "mapping.json"))
	if err != nil {
		t.Fatalf("read alignment/mapping.json: %v", err)
	}
	var mapping Mapping
	if err := json.Unmarshal(mappingBytes, &mapping); err != nil {
		t.Fatalf("unmarshal mapping: %v", err)
	}

	styleBytes, err := os.ReadFile(filepath.Join(dir, "delivered-style.json"))
	if err != nil {
		t.Fatalf("read delivered-style.json: %v", err)
	}
	var style config.ASSStyleConfig
	if err := json.Unmarshal(styleBytes, &style); err != nil {
		t.Fatalf("unmarshal style: %v", err)
	}

	wordsBytes, err := os.ReadFile(filepath.Join(dir, "word-timings.json"))
	if err != nil {
		t.Fatalf("read word-timings.json: %v", err)
	}
	var words WordTimings
	if err := json.Unmarshal(wordsBytes, &words); err != nil {
		t.Fatalf("unmarshal words: %v", err)
	}

	settingsBytes, err := os.ReadFile(filepath.Join(dir, "render-settings.json"))
	if err != nil {
		t.Fatalf("read render-settings.json: %v", err)
	}
	var settings struct {
		Width  int `json:"width"`
		Height int `json:"height"`
		LLM    struct {
			EnChars   int `json:"en_chars"`
			ZhChars   int `json:"zh_chars"`
			EnGapMS   int `json:"en_gap_ms"`
			ZhGapMS   int `json:"zh_gap_ms"`
			SpanMS    int `json:"span_ms"`
			FillGapMS int `json:"fill_gap_ms"`
		} `json:"llm"`
	}
	if err := json.Unmarshal(settingsBytes, &settings); err != nil {
		t.Fatalf("unmarshal settings: %v", err)
	}
	llmCfg := config.LLMConfig{
		EnChars:   settings.LLM.EnChars,
		ZhChars:   settings.LLM.ZhChars,
		EnGapMS:   settings.LLM.EnGapMS,
		ZhGapMS:   settings.LLM.ZhGapMS,
		SpanMS:    settings.LLM.SpanMS,
		FillGapMS: settings.LLM.FillGapMS,
	}

	deliveredASSBytes, err := os.ReadFile(filepath.Join(dir, "bilingual.ass"))
	if err != nil {
		t.Fatalf("read bilingual.ass: %v", err)
	}
	deliveredASS := string(deliveredASSBytes)
	deliveredEvents := parseDialogues(t, deliveredASS)

	out, err := RenderWithWordTimings(bundle, mapping, llmCfg, style, settings.Width, settings.Height, words)
	if err != nil {
		t.Fatalf("render enhanced: %v", err)
	}
	_ = os.WriteFile("/tmp/fusionn_enhanced_rerendered.ass", out, 0644)

	newEvents := parseDialogues(t, string(out))
	t.Logf("Delivered events: %d, re-rendered events: %d", len(deliveredEvents), len(newEvents))

	enMap, zhMap, err := cueIndex(bundle)
	if err != nil {
		t.Fatalf("cueIndex: %v", err)
	}

	// Helper to find events overlapping a time range
	findEvents := func(evs []dialogue, start, end int) []dialogue {
		var res []dialogue
		for _, e := range evs {
			if e.start >= start-50 && e.end <= end+50 {
				res = append(res, e)
			}
		}
		return res
	}

	// Group verification definitions for 0-based group indices:
	// 101, 102, 103, 106, 107, 119, 121, 128, 305.
	type groupExpectation struct {
		groupIndex  int
		expectStyle string
		checkFunc   func(t *testing.T, delivered, rendered []dialogue, g Group)
	}

	expectations := []groupExpectation{
		{
			groupIndex:  101, // 0-based 101: EN [105, 106], ZH [86], Top
			expectStyle: "TopDefault",
			checkFunc: func(t *testing.T, del, ren []dialogue, g Group) {
				if len(ren) != 2 {
					t.Fatalf("group 101: want 2 counterpart sliced events, got %d", len(ren))
				}
				for i, e := range ren {
					if e.style != "TopDefault" {
						t.Errorf("group 101 ev[%d]: style = %q, want TopDefault", i, e.style)
					}
				}
				if ren[0].start != 484570 || ren[0].end != 486610 {
					t.Errorf("group 101 ev[0] time = [%d..%d], want [484570..486610]", ren[0].start, ren[0].end)
				}
				if ren[1].start != 486610 || ren[1].end != 489950 {
					t.Errorf("group 101 ev[1] time = [%d..%d], want [486610..489950]", ren[1].start, ren[1].end)
				}
				assertGlyphs(t, ren[0].text, "出现了一个精通高新技术的a new, technologically savvy al-Qaeda")
				assertGlyphs(t, ren[1].text, "新基地组织对西方发动战争has emerged to wage war against the west,")
			},
		},
		{
			groupIndex:  102, // 0-based 102: EN [107, 108], ZH [87], Top
			expectStyle: "TopDefault",
			checkFunc: func(t *testing.T, del, ren []dialogue, g Group) {
				if len(ren) != 2 {
					t.Fatalf("group 102: want 2 counterpart sliced events, got %d", len(ren))
				}
				for i, e := range ren {
					if e.style != "TopDefault" {
						t.Errorf("group 102 ev[%d]: style = %q, want TopDefault", i, e.style)
					}
				}
				if ren[0].start != 489950 || ren[0].end != 491740 {
					t.Errorf("group 102 ev[0] time = [%d..%d], want [489950..491740]", ren[0].start, ren[0].end)
				}
				if ren[1].start != 491740 || ren[1].end != 493830 {
					t.Errorf("group 102 ev[1] time = [%d..%d], want [491740..493830]", ren[1].start, ren[1].end)
				}
				assertGlyphs(t, ren[0].text, "他们甚至在社媒上even using images of the attack")
				assertGlyphs(t, ren[1].text, "利用袭击现场的影像招兵买马on social media for recruitment.")
			},
		},
		{
			groupIndex:  103, // 0-based 103: EN [109, 110], ZH [88, 89], Top
			expectStyle: "TopDefault",
			checkFunc: func(t *testing.T, del, ren []dialogue, g Group) {
				if len(ren) != 4 {
					t.Fatalf("group 103: want 4 split events, got %d", len(ren))
				}
				for i, e := range ren {
					if e.style != "TopDefault" {
						t.Errorf("group 103 ev[%d]: style = %q, want TopDefault", i, e.style)
					}
				}
				if ren[0].start != 493830 || ren[0].end != 495350 {
					t.Errorf("group 103 ev[0] time = [%d..%d], want [493830..495350]", ren[0].start, ren[0].end)
				}
				if ren[1].start != 495350 || ren[1].end != 497370 {
					t.Errorf("group 103 ev[1] time = [%d..%d], want [495350..497370]", ren[1].start, ren[1].end)
				}
				if ren[2].start != 497370 || ren[2].end != 498250 {
					t.Errorf("group 103 ev[2] time = [%d..%d], want [497370..498250]", ren[2].start, ren[2].end)
				}
				if ren[3].start != 498250 || ren[3].end != 500330 {
					t.Errorf("group 103 ev[3] time = [%d..%d], want [498250..500330]", ren[3].start, ren[3].end)
				}
				assertGlyphs(t, ren[0].text, "新基地组织的威胁 出现在詹姆斯里斯The threat of this new al-Qaeda")
				assertGlyphs(t, ren[1].text, "新基地组织的威胁 出现在詹姆斯里斯comes only six months after James Reece's")
				assertGlyphs(t, ren[2].text, "新基地组织的威胁 出现在詹姆斯里斯attack on Defense Secretary Hartley's home,")
				assertGlyphs(t, ren[3].text, "偷袭国防部长哈特利家六个月后attack on Defense Secretary Hartley's home,")
			},
		},
		{
			groupIndex:  106, // 0-based 106: EN [114, 115], ZH [92, 93], Top
			expectStyle: "TopDefault",
			checkFunc: func(t *testing.T, del, ren []dialogue, g Group) {
				if len(ren) != 3 {
					t.Fatalf("group 106: want 3 events from source split, got %d", len(ren))
				}
				for i, e := range ren {
					if e.style != "TopDefault" {
						t.Errorf("group 106 ev[%d]: style = %q, want TopDefault", i, e.style)
					}
				}
				if ren[0].start != 506550 || ren[0].end != 509090 {
					t.Errorf("group 106 ev[0] time = [%d..%d], want [506550..509090]", ren[0].start, ren[0].end)
				}
				if ren[1].start != 509090 || ren[1].end != 510510 {
					t.Errorf("group 106 ev[1] time = [%d..%d], want [509090..510510]", ren[1].start, ren[1].end)
				}
				if ren[2].start != 510510 || ren[2].end != 512760 {
					t.Errorf("group 106 ev[2] time = [%d..%d], want [510510..512760]", ren[2].start, ren[2].end)
				}
				assertGlyphs(t, ren[0].text, "基本上就是 亚敏阿尔-纳瓦兹的新基地组织that's basically Amin Al-Nawaz's")
				assertGlyphs(t, ren[1].text, "基本上就是 亚敏阿尔-纳瓦兹的新基地组织new al-Qaeda: making terror into TikToks.")
				assertGlyphs(t, ren[2].text, "把恐袭做成短视频new al-Qaeda: making terror into TikToks.")
			},
		},
		{
			groupIndex:  107, // 0-based 107: EN [116, 117], ZH [94], Top
			expectStyle: "TopDefault",
			checkFunc: func(t *testing.T, del, ren []dialogue, g Group) {
				if len(ren) != 2 {
					t.Fatalf("group 107: want 2 counterpart sliced events, got %d", len(ren))
				}
				for i, e := range ren {
					if e.style != "TopDefault" {
						t.Errorf("group 107 ev[%d]: style = %q, want TopDefault", i, e.style)
					}
				}
				if ren[0].start != 512760 || ren[0].end != 514350 {
					t.Errorf("group 107 ev[0] time = [%d..%d], want [512760..514350]", ren[0].start, ren[0].end)
				}
				if ren[1].start != 514350 || ren[1].end != 518060 {
					t.Errorf("group 107 ev[1] time = [%d..%d], want [514350..518060]", ren[1].start, ren[1].end)
				}
				assertGlyphs(t, ren[0].text, "亚敏在散播恐惧 一是要招收新人Amin is spreading fear")
				assertGlyphs(t, ren[1].text, "二是要让我们的经济瘫痪both to find recruits and to disable our economy.")
			},
		},
		{
			groupIndex:  119, // 0-based 119: EN [133, 134], ZH [108], Top
			expectStyle: "TopDefault",
			checkFunc: func(t *testing.T, del, ren []dialogue, g Group) {
				// The real English source cues carry a 1084ms gap (EN133 ends 561395,
				// EN134 starts 562479). Each Chinese source line must follow its own
				// English counterpart, so there are exactly two 1:1 events and no
				// replay of the first line inside EN134's window.
				if len(ren) != 2 {
					t.Fatalf("group 119: want 2 events (one per Chinese source line), got %d", len(ren))
				}
				for i, e := range ren {
					if e.style != "TopDefault" {
						t.Errorf("group 119 ev[%d]: style = %q, want TopDefault", i, e.style)
					}
				}
				if ren[0].start != 559480 || ren[0].end != 561400 {
					t.Errorf("group 119 ev[0] time = [%d..%d], want [559480..561400]", ren[0].start, ren[0].end)
				}
				if ren[1].start != 562480 || ren[1].end != 565110 {
					t.Errorf("group 119 ev[1] time = [%d..%d], want [562480..565110]", ren[1].start, ren[1].end)
				}
				assertGlyphs(t, ren[0].text, "不在乎17点回家 他四处为家Not going home at 1700. He is home.")
				assertGlyphs(t, ren[1].text, "他只关心大业”He only cares about the cause.")
				// The first Chinese line must never appear in the second English window.
				for i, e := range ren {
					if strings.Contains(e.text, "他四处为家") && e.start >= 562479 {
						t.Errorf("group 119 ev[%d] replays the first Chinese line inside EN134: [%d..%d] %s",
							i, e.start, e.end, e.text)
					}
				}
			},
		},
		{
			groupIndex:  121, // 0-based 121: EN [135], ZH [110, 111], Top
			expectStyle: "TopDefault",
			checkFunc: func(t *testing.T, del, ren []dialogue, g Group) {
				if len(ren) != 2 {
					t.Fatalf("group 121: want 2 events, got %d", len(ren))
				}
				for i, e := range ren {
					if e.style != "TopDefault" {
						t.Errorf("group 121 ev[%d]: style = %q, want TopDefault", i, e.style)
					}
				}
				if ren[0].start != 565110 || ren[0].end != 567100 {
					t.Errorf("group 121 ev[0] time = [%d..%d], want [565110..567100]", ren[0].start, ren[0].end)
				}
				if ren[1].start != 567100 || ren[1].end != 568400 {
					t.Errorf("group 121 ev[1] time = [%d..%d], want [567100..568400]", ren[1].start, ren[1].end)
				}
				assertGlyphs(t, ren[0].text, "这是一位不具名的 特种部队教官的话And that is a quote from an unidentified")
				assertGlyphs(t, ren[1].text, "这是一位不具名的 特种部队教官的话Special Forces instructor,")
			},
		},
		{
			groupIndex:  128, // 0-based 128: EN [143, 144], ZH [118], Top
			expectStyle: "TopDefault",
			checkFunc: func(t *testing.T, del, ren []dialogue, g Group) {
				var topEvs []dialogue
				for _, e := range ren {
					if e.style == "TopDefault" {
						topEvs = append(topEvs, e)
					}
				}
				if len(topEvs) != 2 {
					t.Fatalf("group 128: want 2 TopDefault events, got %d", len(topEvs))
				}
				if topEvs[0].start != 584000 || topEvs[0].end != 586420 {
					t.Errorf("group 128 ev[0] time = [%d..%d], want [584000..586420]", topEvs[0].start, topEvs[0].end)
				}
				if topEvs[1].start != 586420 || topEvs[1].end != 589670 {
					t.Errorf("group 128 ev[1] time = [%d..%d], want [586420..589670]", topEvs[1].start, topEvs[1].end)
				}
				assertGlyphs(t, topEvs[0].text, "我同意基地组织一直都在I agree that Al-Qaeda never went away,")
				assertGlyphs(t, topEvs[1].text, "但阿尔-纳瓦兹是不一样的敌人but Al-Nawaz is a very different animal.")
			},
		},
		{
			groupIndex:  300, // 0-based 300: EN [332], ZH [296], Bottom (clean bottom control)
			expectStyle: "Default",
			checkFunc: func(t *testing.T, del, ren []dialogue, g Group) {
				if len(ren) != 1 {
					t.Fatalf("group 300: want 1 event, got %d", len(ren))
				}
				if ren[0].style != "Default" {
					t.Errorf("group 300: style = %q, want Default", ren[0].style)
				}
				if len(del) != 1 {
					t.Fatalf("group 300 delivered: want 1 event, got %d", len(del))
				}
				if ren[0].start != del[0].start || ren[0].end != del[0].end || ren[0].text != del[0].text {
					t.Errorf("group 300 mismatch:\n  del: [%d..%d] %s\n  ren: [%d..%d] %s",
						del[0].start, del[0].end, del[0].text, ren[0].start, ren[0].end, ren[0].text)
				}
			},
		},
		{
			groupIndex:  323, // 0-based 323: EN [], ZH [322], Top on_screen_text
			expectStyle: "TopDefault",
			checkFunc: func(t *testing.T, del, ren []dialogue, g Group) {
				if len(ren) != 2 {
					t.Fatalf("group 323: want 2 events from split, got %d", len(ren))
				}
				for i, e := range ren {
					if e.style != "TopDefault" {
						t.Errorf("group 323 ev[%d]: style = %q, want TopDefault", i, e.style)
					}
				}
				if ren[0].start != 2047710 || ren[0].end != 2049270 {
					t.Errorf("group 323 ev[0] time = [%d..%d], want [2047710..2049270]", ren[0].start, ren[0].end)
				}
				if ren[1].start != 2049270 || ren[1].end != 2051550 {
					t.Errorf("group 323 ev[1] time = [%d..%d], want [2049270..2051550]", ren[1].start, ren[1].end)
				}
				assertGlyphs(t, ren[0].text, "（詹姆斯里斯信息/下落线索")
				assertGlyphs(t, ren[1].text, "请用标签“詹姆斯里斯在哪里”联系我们）")
			},
		},
	}

	for _, exp := range expectations {
		g := mapping.Groups[exp.groupIndex]

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

		delG := findEvents(deliveredEvents, minStart, maxEnd)
		renG := findEvents(newEvents, minStart, maxEnd)

		t.Run(fmt.Sprintf("group_%d", exp.groupIndex), func(t *testing.T) {
			exp.checkFunc(t, delG, renG, g)
		})
	}

	// Global parity assertion:
	// All changes must fall inside the permitted windows of the decoupled top groups.
	// Each window is a top (or top-migrated) group whose temporal planning was unblocked:
	//   [484570..500330]  groups 101, 102, 103   (delivered 6 -> new 8, +2)
	//   [506550..518060]  groups 106, 107         (delivered 4 -> new 5, +1)
	//   [559480..565110]  group  119              (delivered 2 -> new 2,  0)
	//   [565110..568400]  group  121              (delivered 2 -> new 2,  0)
	//   [584000..589670]  group  128              (delivered 2 -> new 2,  0)
	//   [2047710..2051550] group 323              (delivered 1 -> new 2, +1)
	permittedWindows := [][2]int{
		{484570, 500330},
		{506550, 518060},
		{559480, 565110},
		{565110, 568400},
		{584000, 589670},
		{2047710, 2051550},
	}
	// Per-window expected (delivered, new) event tallies: every changed event must be
	// attributed to one of these windows, and the tally must close exactly.
	wantPerWindow := [][2]int{
		{6, 8},
		{4, 5},
		{2, 2},
		{2, 2},
		{2, 2},
		{1, 2},
	}
	windowOf := func(start, end int) int {
		for i, w := range permittedWindows {
			if start >= w[0]-100 && end <= w[1]+100 {
				return i
			}
		}
		return -1
	}

	// Group 331 is a bottom-layer model dialogue whose rendered placement is decided by
	// the pre-existing bidirectional migration rule, not by this change. It must be
	// untouched: assert that every delivered event in its span is reproduced byte-for-byte.
	{
		const g331Lo, g331Hi = 2106230, 2110441
		var del331, new331 []dialogue
		for _, e := range deliveredEvents {
			if e.start >= g331Lo && e.end <= g331Hi {
				del331 = append(del331, e)
			}
		}
		for _, e := range newEvents {
			if e.start >= g331Lo && e.end <= g331Hi {
				new331 = append(new331, e)
			}
		}
		if len(del331) == 0 {
			t.Fatalf("group 331 span has no delivered events to compare")
		}
		if len(del331) != len(new331) {
			t.Fatalf("group 331 span event count changed: %d -> %d", len(del331), len(new331))
		}
		for i := range del331 {
			if del331[i].start != new331[i].start || del331[i].end != new331[i].end ||
				del331[i].style != new331[i].style || del331[i].text != new331[i].text {
				t.Errorf("group 331 span ev[%d] changed:\n  del: [%d..%d] %s\n  new: [%d..%d] %s",
					i, del331[i].start, del331[i].end, del331[i].text,
					new331[i].start, new331[i].end, new331[i].text)
			}
		}
	}

	dI, nI := 0, 0
	matchedCount := 0
	deliveredDiffCount := 0
	newDiffCount := 0
	deliveredPerWindow := make([]int, len(permittedWindows))
	newPerWindow := make([]int, len(permittedWindows))

	for dI < len(deliveredEvents) || nI < len(newEvents) {
		if dI < len(deliveredEvents) && nI < len(newEvents) {
			d := deliveredEvents[dI]
			n := newEvents[nI]
			if d.start == n.start && d.end == n.end && d.style == n.style && d.text == n.text {
				matchedCount++
				dI++
				nI++
				continue
			}
		}

		dTime := 1 << 60
		if dI < len(deliveredEvents) {
			dTime = deliveredEvents[dI].start
		}
		nTime := 1 << 60
		if nI < len(newEvents) {
			nTime = newEvents[nI].start
		}

		if dTime < nTime {
			d := deliveredEvents[dI]
			wi := windowOf(d.start, d.end)
			if wi < 0 {
				t.Fatalf("unattributable change in delivered event at [%d..%d] %s: %s",
					d.start, d.end, d.style, d.text)
			}
			deliveredPerWindow[wi]++
			deliveredDiffCount++
			dI++
		} else if nTime < dTime {
			n := newEvents[nI]
			wi := windowOf(n.start, n.end)
			if wi < 0 {
				t.Fatalf("unattributable change in new event at [%d..%d] %s: %s",
					n.start, n.end, n.style, n.text)
			}
			newPerWindow[wi]++
			newDiffCount++
			nI++
		} else {
			d := deliveredEvents[dI]
			n := newEvents[nI]
			wi := windowOf(d.start, d.end)
			if wi < 0 {
				t.Fatalf("unattributable change in delivered event at [%d..%d] %s: %s",
					d.start, d.end, d.style, d.text)
			}
			wj := windowOf(n.start, n.end)
			if wj < 0 {
				t.Fatalf("unattributable change in new event at [%d..%d] %s: %s",
					n.start, n.end, n.style, n.text)
			}
			deliveredPerWindow[wi]++
			newPerWindow[wj]++
			deliveredDiffCount++
			newDiffCount++
			dI++
			nI++
		}
	}

	if matchedCount != 647 {
		t.Errorf("matched identical events = %d, want 647", matchedCount)
	}
	if deliveredDiffCount != 17 {
		t.Errorf("delivered events in diff clusters = %d, want 17", deliveredDiffCount)
	}
	if newDiffCount != 21 {
		t.Errorf("new events in diff clusters = %d, want 21", newDiffCount)
	}
	for i := range permittedWindows {
		if deliveredPerWindow[i] != wantPerWindow[i][0] {
			t.Errorf("window [%d..%d]: delivered changed events = %d, want %d",
				permittedWindows[i][0], permittedWindows[i][1],
				deliveredPerWindow[i], wantPerWindow[i][0])
		}
		if newPerWindow[i] != wantPerWindow[i][1] {
			t.Errorf("window [%d..%d]: new changed events = %d, want %d",
				permittedWindows[i][0], permittedWindows[i][1],
				newPerWindow[i], wantPerWindow[i][1])
		}
	}
}

// TestTopTemporal_CompareTopVsBottomTemporalEquivalence verifies that for the
// real episode groups (such as 101, 103, 104, 106, 109, 121), the temporal
// planning decisions (event intervals, splitting, text fragments) are identical
// whether the group is at the top or at the bottom.
//
// Gated behind FUSIONN_POSITION_RECORD_DIR.
func TestTopTemporal_CompareTopVsBottomTemporalEquivalence(t *testing.T) {
	dir := os.Getenv(positionRecordDirEnv)
	if dir == "" {
		t.Skip("skipping E01 recorded replay: set FUSIONN_POSITION_RECORD_DIR to run")
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
