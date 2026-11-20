package subtitlellm

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/fusionn/internal/config"
)

const prototypeRenderRoot = "testdata/dark-matter-s02e07"

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

func TestRenderEpisodePreservesSourceTextAndDisplayIntervals(t *testing.T) {
	bundle, mapping := loadPrototypeInputs(t)
	output, err := Render(bundle, mapping, renderTestConfig(), renderTestStyle(), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	got := parseDialogues(t, string(output))
	prototype, err := readPrototypeEvents(filepath.Join(prototypeRenderRoot, "events.json"))
	if err != nil {
		t.Fatal(err)
	}
	var before, after [][2]int
	for _, event := range prototype {
		before = append(before, [2]int{event.ASSStart, event.ASSEnd})
	}
	var english, chinese strings.Builder
	previousEN, previousZH := "", ""
	for _, event := range got {
		after = append(after, [2]int{event.start, event.end})
		if event.hasPos {
			t.Fatal("ordinary speech must retain the player-positioned style")
		}
		primary, secondary, bilingual := strings.Cut(event.text, `\N{\rEng}`)
		if !bilingual {
			primary, secondary, bilingual = strings.Cut(event.text, `\N{\rTopEng}`)
		}
		if !bilingual {
			if event.style == "Default_1" || event.style == "Eng" || event.style == "TopEng" {
				primary, secondary = "", event.text
			} else {
				primary = event.text
			}
		}
		z, e := glyphs(primary), glyphs(secondary)
		if z != previousZH {
			chinese.WriteString(z)
		}
		if e != previousEN {
			english.WriteString(e)
		}
		previousZH, previousEN = z, e
	}
	// Rules A and C move ends only: a screen may grow to the short-screen
	// reading floor or to the next screen of the same region, and rule A may
	// close a blank shorter than 100ms between two of them, which joins their
	// intervals. Nothing may be lost, shortened or started later.
	if failure := reasonForIntervalChange(before, after); failure != "" {
		t.Fatalf("splitting changed the episode's occupied display intervals: %s", failure)
	}
	for lang, track := range map[string]struct {
		cues []Cue
		text string
	}{
		"en": {bundle.EN, english.String()}, "zh": {bundle.ZH, chinese.String()},
	} {
		for _, cue := range track.cues {
			if !strings.Contains(track.text, glyphs(cue.Display)) {
				t.Fatalf("%s cue %d lost text or source order: %q", lang, cue.ID, cue.Display)
			}
		}
	}
	assertMappingCoverage(t, mapping, bundle)
}

func TestRenderMNRetainsShortChineseInSourceOrder(t *testing.T) {
	bundle := makeBundle(
		[]Cue{{ID: 1, Start: 1000, End: 4000, Display: "The first complete sentence."}, {ID: 2, Start: 4000, End: 7000, Display: "The second complete sentence."}},
		[]Cue{{ID: 1, Start: 900, End: 3900, Display: "完整的第一句"}, {ID: 2, Start: 4000, End: 7100, Display: "第二句"}},
	)
	mapping := makeMapping(bundle, []Group{{ENIDs: []int{1, 2}, ZHIDs: []int{1, 2}, Layer: LayerBottom, Origin: OriginModel}})
	output, err := Render(bundle, mapping, renderTestConfig(), renderTestStyle(), 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	events := parseDialogues(t, string(output))
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	for i, event := range events {
		assertGlyphs(t, event.text, "完整的第一句第二句"+bundle.EN[i].Display)
		if event.hasPos {
			t.Fatal("held short text must keep the ordinary style anchor")
		}
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
	// Zero bottom margin anchors the block at the canvas bottom via Default style.
	events := parseDialogues(t, content)
	if len(events) != 1 {
		t.Fatalf("bottom events = %v, want 1", events)
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
		if event.hasPos && (event.posY < -1000 || event.posY > 2200) {
			t.Fatalf("event position outside visible playback canvas: %g", event.posY)
		}
	}
	// The native top position stays frame-relative.
	if !events[1].hasPos || events[1].posY != 60 {
		t.Fatalf("native top position = %g (hasPos=%v), want 60", events[1].posY, events[1].hasPos)
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
		"Style: TopDefault,Arial,52,", "Style: TopEng,Arial,36,",
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

func TestRenderSkipsFormatOnlySourceEvents(t *testing.T) {
	for _, tc := range []struct {
		name, enText, zhText, want string
	}{
		{name: "English position tag", enText: `{\an8}`},
		{name: "Chinese position tag", zhText: `{\an8}`},
		{name: "both languages", enText: `{\an8}`, zhText: `<i></i>`},
		{name: "visible Chinese", enText: `{\an8}`, zhText: "真实字幕", want: "真实字幕"},
		{name: "visible English", enText: "Actual dialogue", zhText: `{\an8}`, want: "Actual dialogue"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			enPath, zhPath := filepath.Join(dir, "en.srt"), filepath.Join(dir, "zh.srt")
			writeFile(t, enPath, "1\n00:00:01,000 --> 00:00:03,000\nHello\n")
			writeFile(t, zhPath, "1\n00:00:01,000 --> 00:00:03,000\n你好\n")
			group := Group{Layer: LayerTop, Origin: OriginModel}
			for _, track := range []struct{ path, text string }{{enPath, tc.enText}, {zhPath, tc.zhText}} {
				if track.text == "" {
					continue
				}
				f, err := os.OpenFile(track.path, os.O_APPEND|os.O_WRONLY, 0600)
				if err != nil {
					t.Fatal(err)
				}
				_, err = fmt.Fprintf(f, "\n2\n00:00:53,387 --> 00:00:55,221\n%s\n", track.text)
				closeErr := f.Close()
				if err != nil || closeErr != nil {
					t.Fatalf("append source: write=%v close=%v", err, closeErr)
				}
				if track.path == enPath {
					group.ENIDs = []int{2}
				} else {
					group.ZHIDs = []int{2}
				}
			}
			bundle, err := LoadSources(enPath, zhPath)
			if err != nil {
				t.Fatal(err)
			}
			mapping := makeMapping(bundle, []Group{
				{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel},
				group,
			})
			fingerprint := bundle.Fingerprint
			data, err := Render(bundle, mapping, renderTestConfig(), renderTestStyle(), 1920, 1080)
			if err != nil {
				t.Fatalf("Render() error = %v", err)
			}
			events := parseDialogues(t, string(data))
			wantEvents := 1
			if tc.want != "" {
				wantEvents++
			}
			if len(events) != wantEvents {
				t.Fatalf("event count = %d, want %d", len(events), wantEvents)
			}
			if events[0].start != 1000 || events[0].end != 3000 {
				t.Fatalf("visible event timing changed: %+v", events[0])
			}
			assertGlyphs(t, events[0].text, "你好Hello")
			if tc.want != "" {
				assertGlyphs(t, events[1].text, tc.want)
				if events[1].start != 53390 || events[1].end != 55220 {
					t.Fatalf("one-sided timing changed: %+v", events[1])
				}
			}
			got, err := bundleDigest(bundle)
			if err != nil || got != fingerprint || mapping.SourceFingerprint != fingerprint {
				t.Fatalf("source fingerprint changed: got=%s want=%s err=%v", got, fingerprint, err)
			}
		})
	}
}

func TestLayoutRejectsLostVisibleSourceText(t *testing.T) {
	empty := ""
	blocks := []block{{start: 1000, end: 3000, enIDs: []int{1}, alignment: 2, enText: &empty}}
	en := map[int]Cue{1: {ID: 1, Plain: "Actual dialogue", Display: "Actual dialogue"}}
	geometry := renderGeometry{canvasWidth: 1920, canvasHeight: 1080, enFontSize: 36, lineHeight: 1.23, blockPadding: 16, marginV: 32, enChars: 65, zhChars: 25}
	var writer assWriter
	events, err := layoutBlocks(blocks, en, nil, geometry, func(_ block, start, end int, text, style string, align int, x, y float64, wrap string, hasPos bool) error {
		return writer.writeDialogue(start, end, style, text, align, x, y, wrap, hasPos)
	})
	if err == nil || !strings.Contains(err.Error(), "empty dialogue event") {
		t.Fatalf("lost source text error = %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("rejected event returned %d layout records", len(events))
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

// reasonForIntervalChange explains why the emitted display intervals differ from
// the prototype events, or returns "" when only rules A and C moved ends. Both
// rules move end times only, so a lost source start, a moved start, and an end
// past the reading floor or past the next screen must still fail.
func reasonForIntervalChange(want, got [][2]int) string {
	// Every source window must stay fully occupied: the renderer may split one
	// window into contiguous slices, but it may not drop any of its display time.
	for _, source := range want {
		covered := joinIntervals(overlapping(got, source))
		if !covers(covered, source) {
			return fmt.Sprintf("source window %v is no longer fully shown: covered by %v", source, covered)
		}
	}
	// An event may only extend past its source window for rules C and A.
	order := make([]int, 0, len(got))
	for _, interval := range got {
		order = append(order, interval[0])
	}
	sort.Ints(order)
	for _, interval := range got {
		var source [2]int
		found := false
		for _, candidate := range want {
			if candidate[0] <= interval[0] && (!found || candidate[0] > source[0]) {
				source, found = candidate, true
			}
		}
		if !found {
			return fmt.Sprintf("emitted interval %v starts before any source window", interval)
		}
		if interval[1] <= source[1] {
			continue
		}
		// Rule C: the end may reach the reading floor.
		if interval[1] <= source[0]+shortScreenMS {
			continue
		}
		// Rule A: the end may move to the next start, closing a blank shorter
		// than 100ms.
		next := 0
		for _, at := range order {
			if at > interval[0] {
				next = at
				break
			}
		}
		if next > 0 && interval[1] == next && next-source[1] < gapCloseMS {
			continue
		}
		return fmt.Sprintf("emitted interval %v runs past the end rules A and C allow for source window %v", interval, source)
	}
	return ""
}

// overlapping returns the intervals that share time with window.
func overlapping(intervals [][2]int, window [2]int) [][2]int {
	var result [][2]int
	for _, interval := range intervals {
		if interval[0] < window[1] && window[0] < interval[1] {
			result = append(result, interval)
		}
	}
	return result
}

// covers reports whether the joined intervals show every millisecond of window.
func covers(joined [][2]int, window [2]int) bool {
	at := window[0]
	for _, interval := range joined {
		if interval[0] > at {
			return false
		}
		if interval[1] > at {
			at = interval[1]
		}
		if at >= window[1] {
			return true
		}
	}
	return at >= window[1]
}

// joinIntervals merges touching or overlapping intervals, so the occupied
// display time of one language is compared as a whole.
func joinIntervals(intervals [][2]int) [][2]int {
	sorted := append([][2]int(nil), intervals...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i][0] < sorted[j][0] })
	var result [][2]int
	for _, interval := range sorted {
		if len(result) == 0 || result[len(result)-1][1] < interval[0] {
			result = append(result, interval)
			continue
		}
		if interval[1] > result[len(result)-1][1] {
			result[len(result)-1][1] = interval[1]
		}
	}
	return result
}

// TestIntervalVerifierRejectsUnexplainedExtension keeps the check above honest:
// an end moved for a reason other than rules A and C must still fail.
func TestIntervalVerifierRejectsUnexplainedExtension(t *testing.T) {
	want := [][2]int{{1000, 2000}, {5000, 6000}}
	// A gap of 3000ms is neither a reading floor nor a short blank, so nothing
	// may extend the first window.
	got := [][2]int{{1000, 2500}, {5000, 6000}}
	if reason := reasonForIntervalChange(want, got); reason == "" {
		t.Fatal("an unexplained end extension passed verification")
	}
	// A start that moves must fail even when the end is fine.
	got = [][2]int{{1200, 2000}, {5000, 6000}}
	if reason := reasonForIntervalChange(want, got); reason == "" {
		t.Fatal("a moved start passed verification")
	}
	// The reading floor alone is allowed.
	got = [][2]int{{1000, 2200}, {5000, 6000}}
	if reason := reasonForIntervalChange(want, got); reason != "" {
		t.Fatalf("the reading floor was rejected: %s", reason)
	}
	// Closing a blank shorter than 100ms is allowed.
	want = [][2]int{{1000, 2000}, {2050, 3000}}
	got = [][2]int{{1000, 2050}, {2050, 3000}}
	if reason := reasonForIntervalChange(want, got); reason != "" {
		t.Fatalf("closing a 50ms blank was rejected: %s", reason)
	}
	// A window shortened to a token length must still fail: the guarantee is on
	// the occupied display time, not on the event count.
	want = [][2]int{{1000, 2000}, {5000, 6000}}
	got = [][2]int{{1000, 1010}, {5000, 5010}}
	if reason := reasonForIntervalChange(want, got); reason == "" {
		t.Fatal("dropping nearly all display time passed verification")
	}
	// An extra event after every source window must fail too.
	got = append(got, [2]int{9000, 9010})
	if reason := reasonForIntervalChange(want, got); reason == "" {
		t.Fatal("an extra event outside every source window passed verification")
	}
}
