package subtitlellm

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fusionn/internal/config"
)

// eventOverlapsGap returns true if the dialogue event intersects the interval (gapStart, gapEnd).
// Centisecond rounding matches ASS Dialogue timing.
func eventOverlapsGap(ev dialogue, gapStart, gapEnd int) bool {
	gStart := roundCentiseconds(gapStart)
	gEnd := roundCentiseconds(gapEnd)
	interStart := max(ev.start, gStart)
	interEnd := min(ev.end, gEnd)
	return interEnd > interStart
}

// TestGeneric2To2SourceGapDoesNotFlashOldChinese verifies that in a 2:2 group
// with a real gap between English slices, an earlier Chinese cue whose source
// interval already ended before the gap does not flash back at the start of
// the second English slice.
func TestGeneric2To2SourceGapDoesNotFlashOldChinese(t *testing.T) {
	cfg := renderTestConfig()
	bundle := makeBundle(
		[]Cue{
			{ID: 1, Start: 1000, End: 4000, Display: "You go ashore and wait."},
			{ID: 2, Start: 6000, End: 9000, Display: "Bring all your trouble to the doorstep."},
		},
		[]Cue{
			{ID: 1, Start: 1200, End: 3800, Display: "你在岸上等我"},
			{ID: 2, Start: 6080, End: 9200, Display: "把你的麻烦带到门口"},
		},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1, 2}, ZHIDs: []int{1, 2}, Layer: LayerBottom, Origin: OriginModel}})
	output, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	events := parseDialogues(t, string(output))
	for i, ev := range events {
		t.Logf("event[%d]: [%d..%d] text=%q", i, ev.start, ev.end, ev.text)
	}

	// Assert:
	// 1. The old Chinese text "你在岸上等我" must NEVER appear after the gap (after 4000ms).
	// 2. The gap between 4000ms and 6000ms must remain completely blank (no interval overlap).
	// 3. No 80ms flashback event at the start of EN2 (6000ms).
	for _, ev := range events {
		if ev.start >= 4000 && strings.Contains(ev.text, "你在岸上等我") {
			t.Fatalf("old Chinese flashed back after gap in event: [%d..%d] text=%q", ev.start, ev.end, ev.text)
		}
		if eventOverlapsGap(ev, 4000, 6000) {
			t.Fatalf("source gap [4000..6000] was intersected by event: [%d..%d] text=%q", ev.start, ev.end, ev.text)
		}
	}
	if len(events) != 2 {
		t.Fatalf("expected exactly 2 dialogue events, got %d: %+v", len(events), events)
	}
	assertGlyphs(t, events[0].text, "你在岸上等我You go ashore and wait.")
	assertGlyphs(t, events[1].text, "把你的麻烦带到门口Bring all your trouble to the doorstep.")
}

// TestTerminalListS02E01Group305EmbeddedFixture is a self-contained portable
// test exercising the exact timing and text from Group 305 of Terminal List S02E01
// through both Render and RenderWithWordTimings, ensuring no developer-local directory
// is required to prove that the 80ms flashback is eliminated.
func TestTerminalListS02E01Group305EmbeddedFixture(t *testing.T) {
	cfg := config.LLMConfig{
		EnChars:   65,
		ZhChars:   20,
		EnGapMS:   400,
		ZhGapMS:   500,
		FillGapMS: 500,
		SpanMS:    5500,
	}
	style := renderTestStyle()

	bundle := makeBundle(
		[]Cue{
			{ID: 1, Start: 1762428, End: 1764054, Display: "You go ashore..."},
			{ID: 2, Start: 1765431, End: 1768433, Display: "...you're gonna bring all your bullshit\nright to the Hastings' doorstep."},
		},
		[]Cue{
			{ID: 1, Start: 1762678, End: 1763888, Display: "你上岸后"},
			{ID: 2, Start: 1765514, End: 1768641, Display: "把你所有的仇恨\n带到海斯廷斯家门口"},
		},
	)
	mapping := makeMapping(bundle, []Group{
		{ENIDs: []int{1, 2}, ZHIDs: []int{1, 2}, Layer: LayerBottom, Origin: OriginModel},
	})
	words := WordTimings{
		1: {{Text: "You", StartMS: 1762428, EndMS: 1762700}, {Text: "go", StartMS: 1762700, EndMS: 1763000}, {Text: "ashore...", StartMS: 1763000, EndMS: 1764054}},
		2: {{Text: "...you're", StartMS: 1765431, EndMS: 1766000}, {Text: "doorstep.", StartMS: 1768000, EndMS: 1768433}},
	}

	for _, mode := range []string{"Render", "RenderWithWordTimings"} {
		var output []byte
		var err error
		if mode == "Render" {
			output, err = Render(bundle, mapping, cfg, style, 3840, 1632)
		} else {
			output, err = RenderWithWordTimings(bundle, mapping, cfg, style, 3840, 1632, words)
		}
		if err != nil {
			t.Fatalf("[%s] render failed: %v", mode, err)
		}

		events := parseDialogues(t, string(output))
		for i, ev := range events {
			t.Logf("[%s] event[%d]: [%d..%d] (dur=%dms) text=%q", mode, i, ev.start, ev.end, ev.end-ev.start, ev.text)
		}

		// 1. "你上岸后" must NOT appear in any event after 1764054ms.
		for _, ev := range events {
			if ev.start >= 1764054 && strings.Contains(ev.text, "你上岸后") {
				t.Fatalf("[%s] '你上岸后' flashed back after its speech window at [%d..%d]: %s", mode, ev.start, ev.end, ev.text)
			}
		}

		// 2. The 80ms event [1765430..1765510] must NOT exist.
		for _, ev := range events {
			if ev.start == 1765430 && ev.end <= 1765520 {
				t.Fatalf("[%s] 80ms flashback event still exists: [%d..%d] %s", mode, ev.start, ev.end, ev.text)
			}
		}

		// 3. Natural speech gap between 1764054 and 1765431 must remain completely blank.
		for _, ev := range events {
			if eventOverlapsGap(ev, 1764054, 1765431) {
				t.Fatalf("[%s] natural speech gap was intersected by event: [%d..%d] text=%q", mode, ev.start, ev.end, ev.text)
			}
		}

		// 4. "把你所有的仇恨" starts cleanly at 1765430ms.
		var foundZH302Start bool
		for _, ev := range events {
			if ev.start == 1765430 && strings.Contains(ev.text, "把你所有的仇恨") {
				foundZH302Start = true
			}
		}
		if !foundZH302Start {
			t.Fatalf("[%s] ZH302 did not start cleanly at 1765430ms", mode)
		}
	}
}

// TestCounterexample_IndependentShortCuePreserved verifies that an originally
// independent short cue (such as 80ms) is preserved and not removed as noise.
func TestCounterexample_IndependentShortCuePreserved(t *testing.T) {
	cfg := renderTestConfig()
	bundle := makeBundle(
		[]Cue{{ID: 1, Start: 2000, End: 2080, Display: "Wait."}},
		[]Cue{{ID: 1, Start: 2000, End: 2080, Display: "等下"}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel}})
	output, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	events := parseDialogues(t, string(output))
	if len(events) != 1 {
		t.Fatalf("expected 1 event for independent short cue, got %d", len(events))
	}
	if events[0].start != 2000 || events[0].end != 2080 {
		t.Fatalf("expected exact 80ms timing [2000..2080], got [%d..%d]", events[0].start, events[0].end)
	}
	assertGlyphs(t, events[0].text, "等下Wait.")
}

// TestCounterexample_IdenticalTextDifferentSourceIDsPreserved verifies B06:
// distinct source IDs speaking identical text at different times are both
// preserved and not mistakenly deduplicated or dropped.
func TestCounterexample_IdenticalTextDifferentSourceIDsPreserved(t *testing.T) {
	cfg := renderTestConfig()
	bundle := makeBundle(
		[]Cue{
			{ID: 1, Start: 1000, End: 2000, Display: "Yeah."},
			{ID: 2, Start: 4000, End: 5000, Display: "Yeah."},
		},
		[]Cue{
			{ID: 1, Start: 1000, End: 2000, Display: "是的"},
			{ID: 2, Start: 4000, End: 5000, Display: "是的"},
		},
	)
	mapping := makeMapping(bundle, []Group{
		{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel},
		{ENIDs: []int{2}, ZHIDs: []int{2}, Layer: LayerBottom, Origin: OriginModel},
	})
	output, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	events := parseDialogues(t, string(output))
	if len(events) != 2 {
		t.Fatalf("expected 2 separate events for identical text with different IDs, got %d", len(events))
	}
	if events[0].start != 1000 || events[0].end != 2000 {
		t.Fatalf("unexpected timing for first event: [%d..%d]", events[0].start, events[0].end)
	}
	if events[1].start != 4000 || events[1].end != 5000 {
		t.Fatalf("unexpected timing for second event: [%d..%d]", events[1].start, events[1].end)
	}
	assertGlyphs(t, events[0].text, "是的Yeah.")
	assertGlyphs(t, events[1].text, "是的Yeah.")
}

// TestCounterexample_CrossSliceContinuousHoldPreserved verifies B05:
// holding across continuous English slices without a gap is preserved,
// even when an individual ASS slice is very short (e.g. 100ms).
func TestCounterexample_CrossSliceContinuousHoldPreserved(t *testing.T) {
	cfg := renderTestConfig()
	cfg.EnChars = 15 // forces English to split across multiple slices
	bundle := makeBundle(
		[]Cue{
			{ID: 1, Start: 1000, End: 1100, Display: "First short."},
			{ID: 2, Start: 1100, End: 4000, Display: "Second longer part of sentence."},
		},
		[]Cue{
			{ID: 1, Start: 1000, End: 4000, Display: "整句中文保持"},
		},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1, 2}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel}})
	output, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	events := parseDialogues(t, string(output))
	if len(events) < 2 {
		t.Fatalf("expected at least 2 events for English split, got %d", len(events))
	}
	for i, ev := range events {
		if !strings.Contains(ev.text, "整句中文保持") {
			t.Fatalf("event %d lost the continuous held Chinese: %s", i, ev.text)
		}
	}
	// Verify continuity: first event ends exactly where second begins.
	if events[0].end != events[1].start {
		t.Fatalf("expected continuous boundary between slices: event[0].end=%d != event[1].start=%d", events[0].end, events[1].start)
	}
}

// TestCounterexample_HoldExtendingIntoGapPreserved verifies requirement ①:
// when a Chinese cue's source end extends into the English gap (cues[i].End > gapStart),
// it is an intentional hold that covers the gap, so it is NOT treated as having
// finished before the gap and is not rolled back.
func TestCounterexample_HoldExtendingIntoGapPreserved(t *testing.T) {
	cfg := renderTestConfig()
	cfg.ZhChars = 10
	bundle := makeBundle(
		[]Cue{
			{ID: 1, Start: 1000, End: 3000, Display: "First english clause."},
			{ID: 2, Start: 5000, End: 8000, Display: "Second english clause."},
		},
		[]Cue{
			// ZH1 source extends to 4500 (inside the 3000..5000 gap).
			{ID: 1, Start: 1000, End: 4500, Display: "延伸入间隙中文"},
			{ID: 2, Start: 5100, End: 8000, Display: "后部第二句中文"},
		},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1, 2}, ZHIDs: []int{1, 2}, Layer: LayerBottom, Origin: OriginModel}})
	output, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	events := parseDialogues(t, string(output))
	for i, ev := range events {
		t.Logf("HoldExtending event[%d]: [%d..%d] text=%q", i, ev.start, ev.end, ev.text)
	}

	// 1. Natural gap (3000..5000) must remain blank (no interval intersection).
	for _, ev := range events {
		if eventOverlapsGap(ev, 3000, 5000) {
			t.Fatalf("gap (3000..5000) was intersected by event: [%d..%d] text=%q", ev.start, ev.end, ev.text)
		}
	}

	// 2. Observable invariant: ZH1 was preserved in the post-gap window from 5000 to 5100,
	// followed by ZH2 from 5100 to 8000. It was NOT rolled back to 5000.
	if len(events) != 3 {
		t.Fatalf("expected exactly 3 dialogue events, got %d: %+v", len(events), events)
	}
	if events[0].start != 1000 || events[0].end != 3000 || !strings.Contains(events[0].text, "延伸入间隙中文") {
		t.Fatalf("unexpected event[0]: [%d..%d] text=%q", events[0].start, events[0].end, events[0].text)
	}
	if events[1].start != 5000 || events[1].end != 5100 || !strings.Contains(events[1].text, "延伸入间隙中文") {
		t.Fatalf("unexpected event[1] (expected ZH1 preserved in 5000..5100): [%d..%d] text=%q", events[1].start, events[1].end, events[1].text)
	}
	if events[2].start != 5100 || events[2].end != 8000 || !strings.Contains(events[2].text, "后部第二句中文") {
		t.Fatalf("unexpected event[2] (expected ZH2 starting at 5100): [%d..%d] text=%q", events[2].start, events[2].end, events[2].text)
	}
}

// TestCounterexample_HoldOriginallyCoveringGapPreserved verifies that when
// a Chinese cue's source timing originally covers the gap and extends into the
// subsequent window, that intentional hold across the gap is NOT trimmed away.
func TestCounterexample_HoldOriginallyCoveringGapPreserved(t *testing.T) {
	cfg := renderTestConfig()
	cfg.ZhChars = 10
	bundle := makeBundle(
		[]Cue{
			{ID: 1, Start: 1000, End: 3000, Display: "First english clause."},
			{ID: 2, Start: 5000, End: 8000, Display: "Second english clause."},
		},
		[]Cue{
			// ZH1 originally extends to 6500 (deep inside EN2).
			{ID: 1, Start: 1000, End: 6500, Display: "跨间隙长中文"},
			{ID: 2, Start: 6500, End: 8000, Display: "后部短中文"},
		},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1, 2}, ZHIDs: []int{1, 2}, Layer: LayerBottom, Origin: OriginModel}})
	output, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	events := parseDialogues(t, string(output))

	// 1. Natural gap (3000..5000) must remain blank (no interval intersection).
	for _, ev := range events {
		if eventOverlapsGap(ev, 3000, 5000) {
			t.Fatalf("gap (3000..5000) was intersected by event: [%d..%d] text=%q", ev.start, ev.end, ev.text)
		}
	}

	// 2. In EN2 (5000..8000), ZH1 should remain displayed from 5000 until 6500,
	// followed by ZH2 from 6500 to 8000.
	if len(events) != 3 {
		t.Fatalf("expected exactly 3 dialogue events, got %d: %+v", len(events), events)
	}
	if events[1].start != 5000 || events[1].end != 6500 || !strings.Contains(events[1].text, "跨间隙长中文") {
		t.Fatalf("expected ZH1 held in [5000..6500], got: [%d..%d] text=%q", events[1].start, events[1].end, events[1].text)
	}
	if events[2].start != 6500 || events[2].end != 8000 || !strings.Contains(events[2].text, "后部短中文") {
		t.Fatalf("expected ZH2 displayed in [6500..8000], got: [%d..%d] text=%q", events[2].start, events[2].end, events[2].text)
	}
}

// TestCounterexample_PriorDisplayTooShortNotRolledBack verifies requirement ②:
// when the prior Chinese cue had insufficient visible duration (< 800ms) in the
// earlier window, it does not meet the roll-back condition.
func TestCounterexample_PriorDisplayTooShortNotRolledBack(t *testing.T) {
	cues := []Cue{
		{ID: 1, Start: 1000, End: 3500, Display: "前段首句"},
		// ZH2 in first window only has 4000 - 3600 = 400ms (< 800ms)
		{ID: 2, Start: 3600, End: 3900, Display: "短句"},
		{ID: 3, Start: 6050, End: 9000, Display: "后部第三句"},
	}
	windows := [][2]int{{1000, 4000}, {6000, 9000}}
	cuts := []int{3600, 6050}

	// 1. Unit level invariant: cut at 6050 is NOT rolled back to 6000 because prior display is 400ms < 800ms.
	adjusted := adjustCutsForSourceGaps(cuts, cues, windows)
	if adjusted[1] != 6050 {
		t.Fatalf("expected cut at 6050 to NOT roll back to 6000, got %d", adjusted[1])
	}

	// 2. Render level invariant:
	cfg := renderTestConfig()
	cfg.ZhChars = 8
	bundle := makeBundle(
		[]Cue{
			{ID: 1, Start: 1000, End: 4000, Display: "First english window."},
			{ID: 2, Start: 6000, End: 9000, Display: "Second english window."},
		},
		cues,
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1, 2}, ZHIDs: []int{1, 2, 3}, Layer: LayerBottom, Origin: OriginModel}})
	output, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatal(err)
	}
	events := parseDialogues(t, string(output))
	for i, ev := range events {
		t.Logf("PriorDisplayTooShort event[%d]: [%d..%d] text=%q", i, ev.start, ev.end, ev.text)
	}

	// Speech gap (4000..6000) must not be intersected by any event.
	for _, ev := range events {
		if eventOverlapsGap(ev, 4000, 6000) {
			t.Fatalf("speech gap [4000..6000] was intersected by event: [%d..%d] text=%q", ev.start, ev.end, ev.text)
		}
	}

	// Observable content and interval invariant:
	// Because ZH2 only had 400ms prior display, it was not chopped into an unreadable 50ms tail at 6000;
	// instead it remained readable by being grouped into [6000..9000] with ZH3.
	if len(events) != 3 {
		t.Fatalf("expected exactly 3 dialogue events, got %d: %+v", len(events), events)
	}
	if events[0].start != 1000 || events[0].end != 3670 || !strings.Contains(events[0].text, "前段首句") {
		t.Fatalf("unexpected event[0]: [%d..%d] text=%q", events[0].start, events[0].end, events[0].text)
	}
	if events[1].start != 3670 || events[1].end != 4000 || !strings.Contains(events[1].text, "短句") {
		t.Fatalf("unexpected event[1]: [%d..%d] text=%q", events[1].start, events[1].end, events[1].text)
	}
	if events[2].start != 6000 || events[2].end != 9000 || !strings.Contains(events[2].text, "短句") || !strings.Contains(events[2].text, "后部第三句") {
		t.Fatalf("unexpected event[2]: [%d..%d] text=%q", events[2].start, events[2].end, events[2].text)
	}
}

// TestRoundCentisecondsTiesToEvenBoundary verifies the exact ties-to-even centisecond
// rounding behavior matching render.go:roundCentiseconds:
//  1. [1004..1796] renders as [1000..1800] (800ms >= 800ms) -> triggers rollback.
//  2. [1006..1804] renders as [1010..1800] (790ms < 800ms) -> does NOT trigger rollback.
//  3. Tail boundary at gapEnd=6000:
//     cut=6794 renders as 6790 (tail 790ms < 800ms) -> triggers rollback to 6000.
//     cut=6804 renders as 6800 (tail 800ms >= 800ms) -> does NOT trigger rollback.
func TestRoundCentisecondsTiesToEvenBoundary(t *testing.T) {
	// Case 1: [1004..1796] has priorDuration = roundCentiseconds(1796) - roundCentiseconds(1004) = 1800 - 1000 = 800ms.
	// Since 800ms >= minFallbackFragmentMS (800ms), it triggers rollback.
	{
		cues := []Cue{
			{ID: 1, Start: 1004, End: 1796, Display: "第一句中文"},
			{ID: 2, Start: 6080, End: 9000, Display: "第二句中文"},
		}
		windows := [][2]int{{1004, 1796}, {6000, 9000}}
		cuts := []int{6080}
		adjusted := adjustCutsForSourceGaps(cuts, cues, windows)
		if adjusted[0] != 6000 {
			t.Fatalf("Case 1 (1004..1796 -> 800ms): expected cut rolled back to 6000, got %d", adjusted[0])
		}

		// Also verify via Render:
		cfg := renderTestConfig()
		bundle := makeBundle(
			[]Cue{
				{ID: 1, Start: 1004, End: 1796, Display: "First clause."},
				{ID: 2, Start: 6000, End: 9000, Display: "Second clause."},
			},
			cues,
		)
		mapping := makeMapping(bundle, []Group{{ENIDs: []int{1, 2}, ZHIDs: []int{1, 2}, Layer: LayerBottom, Origin: OriginModel}})
		output, err := Render(bundle, mapping, cfg, renderTestStyle(), 1920, 1080)
		if err != nil {
			t.Fatal(err)
		}
		events := parseDialogues(t, string(output))
		if len(events) != 2 {
			t.Fatalf("Case 1: expected 2 events, got %d", len(events))
		}
		// In ASS centiseconds: 1004->1000, 1796->1800
		if events[0].start != 1000 || events[0].end != 1800 {
			t.Fatalf("Case 1: expected event[0] [1000..1800], got [%d..%d]", events[0].start, events[0].end)
		}
		if events[1].start != 6000 || events[1].end != 9000 || !strings.Contains(events[1].text, "第二句中文") {
			t.Fatalf("Case 1: expected event[1] [6000..9000] with second cue, got [%d..%d] text=%q", events[1].start, events[1].end, events[1].text)
		}
	}

	// Case 2: [1006..1804] has priorDuration = roundCentiseconds(1804) - roundCentiseconds(1006) = 1800 - 1010 = 790ms.
	// Since 790ms < minFallbackFragmentMS (800ms), it does NOT trigger rollback.
	{
		cues := []Cue{
			{ID: 1, Start: 1006, End: 1804, Display: "第一句中文"},
			{ID: 2, Start: 6080, End: 9000, Display: "第二句中文"},
		}
		windows := [][2]int{{1006, 1804}, {6000, 9000}}
		cuts := []int{6080}
		adjusted := adjustCutsForSourceGaps(cuts, cues, windows)
		if adjusted[0] != 6080 {
			t.Fatalf("Case 2 (1006..1804 -> 790ms): expected cut kept at 6080, got %d", adjusted[0])
		}
	}

	// Case 3: Tail duration boundary at gapEnd = 6000.
	// 3a. cut = 6794: roundCentiseconds(6794) = 6790. tailDuration = 6790 - 6000 = 790ms < 800ms -> rolled back!
	{
		cues := []Cue{
			{ID: 1, Start: 1000, End: 3000, Display: "第一句中文"},
			{ID: 2, Start: 6794, End: 9000, Display: "第二句中文"},
		}
		windows := [][2]int{{1000, 3000}, {6000, 9000}}
		cuts := []int{6794}
		adjusted := adjustCutsForSourceGaps(cuts, cues, windows)
		if adjusted[0] != 6000 {
			t.Fatalf("Case 3a (tail=6794 -> 790ms): expected cut rolled back to 6000, got %d", adjusted[0])
		}
	}
	// 3b. cut = 6804: roundCentiseconds(6804) = 6800. tailDuration = 6800 - 6000 = 800ms >= 800ms -> NOT rolled back!
	{
		cues := []Cue{
			{ID: 1, Start: 1000, End: 3000, Display: "第一句中文"},
			{ID: 2, Start: 6804, End: 9000, Display: "第二句中文"},
		}
		windows := [][2]int{{1000, 3000}, {6000, 9000}}
		cuts := []int{6804}
		adjusted := adjustCutsForSourceGaps(cuts, cues, windows)
		if adjusted[0] != 6804 {
			t.Fatalf("Case 3b (tail=6804 -> 800ms): expected cut kept at 6804, got %d", adjusted[0])
		}
	}
}

// TestRecordedRealSampleSourceGapFlashEliminated runs the full public Render
// and RenderWithWordTimings against the recorded Terminal List S02E01 fixture
// (read-only), verifying that the 80ms flashback at 29:25.43 is eliminated in the
// new rendered output while preserving natural speech gaps and dialogue text.
// This test runs only when FUSIONN_RECORDED_E01_DIR is explicitly provided.
func TestRecordedRealSampleSourceGapFlashEliminated(t *testing.T) {
	sampleDir := os.Getenv("FUSIONN_RECORDED_E01_DIR")
	if sampleDir == "" {
		t.Skip("skipping external recorded sample test: FUSIONN_RECORDED_E01_DIR not set")
	}

	// 1. Verify sources.json SHA256 matches frozen hash.
	sourcesRaw, err := os.ReadFile(filepath.Join(sampleDir, "sources.json"))
	if err != nil {
		t.Fatalf("failed to read sources.json: %v", err)
	}
	hash := sha256.Sum256(sourcesRaw)
	actualHash := hex.EncodeToString(hash[:])
	expectedHash := "780618fa3d215f2b4ccfc33dd9d75bbaf453246e8c99171c4f10d6b61916837e"
	if actualHash != expectedHash {
		t.Fatalf("sources.json SHA256 mismatch: got %s, expected %s", actualHash, expectedHash)
	}

	var bundle Bundle
	if err := json.Unmarshal(sourcesRaw, &bundle); err != nil {
		t.Fatalf("failed to unmarshal sources.json: %v", err)
	}

	mappingRaw, err := os.ReadFile(filepath.Join(sampleDir, "alignment", "mapping.json"))
	if err != nil {
		t.Fatalf("failed to read alignment/mapping.json: %v", err)
	}
	var mapping Mapping
	if err := json.Unmarshal(mappingRaw, &mapping); err != nil {
		t.Fatalf("failed to unmarshal mapping.json: %v", err)
	}

	wordsRaw, err := os.ReadFile(filepath.Join(sampleDir, "word-timings.json"))
	if err != nil {
		t.Fatalf("failed to read word-timings.json: %v", err)
	}
	var words WordTimings
	if err := json.Unmarshal(wordsRaw, &words); err != nil {
		t.Fatalf("failed to unmarshal word-timings.json: %v", err)
	}

	styleRaw, err := os.ReadFile(filepath.Join(sampleDir, "delivered-style.json"))
	if err != nil {
		t.Fatalf("failed to read delivered-style.json: %v", err)
	}
	var style config.ASSStyleConfig
	if err := json.Unmarshal(styleRaw, &style); err != nil {
		t.Fatalf("failed to unmarshal delivered-style.json: %v", err)
	}

	renderSettingsRaw, err := os.ReadFile(filepath.Join(sampleDir, "render-settings.json"))
	if err != nil {
		t.Fatalf("failed to read render-settings.json: %v", err)
	}
	var settings struct {
		Width  int `json:"width"`
		Height int `json:"height"`
		LLM    struct {
			EnChars   int `json:"en_chars"`
			ZhChars   int `json:"zh_chars"`
			EnGapMS   int `json:"en_gap_ms"`
			ZhGapMS   int `json:"zh_gap_ms"`
			FillGapMS int `json:"fill_gap_ms"`
			SpanMS    int `json:"span_ms"`
		} `json:"llm"`
	}
	if err := json.Unmarshal(renderSettingsRaw, &settings); err != nil {
		t.Fatalf("failed to unmarshal render-settings.json: %v", err)
	}

	cfg := config.LLMConfig{
		EnChars:   settings.LLM.EnChars,
		ZhChars:   settings.LLM.ZhChars,
		EnGapMS:   settings.LLM.EnGapMS,
		ZhGapMS:   settings.LLM.ZhGapMS,
		FillGapMS: settings.LLM.FillGapMS,
		SpanMS:    settings.LLM.SpanMS,
	}

	// Render with Render (without word timings)
	outputWithoutWords, err := Render(bundle, mapping, cfg, style, settings.Width, settings.Height)
	if err != nil {
		t.Fatalf("Render failed: %v", err)
	}

	// Render with RenderWithWordTimings
	outputWithWords, err := RenderWithWordTimings(bundle, mapping, cfg, style, settings.Width, settings.Height, words)
	if err != nil {
		t.Fatalf("RenderWithWordTimings failed: %v", err)
	}

	// Save rendered artifacts to a temp dir (never touching sampleDir)
	tmpOutDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpOutDir, "new-rendered.ass"), outputWithoutWords, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmpOutDir, "new-rendered-words.ass"), outputWithWords, 0o644); err != nil {
		t.Fatal(err)
	}

	eventsWithoutWords := parseDialogues(t, string(outputWithoutWords))
	eventsWithWords := parseDialogues(t, string(outputWithWords))

	// Verify both Render and RenderWithWordTimings eliminate the 80ms flashback.
	for name, evs := range map[string][]dialogue{
		"Render":                eventsWithoutWords,
		"RenderWithWordTimings": eventsWithWords,
	} {
		var g305Events []dialogue
		for _, ev := range evs {
			// Window of interest: 29:20 (1760000ms) to 29:30 (1770000ms)
			if ev.start >= 1760000 && ev.end <= 1770000 {
				g305Events = append(g305Events, ev)
			}
		}

		t.Logf("[%s] Events in 29:20..29:30:", name)
		for _, ev := range g305Events {
			t.Logf("  [%d..%d] (dur=%dms) %s", ev.start, ev.end, ev.end-ev.start, ev.text)
		}

		// Assertions on the real sample:
		// 1. "你上岸后" must NOT appear in any event after 1764054ms (29:24.05).
		for _, ev := range evs {
			if ev.start >= 1764054 && strings.Contains(ev.text, "你上岸后") {
				t.Fatalf("[%s] '你上岸后' flashed back after its speech window at [%d..%d]: %s",
					name, ev.start, ev.end, ev.text)
			}
		}

		// 2. The 80ms event [1765430..1765510] must NOT exist.
		for _, ev := range evs {
			if ev.start == 1765430 && ev.end <= 1765520 {
				t.Fatalf("[%s] 80ms flashback event still exists: [%d..%d] %s", name, ev.start, ev.end, ev.text)
			}
		}

		// 3. The natural gap [1764054..1765431] (approx 1377ms) must NOT intersect subtitles.
		for _, ev := range evs {
			if eventOverlapsGap(ev, 1764054, 1765431) {
				t.Fatalf("[%s] natural speech gap was erroneously intersected by: [%d..%d] %s", name, ev.start, ev.end, ev.text)
			}
		}

		// 4. "把你所有的仇恨" starts cleanly at the start of EN336 (1765430ms) without being delayed by old ZH.
		var foundZH302Start bool
		for _, ev := range evs {
			if ev.start == 1765430 && strings.Contains(ev.text, "把你所有的仇恨") {
				foundZH302Start = true
			}
		}
		if !foundZH302Start {
			t.Fatalf("[%s] ZH302 did not start cleanly at 1765430ms", name)
		}
	}
}
