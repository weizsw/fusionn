package subtitlellm

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/fusionn/internal/config"
)

const prototypeRenderRoot = "../../experiments/llm-subtitle-merge/results/dark-matter-s02e07"

func renderTestConfig() config.LLMConfig {
	return config.LLMConfig{
		EnChars:   65,
		ZhChars:   25,
		EnGapMS:   400,
		ZhGapMS:   500,
		SpanMS:    5500,
		FillGapMS: 500,
	}
}

func renderTestStyle() config.ASSStyleConfig {
	return config.ASSStyleConfig{
		Enabled:         true,
		PlayResX:        1920,
		PlayResY:        1080,
		PrimaryFont:     "PingFang SC",
		PrimarySize:     52,
		PrimaryColor:    "&H08EFECEA",
		SecondaryFont:   "Helvetica Neue",
		SecondarySize:   36,
		SecondaryColor:  "&H087BC0E5",
		Outline:         1.4,
		OutlineColor:    "&H30141414",
		Shadow:          1.5,
		BackColor:       "&H80000000",
		MarginV:         32,
		MarginLeft:      80,
		MarginRight:     80,
		Alignment:       2,
		BorderStyle:     1,
		WrapStyle:       "0",
		ScaleX:          100,
		LineHeightRatio: 1.23,
		BlockPadding:    16,
		CollisionGap:    12,
	}
}

func loadPrototypeInputs(t *testing.T) (Bundle, Mapping) {
	t.Helper()
	bundle, err := readBundle(filepath.Join(prototypeRenderRoot, "sources.json"))
	if err != nil {
		t.Fatalf("load prototype sources: %v", err)
	}
	mapping, err := readMapping(filepath.Join(prototypeRenderRoot, "alignment", "mapping.json"))
	if err != nil {
		t.Fatalf("load prototype mapping: %v", err)
	}
	return bundle, mapping
}

func readBundle(path string) (Bundle, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Bundle{}, err
	}
	var bundle Bundle
	if err := jsonUnmarshal(data, &bundle); err != nil {
		return Bundle{}, err
	}
	return bundle, nil
}

func readMapping(path string) (Mapping, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Mapping{}, err
	}
	var mapping Mapping
	if err := jsonUnmarshal(data, &mapping); err != nil {
		return Mapping{}, err
	}
	return mapping, nil
}

type dialogue struct {
	start, end int
	style      string
	text       string
	an         int
	posX, posY float64
	hasPos     bool
}

var (
	dialoguePattern = regexp.MustCompile(`^Dialogue: 0,([^,]+),([^,]+),([^,]+),[^,]*,[^,]*,[^,]*,[^,]*,[^,]*,(.*)$`)
	posParsePattern = regexp.MustCompile(`\\pos\((-?[\d.]+),(-?[\d.]+)\)`)
	anParsePattern  = regexp.MustCompile(`\\an([1-9])`)
)

func parseDialogues(t *testing.T, content string) []dialogue {
	t.Helper()
	var result []dialogue
	for _, line := range strings.Split(content, "\n") {
		match := dialoguePattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		d := dialogue{
			start: parseASSTimeForTest(t, match[1]),
			end:   parseASSTimeForTest(t, match[2]),
			style: match[3],
			text:  match[4],
		}
		if pos := posParsePattern.FindStringSubmatch(d.text); pos != nil {
			d.posX, _ = strconv.ParseFloat(pos[1], 64)
			d.posY, _ = strconv.ParseFloat(pos[2], 64)
			d.hasPos = true
		}
		if an := anParsePattern.FindStringSubmatch(d.text); an != nil {
			d.an, _ = strconv.Atoi(an[1])
		}
		result = append(result, d)
	}
	return result
}

func parseASSTimeForTest(t *testing.T, text string) int {
	t.Helper()
	parts := strings.Split(strings.TrimSpace(text), ":")
	if len(parts) != 3 {
		t.Fatalf("bad ASS time %q", text)
	}
	hours, _ := strconv.Atoi(parts[0])
	minutes, _ := strconv.Atoi(parts[1])
	seconds, err := strconv.ParseFloat(parts[2], 64)
	if err != nil {
		t.Fatalf("bad ASS time %q: %v", text, err)
	}
	return hours*3600000 + minutes*60000 + int(seconds*1000+0.5)
}

// glyphs strips all presentation whitespace and ASS markup added by the
// renderer, leaving only source glyphs.
func glyphs(text string) string {
	text = strings.ReplaceAll(text, `\N`, "")
	text = regexp.MustCompile(`\\r[A-Za-z0-9_ ]*`).ReplaceAllString(text, "")
	text = strings.ReplaceAll(text, `\q2`, "")
	text = posParsePattern.ReplaceAllString(text, "")
	text = anParsePattern.ReplaceAllString(text, "")
	text = strings.ReplaceAll(text, "{", "")
	text = strings.ReplaceAll(text, "}", "")
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, text)
}

func jsonUnmarshal(data []byte, v any) error {
	return decodeJSON(data, v)
}

func TestRenderMatchesPrototypeEpisodes(t *testing.T) {
	bundle, mapping := loadPrototypeInputs(t)
	output, err := Render(bundle, mapping, renderTestConfig(), renderTestStyle(), 0, 0)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := parseDialogues(t, string(output))
	if len(got) != 720 {
		t.Fatalf("rendered %d events, want 720", len(got))
	}

	prototype, err := readPrototypeEvents(filepath.Join(prototypeRenderRoot, "events.json"))
	if err != nil {
		t.Fatalf("read prototype events: %v", err)
	}
	if len(prototype) != len(got) {
		t.Fatalf("prototype events = %d, rendered = %d", len(prototype), len(got))
	}
	en := indexCues(bundle.EN)
	zh := indexCues(bundle.ZH)
	for i, want := range prototype {
		event := got[i]
		if event.start != want.ASSStart || event.end != want.ASSEnd {
			t.Fatalf("event %d time = %d-%d, prototype %d-%d", i, event.start, event.end, want.ASSStart, want.ASSEnd)
		}
		// Held M:N events are top-anchored at runtime (an 7-9); the prototype
		// keeps the source alignment in events.json and applies the hold
		// override only when serializing.
		wantAlignment := want.Alignment
		if want.Strategy == strategyMNReview {
			wantAlignment = (want.Alignment-1)%3 + 7
		}
		if event.an != wantAlignment {
			t.Fatalf("event %d alignment = %d, prototype %d", i, event.an, wantAlignment)
		}
		if !event.hasPos {
			t.Fatalf("event %d has no explicit position", i)
		}
		// Horizontal anchors are exact. Vertical bands use the configurable
		// LineHeightRatio (1.23) instead of the prototype's hardcoded 64/46,
		// so held blocks with two or more lines differ by a few pixels; the
		// same top-anchor rule and stacking order still apply. Single-line
		// blocks have identical heights and must match exactly.
		if event.posX != want.Position[0] {
			t.Fatalf("event %d x = %g, prototype %g", i, event.posX, want.Position[0])
		}
		if want.Strategy != strategyMNReview && event.posY != want.Position[1] {
			t.Fatalf("event %d y = %g, prototype %g", i, event.posY, want.Position[1])
		}
		// Every rendered glyph must occur in the source text of the mapped IDs,
		// in the prototype's Chinese-then-English order.
		expected := ""
		for _, id := range want.ZHIDs {
			expected += zh[id].Display
		}
		for _, id := range want.ENIDs {
			expected += en[id].Display
		}
		if glyphs(event.text) != glyphs(expected) {
			t.Fatalf("event %d text provenance mismatch\n got %q\nwant %q", i, glyphs(event.text), glyphs(expected))
		}
	}
	// Mapping coverage is exactly once per source cue, and every cue is
	// represented in at least one rendered event.
	assertMappingCoverage(t, mapping, bundle)
	rendered := string(output)
	if strings.Count(rendered, "\nDialogue:") != 720 {
		t.Fatalf("expected 720 dialogue lines, got %d", strings.Count(rendered, "Dialogue:"))
	}
}

func TestRenderMNHoldsCompleteChineseInSourceOrder(t *testing.T) {
	bundle, mapping := loadPrototypeInputs(t)
	output, err := Render(bundle, mapping, renderTestConfig(), renderTestStyle(), 0, 0)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	got := parseDialogues(t, string(output))
	zh := indexCues(bundle.ZH)
	prototype, err := readPrototypeEvents(filepath.Join(prototypeRenderRoot, "events.json"))
	if err != nil {
		t.Fatalf("read prototype events: %v", err)
	}
	holdCount := 0
	for i, want := range prototype {
		if want.Strategy != strategyMNReview {
			continue
		}
		holdCount++
		expected := ""
		for _, id := range want.ZHIDs {
			expected += zh[id].Display
		}
		text := got[i].text
		if glyphs(text[:strings.Index(text, `\N{`)]) != glyphs(expected) {
			t.Fatalf("event %d held Chinese = %q, want %q", i, glyphs(text), glyphs(expected))
		}
		if got[i].an < 7 {
			t.Fatalf("event %d hold alignment = %d, want a top anchor", i, got[i].an)
		}
		if !strings.Contains(text, `\q2`) {
			t.Fatalf("event %d hold text lacks explicit \\q2", i)
		}
		if !strings.Contains(text, `\an`) || !strings.Contains(text, `\pos(`) {
			t.Fatalf("event %d hold text lacks explicit alignment/position", i)
		}
	}
	if holdCount == 0 {
		t.Fatal("prototype has no M:N hold events to compare")
	}
}

func TestRenderOneToOneUsesEnglishTiming(t *testing.T) {
	bundle := makeBundle(
		[]Cue{{ID: 1, Start: 1000, End: 3000, Display: "Hello"}},
		[]Cue{{ID: 1, Start: 500, End: 4000, Display: "你好"}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel}})
	output, err := Render(bundle, mapping, renderTestConfig(), renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	events := parseDialogues(t, string(output))
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	if events[0].start != 1000 || events[0].end != 3000 {
		t.Fatalf("time = %d-%d, want 1000-3000", events[0].start, events[0].end)
	}
	assertGlyphs(t, events[0].text, "你好Hello")
}

func TestRenderOneToNChineseInternalCuts(t *testing.T) {
	bundle := makeBundle(
		[]Cue{{ID: 1, Start: 1000, End: 7000, Display: "English line"}},
		[]Cue{
			{ID: 1, Start: 500, End: 3500, Display: strings.Repeat("甲", 20)},
			{ID: 2, Start: 4000, End: 8000, Display: strings.Repeat("乙", 20)},
		},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1, 2}, Layer: LayerBottom, Origin: OriginModel}})
	output, err := Render(bundle, mapping, renderTestConfig(), renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	events := parseDialogues(t, string(output))
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	wantTimes := [][2]int{{1000, 4000}, {4000, 7000}}
	for i, event := range events {
		if event.start != wantTimes[i][0] || event.end != wantTimes[i][1] {
			t.Fatalf("event %d time = %d-%d, want %v", i, event.start, event.end, wantTimes[i])
		}
		assertGlyphs(t, event.text, []string{strings.Repeat("甲", 20), strings.Repeat("乙", 20)}[i]+"English line")
	}
}

func TestRenderOneSidedAndNativeTop(t *testing.T) {
	position := [2]float64{960, 120}
	bundle := makeBundle(
		[]Cue{{ID: 1, Start: 1000, End: 3000, Display: "[phone ringing]"}},
		[]Cue{{ID: 1, Start: 1000, End: 3000, Display: "【三天后】", Style: "Top", Alignment: 8, Top: true, Position: &position}},
	)
	mapping := makeMapping(bundle, []Group{
		{ENIDs: []int{1}, Layer: LayerBottom, Origin: OriginRestored},
		{ZHIDs: []int{1}, Layer: LayerTop, Origin: OriginModel},
	})
	output, err := Render(bundle, mapping, renderTestConfig(), renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	events := parseDialogues(t, string(output))
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	if events[0].style != "Eng" {
		t.Fatalf("one-sided English style = %q, want Eng", events[0].style)
	}
	if events[1].style != "TopDefault" {
		t.Fatalf("native top style = %q, want TopDefault", events[1].style)
	}
	if events[1].posX != 960 || events[1].posY != 120 {
		t.Fatalf("native position = (%g,%g), want (960,120)", events[1].posX, events[1].posY)
	}
}

func TestRenderExplicitBottomPositionFollowsWideGeometry(t *testing.T) {
	// A native bottom-anchored \pos is attached to the encoded picture; on a
	// 2.39:1 frame it moves into the player-added bar exactly like the style
	// margin. Top positions stay frame-relative.
	position := [2]float64{960, 1040}
	topPosition := [2]float64{960, 60}
	bundle := makeBundle(
		[]Cue{
			{ID: 1, Start: 1000, End: 3000, Display: "bottom", Position: &position},
			{ID: 2, Start: 3100, End: 5000, Display: "top", Position: &topPosition, Alignment: 8, Top: true},
		},
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "台词"}},
	)
	mapping := makeMapping(bundle, []Group{
		{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel},
		{ENIDs: []int{2}, Layer: LayerTop, Origin: OriginRestored},
	})
	output, err := Render(bundle, mapping, renderTestConfig(), renderTestStyle(), 3840, 1606)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	events := parseDialogues(t, string(output))
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	// The bottom gap is preserved on the 2160-high reference canvas: a 40px
	// gap at 1080 becomes margin -132.5 there (equivalent to the adapted
	// style margin), so the explicit anchor lands at 1080 - (-132.48).
	if events[0].posY != 1212.48 {
		t.Fatalf("bottom explicit position = %g, want 1212.48", events[0].posY)
	}
	if events[1].posY != 60 {
		t.Fatalf("top explicit position = %g, want 60", events[1].posY)
	}
}

func TestRenderPreservesShortOverlap(t *testing.T) {
	bundle := makeBundle(
		[]Cue{
			{ID: 1, Start: 1000, End: 3000, Display: "First"},
			{ID: 2, Start: 2900, End: 5000, Display: "Second"},
		},
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "台词"}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1, 2}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel}})
	output, err := Render(bundle, mapping, renderTestConfig(), renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	events := parseDialogues(t, string(output))
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	if events[0].end != 3000 || events[1].start != 2900 {
		t.Fatalf("overlap trimmed: %d-%d", events[0].end, events[1].start)
	}
	// Overlapping bands must not collide.
	if events[0].posY <= events[1].posY {
		t.Fatalf("overlapping events share a vertical band: %g and %g", events[0].posY, events[1].posY)
	}
}

func TestRenderRejectsLostSourceAndWrongFingerprint(t *testing.T) {
	bundle := makeBundle(
		[]Cue{{ID: 1, Start: 1000, End: 3000, Display: "a"}, {ID: 2, Start: 3100, End: 5000, Display: "b"}},
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "甲"}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel}})
	if _, err := Render(bundle, mapping, renderTestConfig(), renderTestStyle(), 1920, 1080); err == nil {
		t.Fatal("Render() accepted a mapping that loses source cue 2")
	}
	badFingerprint := mapping
	badFingerprint.SourceFingerprint = "different"
	if _, err := Render(bundle, badFingerprint, renderTestConfig(), renderTestStyle(), 1920, 1080); err == nil {
		t.Fatal("Render() accepted a mapping for a different bundle")
	}
}

func TestRenderGeometryScalesWideFilms(t *testing.T) {
	// On wide encoded frames the reference 16:9 playback canvas is taller, so
	// glyphs grow and the bottom margin shifts (possibly negative) to keep the
	// configured distance above the player-added bottom bar.
	tests := []struct {
		name        string
		width       int
		height      int
		wantPrimary int
		wantMargin  int
	}{
		{name: "4K full frame", width: 3840, height: 2160, wantPrimary: 52, wantMargin: 32},
		{name: "4K scope", width: 3840, height: 1606, wantPrimary: 70, wantMargin: -143},
		{name: "3840x1920 taller", width: 3840, height: 1920, wantPrimary: 59, wantMargin: -32},
		{name: "16:9", width: 1920, height: 1080, wantPrimary: 52, wantMargin: 32},
		{name: "4:3", width: 1440, height: 1080, wantPrimary: 52, wantMargin: 32},
		{name: "unknown dimensions", width: 0, height: 0, wantPrimary: 52, wantMargin: 32},
		{name: "negative dimensions", width: -1, height: -1, wantPrimary: 52, wantMargin: 32},
	}
	bundle := makeBundle(
		[]Cue{{ID: 1, Start: 1000, End: 3000, Display: "Hello"}},
		[]Cue{{ID: 1, Start: 1000, End: 3000, Display: "你好"}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel}})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := Render(bundle, mapping, renderTestConfig(), renderTestStyle(), tt.width, tt.height)
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			content := string(output)
			if !strings.Contains(content, fmt.Sprintf("Style: Default,PingFang SC,%d,", tt.wantPrimary)) {
				t.Fatalf("primary size not adapted to %d:\n%s", tt.wantPrimary, content)
			}
			if got := styleMargin(content, "Default"); got != tt.wantMargin {
				t.Fatalf("margin = %d, want %d:\n%s", got, tt.wantMargin, content)
			}
			events := parseDialogues(t, content)
			if len(events) != 1 {
				t.Fatalf("events = %d, want 1", len(events))
			}
			if events[0].start != 1000 || events[0].end != 3000 {
				t.Fatalf("time = %d-%d, want 1000-3000", events[0].start, events[0].end)
			}
		})
	}
}

func TestRenderWideGeometryKeepsTextAndTiming(t *testing.T) {
	// Geometry adaptation only changes presentation: every event keeps the
	// same text glyphs and timestamps as the 1080p reference render.
	bundle, mapping := loadPrototypeInputs(t)
	base, err := Render(bundle, mapping, renderTestConfig(), renderTestStyle(), 0, 0)
	if err != nil {
		t.Fatalf("Render() reference error = %v", err)
	}
	wide, err := Render(bundle, mapping, renderTestConfig(), renderTestStyle(), 3840, 1606)
	if err != nil {
		t.Fatalf("Render() wide error = %v", err)
	}
	baseEvents := parseDialogues(t, string(base))
	wideEvents := parseDialogues(t, string(wide))
	if len(baseEvents) != len(wideEvents) {
		t.Fatalf("event counts differ: %d vs %d", len(baseEvents), len(wideEvents))
	}
	en := indexCues(bundle.EN)
	zh := indexCues(bundle.ZH)
	for i := range baseEvents {
		if baseEvents[i].start != wideEvents[i].start || baseEvents[i].end != wideEvents[i].end {
			t.Fatalf("event %d timing changed: %d-%d vs %d-%d", i, baseEvents[i].start, baseEvents[i].end, wideEvents[i].start, wideEvents[i].end)
		}
		if glyphs(baseEvents[i].text) != glyphs(wideEvents[i].text) {
			t.Fatalf("event %d text changed by geometry", i)
		}
		if glyphs(wideEvents[i].text) == "" {
			t.Fatalf("event %d lost all text", i)
		}
	}
	_ = en
	_ = zh
}

func TestWrapDisplayHandlesMultibyteEnglish(t *testing.T) {
	// lastSpaceCut used rune indexes while wrapLine sliced bytes; accented and
	// CJK-adjacent Latin text used to be cut mid-rune. Every character must
	// survive and every line must stay within the limit.
	text := "Un café très crème à Paris, où les élèves préfèrent les mêmes idées"
	wrapped := wrapDisplay(text, "en", 20, false)
	// The prototype converts the break space into a line break; compare words.
	if strings.Join(strings.Fields(wrapped), " ") != strings.Join(strings.Fields(text), " ") {
		t.Fatalf("wrapping changed words:\n%q\n%q", wrapped, text)
	}
	for _, line := range strings.Split(wrapped, "\n") {
		if !utf8.ValidString(line) {
			t.Fatalf("line is not valid UTF-8: %q", line)
		}
		if len([]rune(line)) > 20 {
			t.Fatalf("line exceeds 20 runes: %q", line)
		}
	}
	if !strings.Contains(wrapped, "café") || !strings.Contains(wrapped, "très") {
		t.Fatalf("multibyte words were split: %q", wrapped)
	}
}

func TestRenderZeroMarginsAreValid(t *testing.T) {
	style := renderTestStyle()
	style.MarginLeft = 0
	style.MarginRight = 0
	style.MarginV = 0
	bundle := makeBundle(
		[]Cue{{ID: 1, Start: 1000, End: 3000, Display: "Hello"}},
		[]Cue{{ID: 1, Start: 1000, End: 3000, Display: "你好"}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel}})
	output, err := Render(bundle, mapping, renderTestConfig(), style, 1920, 1080)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	content := string(output)
	if !strings.Contains(content, "Style: Default,PingFang SC,52,&H08EFECEA,&H000000FF,&H30141414,&H80000000,0,0,0,0,100,100,0.0,0,1,1.4,1.5,2,0,0,0,1") {
		t.Fatalf("zero margins were replaced by defaults:\n%s", content)
	}
	// Zero bottom margin anchors the block at the canvas bottom (1080).
	events := parseDialogues(t, content)
	if len(events) != 1 || events[0].posY != 1080 {
		t.Fatalf("bottom anchor = %v, want 1080", events)
	}
}

func TestRenderTopMarginMatchesStyleProcessor(t *testing.T) {
	// On a wide frame the adaptive margin is negative; top style text uses the
	// same margin as the style processor (into the player-added top bar), and
	// the visible-band guard allows it. The top block must be visible.
	position := [2]float64{960, 60}
	bundle := makeBundle(
		[]Cue{
			{ID: 1, Start: 1000, End: 3000, Display: "speech"},
			{ID: 2, Start: 3100, End: 5000, Display: "sign", Position: &position, Alignment: 8, Top: true},
		},
		[]Cue{{ID: 1, Start: 1000, End: 5000, Display: "台词"}},
	)
	mapping := makeMapping(bundle, []Group{
		{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel},
		{ENIDs: []int{2}, Layer: LayerTop, Origin: OriginRestored},
	})
	output, err := Render(bundle, mapping, renderTestConfig(), renderTestStyle(), 3840, 1606)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	content := string(output)
	// Style top margin is the adapted margin, identical to Default/Eng.
	if !strings.Contains(content, "Style: TopDefault,PingFang SC,70,") {
		t.Fatalf("top style size not adapted:\n%s", content)
	}
	if styleMargin(content, "TopDefault") != styleMargin(content, "Default") {
		t.Fatalf("top/bottom style margins differ: %d vs %d", styleMargin(content, "TopDefault"), styleMargin(content, "Default"))
	}
	events := parseDialogues(t, content)
	for _, event := range events {
		if event.posY < -1000 || event.posY > 2200 {
			t.Fatalf("event position outside visible playback canvas: %g", event.posY)
		}
	}
	// The native top position stays frame-relative.
	if events[1].posY != 60 {
		t.Fatalf("native top position = %g, want 60", events[1].posY)
	}
}

func TestRenderDisabledStyleUsesNeutralBaseStyles(t *testing.T) {
	style := renderTestStyle()
	style.Enabled = false
	bundle := makeBundle(
		[]Cue{{ID: 1, Start: 1000, End: 3000, Display: "Hello"}},
		[]Cue{{ID: 1, Start: 1000, End: 3000, Display: "你好"}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel}})
	output, err := Render(bundle, mapping, renderTestConfig(), style, 1920, 1080)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	content := string(output)
	// Base names are always required by events, but custom fonts/colors from
	// the disabled config must not leak into the output.
	for _, want := range []string{
		"Style: Default,Arial,52,&H00FFFFFF", "Style: Eng,Arial,36,&H00FFFFFF",
		"Style: Default_1,Arial,36,", "Style: TopDefault,Arial,52,", "Style: TopEng,Arial,36,",
		"PlayResX: 1920", "PlayResY: 1080",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("neutral base style missing %q:\n%s", want, content)
		}
	}
	for _, unwanted := range []string{"PingFang SC", "Helvetica Neue", "&H08EFECEA", "&H087BC0E5"} {
		if strings.Contains(content, unwanted) {
			t.Fatalf("disabled custom style leaked %q into output", unwanted)
		}
	}
}

func TestRenderUsesSharedStyleHeader(t *testing.T) {
	bundle := makeBundle(
		[]Cue{{ID: 1, Start: 1000, End: 3000, Display: "Hello"}},
		[]Cue{{ID: 1, Start: 1000, End: 3000, Display: "你好"}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel}})
	output, err := Render(bundle, mapping, renderTestConfig(), renderTestStyle(), 1920, 1080)
	if err != nil {
		t.Fatalf("Render() error = %v", err)
	}
	content := string(output)
	for _, want := range []string{
		"PlayResX: 1920", "PlayResY: 1080", "ScaledBorderAndShadow: yes", "YCbCr Matrix: TV.709",
		"Style: Default,PingFang SC,52,&H08EFECEA,&H000000FF,&H30141414,&H80000000,0,0,0,0,100,100,0.0,0,1,1.4,1.5,2,80,80,32,1",
		"Style: Eng,Helvetica Neue,36,&H087BC0E5,&H000000FF,&H30141414,&H80000000,0,0,0,0,100,100,0.0,0,1,1.4,1.5,2,80,80,32,1",
		"Style: Default_1,Helvetica Neue,36,",
		"Style: TopDefault,PingFang SC,52,", "Style: TopEng,Helvetica Neue,36,",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("output missing %q", want)
		}
	}
}

func TestRenderRejectsCommandLikeSourceText(t *testing.T) {
	bundle := makeBundle(
		[]Cue{{ID: 1, Start: 1000, End: 3000, Display: `literal \N injection`}},
		[]Cue{{ID: 1, Start: 1000, End: 3000, Display: "你好"}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel}})
	if _, err := Render(bundle, mapping, renderTestConfig(), renderTestStyle(), 1920, 1080); err == nil {
		t.Fatal("Render() accepted literal ASS command syntax")
	}
}

func TestWrapDisplayIsGlyphPreserving(t *testing.T) {
	text := "就在我们抵达之前 其他版本的我们…　感染了这个病毒 并带回了这里"
	wrapped := wrapDisplay(text, "zh", 25, true)
	for _, line := range strings.Split(wrapped, "\n") {
		if len([]rune(line)) > 25 {
			t.Fatalf("line %q exceeds 25 characters", line)
		}
		if strings.HasPrefix(line, "…") {
			t.Fatalf("line starts with closing punctuation: %q", line)
		}
	}
	if strings.ReplaceAll(wrapped, "\n", "") != text {
		t.Fatalf("wrapping changed glyphs: %q", wrapped)
	}
	english := "The quick brown fox jumps over the lazy dog and keeps running far away"
	wrappedEN := wrapDisplay(english, "en", 30, false)
	if strings.Join(strings.Fields(wrappedEN), " ") != strings.Join(strings.Fields(english), " ") {
		t.Fatalf("English wrapping changed words: %q", wrappedEN)
	}
}

func styleMargin(content, name string) int {
	for _, line := range strings.Split(content, "\n") {
		if !strings.HasPrefix(line, "Style: "+name+",") {
			continue
		}
		fields := strings.Split(line, ",")
		if len(fields) < 22 {
			return -1
		}
		value, _ := strconv.Atoi(fields[21])
		return value
	}
	return -1
}

func assertGlyphs(t *testing.T, text, want string) {
	t.Helper()
	if glyphs(text) != glyphs(want) {
		t.Fatalf("text glyphs = %q, want %q", glyphs(text), glyphs(want))
	}
}

func makeBundle(en, zh []Cue) Bundle {
	for i := range en {
		en[i].OriginalStart = en[i].Start
		en[i].OriginalEnd = en[i].End
		if en[i].Raw == "" {
			en[i].Raw = en[i].Display
		}
		if en[i].Plain == "" {
			en[i].Plain = en[i].Display
		}
		if en[i].Alignment == 0 {
			en[i].Alignment = 2
		}
	}
	for i := range zh {
		zh[i].OriginalStart = zh[i].Start
		zh[i].OriginalEnd = zh[i].End
		if zh[i].Raw == "" {
			zh[i].Raw = zh[i].Display
		}
		if zh[i].Plain == "" {
			zh[i].Plain = zh[i].Display
		}
		if zh[i].Alignment == 0 {
			zh[i].Alignment = 2
		}
	}
	bundle := Bundle{Version: Version, EN: en, ZH: zh}
	fingerprint, err := bundleDigest(bundle)
	if err != nil {
		panic(err)
	}
	bundle.Fingerprint = fingerprint
	return bundle
}

func makeMapping(bundle Bundle, groups []Group) Mapping {
	mapping := Mapping{
		Version:           Version,
		SourceFingerprint: bundle.Fingerprint,
		Groups:            groups,
	}
	fingerprint, err := mappingDigest(mapping)
	if err != nil {
		panic(err)
	}
	mapping.Fingerprint = fingerprint
	return mapping
}

func indexCues(cues []Cue) map[int]Cue {
	index := make(map[int]Cue, len(cues))
	for _, cue := range cues {
		index[cue.ID] = cue
	}
	return index
}

func assertMappingCoverage(t *testing.T, mapping Mapping, bundle Bundle) {
	t.Helper()
	seen := map[string]map[int]int{"en": {}, "zh": {}}
	for _, group := range mapping.Groups {
		for _, id := range group.ENIDs {
			seen["en"][id]++
		}
		for _, id := range group.ZHIDs {
			seen["zh"][id]++
		}
	}
	for lang, cues := range map[string][]Cue{"en": bundle.EN, "zh": bundle.ZH} {
		for _, cue := range cues {
			if seen[lang][cue.ID] != 1 {
				t.Fatalf("%s cue %d mapped %d times, want exactly once", lang, cue.ID, seen[lang][cue.ID])
			}
		}
		if len(seen[lang]) != len(cues) {
			t.Fatalf("%s mapping has %d distinct IDs, sources have %d", lang, len(seen[lang]), len(cues))
		}
	}
}
