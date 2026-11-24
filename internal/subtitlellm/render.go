package subtitlellm

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/fusionn/internal/config"
)

// HeaderMarker identifies a native LLM-rendered ASS file. It is written into
// Script Info so downstream processors can preserve already-final styles and
// explicit positions across process boundaries.
const HeaderMarker = "; fusionn-llm-styled"

// MetadataLLMStyled is the ProcessingContext.Metadata key set by the pipeline
// when the native renderer produced the merged subtitle.
const MetadataLLMStyled = "llm_styled"

// Rendering strategies. They mirror the prototype's build_blocks names so the
// event audit remains comparable.
const (
	strategyOneToOne     = "1:1_english_anchor"
	strategyShortJoin    = "short_join"
	strategyOneToN       = "1:N_chinese_internal_cuts"
	strategyOneToNReview = "1:N_invalid_cuts_full_text_review"
	strategyNToOne       = "N:1_english_slices"
	strategyMNReview     = "M:N_chinese_hold_review"
	strategyOneSided     = "one_sided"
	strategyMNSourceCuts = "M:N_source_cuts"
	strategyTimedSplit   = "long_cue_time_slices"
	strategyScreenPlan   = "screen_plan"
)

// block is one rendered bilingual event before positioning.
type block struct {
	start       int
	end         int
	enIDs       []int
	zhIDs       []int
	group       int
	strategy    string
	top         bool
	sourceTop   bool
	alignment   int
	position    *[2]float64
	enText      *string
	zhText      *string
	zhFlattened bool
	enFlattened bool
	isLyric     bool
	// recheckApplied marks blocks that were created by semantic recheck.
	recheckApplied bool
	// uncertainFallback marks blocks that were created by uncertain semantic recheck fallback.
	uncertainFallback bool
	// enSeparatedRole and zhSeparatedRole mark text overrides that hold a role
	// separated out of a mixed lyric cue. Overrides produced by temporal
	// projection, holds, short joins or layout keep their source cue's speaker
	// evidence instead, so an ordinary override is never mistaken for evidence
	// that the source's speakers were separated.
	enSeparatedRole bool
	zhSeparatedRole bool
}

// Render produces bilingual ASS from an immutable source bundle and ID mapping.
// It keeps source text and uses proportional timing for long cues. The mapping
// chooses which cues share an event. The configured ASS canvas controls placement;
// video dimensions provide the reference playback geometry.
func Render(bundle Bundle, mapping Mapping, llmcfg config.LLMConfig, style config.ASSStyleConfig, width, height int) ([]byte, error) {
	return RenderWithWordTimings(bundle, mapping, llmcfg, style, width, height, nil)
}

// RenderWithWordTimings uses optional audio-derived English word times for
// long cues. Missing or invalid times use proportional slices of source text.
func RenderWithWordTimings(bundle Bundle, mapping Mapping, llmcfg config.LLMConfig, style config.ASSStyleConfig, width, height int, words WordTimings) ([]byte, error) {
	data, _, _, err := renderPlanWithAudits(bundle, mapping, llmcfg, style, width, height, words)
	return data, err
}

// RenderWithScreenPlans renders like RenderWithWordTimings and also returns the
// screen-plan statistics, which only the renderer can count because it validates
// each plan. A nil result means the mapping carried no plans.
func RenderWithScreenPlans(bundle Bundle, mapping Mapping, llmcfg config.LLMConfig, style config.ASSStyleConfig, width, height int, words WordTimings) ([]byte, *ScreenPlanAudit, error) {
	data, _, plans, err := renderPlanWithAudits(bundle, mapping, llmcfg, style, width, height, words)
	return data, plans, err
}

func renderWithAudits(bundle Bundle, mapping Mapping, llmcfg config.LLMConfig, style config.ASSStyleConfig, width, height int, words WordTimings) ([]byte, []renderedEvent, error) {
	data, events, _, err := renderPlanWithAudits(bundle, mapping, llmcfg, style, width, height, words)
	return data, events, err
}

func renderPlanWithAudits(bundle Bundle, mapping Mapping, llmcfg config.LLMConfig, style config.ASSStyleConfig, width, height int, words WordTimings) ([]byte, []renderedEvent, *ScreenPlanAudit, error) {
	if err := validateRenderInputs(bundle, mapping, llmcfg, style); err != nil {
		return nil, nil, nil, err
	}

	en, zh, err := cueIndex(bundle)
	if err != nil {
		return nil, nil, nil, err
	}
	checks := newSplitChecks(mapping.SplitChecks, false)
	blocks := splitLongBlocks(buildBlocks(mapping, en, zh, llmcfg, words, checks), en, zh, llmcfg, words, checks)
	effective := adaptedRenderStyle(renderBaseStyle(style), width, height)
	geometry := renderGeometry{
		zhFontSize:   float64(effective.PrimarySize),
		enFontSize:   float64(effective.SecondarySize),
		lineHeight:   effective.LineHeightRatio,
		blockPadding: float64(effective.BlockPadding),
		collisionGap: float64(effective.CollisionGap),
		marginV:      float64(effective.MarginV),
		marginL:      float64(effective.MarginLeft),
		marginR:      float64(effective.MarginRight),
		canvasWidth:  float64(effective.PlayResX),
		canvasHeight: float64(effective.PlayResY),
		enChars:      llmcfg.EnChars,
		zhChars:      llmcfg.ZhChars,
		frameWidth:   width,
		frameHeight:  height,
	}
	// Screen plans replace a group's blocks before any spatial policy runs, so
	// on-screen-text grouping and top/bottom placement see the planned layout.
	blocks, planAudit := applyScreenPlans(blocks, mapping, en, zh, words, planGeometry{
		enChars: llmcfg.EnChars,
		zhChars: llmcfg.ZhChars,
	})
	blocks = applySpatialPolicies(blocks, mapping, en, zh, geometry, effective, checks)
	yieldToChineseSingles(blocks, mapping, zh)
	// Rule C runs before rule A: a short screen first grows to the reading floor,
	// then a blank that is still shorter than 100ms closes to the next start.
	extendShortScreens(blocks)
	closeTimingGaps(blocks)
	sort.SliceStable(blocks, func(i, j int) bool {
		a, b := blocks[i], blocks[j]
		if a.start != b.start {
			return a.start < b.start
		}
		if a.end != b.end {
			return a.end < b.end
		}
		return a.group < b.group
	})
	if err := verifySourceCoverage(blocks, en, zh); err != nil {
		return nil, nil, nil, err
	}
	var out assWriter
	out.writeHeader(effective)
	audits, err := layoutBlocks(blocks, en, zh, geometry, func(b block, start, end int, text string, styleName string, align int, x, y float64, wrap string, hasPos bool) error {
		return out.writeDialogue(start, end, styleName, text, align, x, y, wrap, hasPos)
	})
	if err != nil {
		return nil, nil, nil, err
	}
	return []byte(out.String()), audits, planAudit, nil
}

// renderBaseStyle returns the style used to render events. The renderer always
// writes the base style names (Default, Eng, TopDefault, TopEng)
// because events reference them, but custom styling is optional: with
// ass_style.enabled=false it uses neutral structural defaults instead of the
// configured fonts, colors and typography. The canvas still comes from the
// shared config.
func renderBaseStyle(style config.ASSStyleConfig) config.ASSStyleConfig {
	if style.Enabled {
		return style
	}
	neutral := config.ASSStyleConfig{
		PlayResX: style.PlayResX, PlayResY: style.PlayResY,
		PrimaryFont: "Arial", PrimarySize: 52, PrimaryColor: "&H00FFFFFF",
		SecondaryFont: "Arial", SecondarySize: 36, SecondaryColor: "&H00FFFFFF",
		Outline: 1, OutlineColor: "&H00000000", Shadow: 0, BackColor: "&H00000000",
		MarginV: 32, MarginLeft: 80, MarginRight: 80, Alignment: 2, BorderStyle: 1,
		WrapStyle: "0", ScaleX: 100, Spacing: 0, LineHeightRatio: 1.23,
		BlockPadding: 16, CollisionGap: 12,
	}
	if neutral.PlayResX <= 0 || neutral.PlayResY <= 0 {
		neutral.PlayResX, neutral.PlayResY = 1920, 1080
	}
	return neutral
}

// adaptedRenderStyle applies the shared television geometry to the renderer's
// canvas: on wide encoded frames the reference 16:9 playback canvas is taller
// than the frame, so glyphs grow and the shared MarginV becomes a (possibly
// negative) script-coordinate offset that keeps the configured distance from
// the playback canvas top/bottom, including player-added bars. The same margin
// is used by Default/Eng and TopDefault/TopEng, matching the style processor.
// With ass_style.enabled=false, neutral base styles are adapted instead of the
// configured appearance. Unknown dimensions (0,0) use the base style unchanged.
func adaptedRenderStyle(style config.ASSStyleConfig, width, height int) config.ASSStyleConfig {
	if width <= 0 || height <= 0 || style.PlayResX <= 0 || style.PlayResY <= 0 {
		return style
	}
	scale := ReferencePlaybackCanvasScale(width, height)
	if scale == 1 || scale <= 0 {
		return style
	}
	style.PrimarySize = int(math.Round(float64(style.PrimarySize) * scale))
	style.SecondarySize = int(math.Round(float64(style.SecondarySize) * scale))
	style.MarginV = AdaptedMarginV(style.MarginV, width, height, style.PlayResY)
	if style.MarginLeft > 0 {
		style.MarginLeft = int(math.Round(float64(style.MarginLeft) * scale))
	}
	if style.MarginRight > 0 {
		style.MarginRight = int(math.Round(float64(style.MarginRight) * scale))
	}
	return style
}

// ReferencePlaybackCanvasScale is the shared 16:9 output-canvas scale: 1 for
// frames at least as tall as width*9/16, otherwise the ratio needed to fit the
// frame inside the reference canvas (wide/letterboxed films).
func ReferencePlaybackCanvasScale(width, height int) float64 {
	canvasHeight := float64(width) * 9 / 16
	if float64(height) >= canvasHeight {
		return 1
	}
	return canvasHeight / float64(height)
}

// AdaptedMarginV preserves the configured distance above the bottom of the
// centered 16:9 playback canvas when the encoded frame is wider than 16:9.
func AdaptedMarginV(configuredMarginV, width, height, playResY int) int {
	scale := ReferencePlaybackCanvasScale(width, height)
	if scale == 1 {
		return configuredMarginV
	}
	canvasHeight := float64(height) * scale
	bottomBar := (canvasHeight - float64(height)) / 2
	verticalScale := float64(height) / float64(playResY)
	targetBottomGap := float64(configuredMarginV) * canvasHeight / float64(playResY)
	return int(math.Round((targetBottomGap - bottomBar) / verticalScale))
}

// renderGeometry holds the typography and canvas values that affect wrapping
// and vertical band allocation. Sizes are script coordinates.
type renderGeometry struct {
	zhFontSize   float64
	enFontSize   float64
	lineHeight   float64
	blockPadding float64
	collisionGap float64
	marginV      float64
	marginL      float64
	marginR      float64
	canvasWidth  float64
	canvasHeight float64
	enChars      int
	zhChars      int
	frameWidth   int
	frameHeight  int
}

// visibleMinY and visibleMaxY bound the script-coordinate bands that map onto
// the centered 16:9 playback canvas. With a 16:9 or taller encoded frame they
// are the canvas itself; on wide frames they extend into the player-added top
// and bottom bars by the same amount as the adaptive margin.
func (g renderGeometry) visibleMinY() float64 {
	topBar, _ := g.playbackBars()
	return -topBar
}

func (g renderGeometry) visibleMaxY() float64 {
	_, bottomBar := g.playbackBars()
	return g.canvasHeight + bottomBar
}

// playbackBars returns the top and bottom player-added bar heights in script
// coordinates.
func (g renderGeometry) playbackBars() (float64, float64) {
	if g.frameWidth <= 0 || g.frameHeight <= 0 {
		return 0, 0
	}
	scale := ReferencePlaybackCanvasScale(g.frameWidth, g.frameHeight)
	if scale <= 1 {
		return 0, 0
	}
	bar := (float64(g.frameHeight)*scale - float64(g.frameHeight)) / 2
	verticalScale := float64(g.frameHeight) / g.canvasHeight
	if verticalScale <= 0 {
		return 0, 0
	}
	return bar / verticalScale, bar / verticalScale
}

// adaptPositionY maps an explicit bottom-anchored position with the same
// television geometry as the style margin: its distance from the bottom of the
// centered 16:9 playback canvas stays proportional, so wide films move the
// anchor into the player-added bottom bar. Top and center positions stay
// frame-relative: the frame top and center coincide with the canvas, and native
// top signs must not move because of letterboxing.
func (g renderGeometry) adaptPositionY(y float64, alignment int) float64 {
	if g.frameWidth <= 0 || g.frameHeight <= 0 || alignment > 3 {
		return y
	}
	scale := ReferencePlaybackCanvasScale(g.frameWidth, g.frameHeight)
	if scale == 1 {
		return y
	}
	canvasHeight := float64(g.frameHeight) * scale
	bottomBar := (canvasHeight - float64(g.frameHeight)) / 2
	verticalScale := float64(g.frameHeight) / g.canvasHeight
	gap := g.canvasHeight - y
	adaptedGap := (gap*canvasHeight/g.canvasHeight - bottomBar) / verticalScale
	return g.canvasHeight - adaptedGap
}

func (g renderGeometry) zhLineHeight() float64 { return g.zhFontSize * g.lineHeight }
func (g renderGeometry) enLineHeight() float64 { return g.enFontSize * g.lineHeight }

// widthFactor converts measured glyph width into script x units on the actual
// frame. Glyph advances only scale with the vertical style scale
// (frameHeight/PlayResY), while a script x unit spans frameWidth/PlayResX
// pixels, so a wide frame would otherwise overestimate the occupied width.
// Without a frame size nothing is known and the factor is 1.
func (g renderGeometry) widthFactor() float64 {
	if g.frameWidth <= 0 || g.frameHeight <= 0 || g.canvasWidth <= 0 || g.canvasHeight <= 0 {
		return 1
	}
	return (float64(g.frameHeight) / g.canvasHeight) / (float64(g.frameWidth) / g.canvasWidth)
}

func validateRenderInputs(bundle Bundle, mapping Mapping, llmcfg config.LLMConfig, style config.ASSStyleConfig) error {
	if bundle.Version != Version {
		return fmt.Errorf("unsupported source bundle version %d", bundle.Version)
	}
	if bundle.Fingerprint == "" {
		return errors.New("source bundle fingerprint is missing")
	}
	if mapping.SourceFingerprint != bundle.Fingerprint {
		return errors.New("mapping belongs to a different source bundle")
	}
	if len(mapping.Groups) == 0 {
		return errors.New("mapping has no groups")
	}
	if llmcfg.EnChars <= 0 || llmcfg.ZhChars <= 0 {
		return errors.New("character limits must be positive")
	}
	if llmcfg.EnGapMS < 0 || llmcfg.ZhGapMS < 0 || llmcfg.FillGapMS < 0 || llmcfg.SpanMS <= 0 {
		return errors.New("gap limits must be non-negative and span must be positive")
	}
	if style.PlayResX <= 0 || style.PlayResY <= 0 {
		return errors.New("play resolution must be positive")
	}
	if style.PrimarySize <= 0 || style.SecondarySize <= 0 {
		return errors.New("font sizes must be positive")
	}
	return nil
}

func cueIndex(bundle Bundle) (map[int]Cue, map[int]Cue, error) {
	en, err := indexTrack(bundle.EN)
	if err != nil {
		return nil, nil, fmt.Errorf("en source: %w", err)
	}
	zh, err := indexTrack(bundle.ZH)
	if err != nil {
		return nil, nil, fmt.Errorf("zh source: %w", err)
	}
	return en, zh, nil
}

func indexTrack(cues []Cue) (map[int]Cue, error) {
	index := make(map[int]Cue, len(cues))
	expected := 1
	for _, cue := range cues {
		if cue.ID != expected {
			return nil, fmt.Errorf("duplicate or nonsequential source ID %d, want %d", cue.ID, expected)
		}
		if cue.Start < 0 || cue.Start >= cue.End {
			return nil, fmt.Errorf("illegal source time for cue %d", cue.ID)
		}
		index[cue.ID] = cue
		expected++
	}
	return index, nil
}

// verifySourceCoverage ensures the renderer never drops a source cue. Held
// text repeats intended, but every source ID must occur in at least one event.
const (
	// singleOverlapMS is the longest overlap between a Chinese-only line and a
	// paired line that counts as two timings of one pause rather than speech
	// at the same time.
	singleOverlapMS = 500
	// minYieldedPairMS is the shortest a paired line may become when it
	// yields to a Chinese-only line.
	minYieldedPairMS = 1000
)

// assCentis converts a source time to the centiseconds the subtitle file shows,
// so every threshold below matches what a viewer sees.
func assCentis(ms int) int { return roundCentiseconds(ms) / 10 }

// closeTimingGaps closes a blank shorter than gapCloseMS between two
// consecutive screens of one region, which the player would otherwise show as
// a one-frame flicker. Only the earlier end moves; starts never change.
func closeTimingGaps(blocks []block) {
	for _, top := range []bool{false, true} {
		order := regionOrder(blocks, top)
		for k := 1; k < len(order); k++ {
			previous, next := &blocks[order[k-1]], blocks[order[k]]
			if previous.uncertainFallback || next.uncertainFallback || previous.recheckApplied {
				continue
			}
			gap := assCentis(next.start) - assCentis(previous.end)
			if gap > 0 && gap < gapCloseMS/10 {
				previous.end = next.start
			}
		}
	}
}

// regionOrder returns the indexes of one region's blocks in start order.
func regionOrder(blocks []block, top bool) []int {
	var order []int
	for i := range blocks {
		if blocks[i].top == top {
			order = append(order, i)
		}
	}
	sort.SliceStable(order, func(a, b int) bool {
		if blocks[order[a]].start != blocks[order[b]].start {
			return blocks[order[a]].start < blocks[order[b]].start
		}
		return blocks[order[a]].end < blocks[order[b]].end
	})
	return order
}

// extendShortScreens grows the end time of a screen shorter than shortScreenMS
// toward a full reading time. The end never moves past the start of a later
// screen of the same region and a screen that already overlaps another one in
// its region keeps its own end. Starts never change, and regions are
// independent: the other side's timing is not a limit.
func extendShortScreens(blocks []block) {
	for _, top := range []bool{false, true} {
		order := regionOrder(blocks, top)
		for _, index := range order {
			b := &blocks[index]
			if b.uncertainFallback || b.recheckApplied {
				continue
			}
			if short := shortScreenMS/10 - (assCentis(b.end) - assCentis(b.start)); short > 0 {
				// The floor is measured on the centiseconds the file shows, so
				// the end lands exactly on start+1200 as the player sees it.
				limit := (assCentis(b.start) + shortScreenMS/10) * 10
				overlapping := false
				for _, otherIndex := range order {
					if otherIndex == index {
						continue
					}
					other := blocks[otherIndex]
					if assCentis(other.start) >= assCentis(b.end) {
						limit = min(limit, other.start)
					} else if assCentis(other.end) > assCentis(b.start) {
						overlapping = true
					}
				}
				if !overlapping && assCentis(limit) > assCentis(b.end) {
					b.end = limit
				}
			}
		}
	}
}

// yieldToChineseSingles removes short overlaps between a Chinese-only
// dialogue line and the paired line next to it. The pair shows on English
// time while the single keeps its Chinese time, so two timings of one pause
// can overlap and the player stacks three lines. The pair yields, which
// restores the Chinese track's own order, unless it would drop below
// minYieldedPairMS; then the single is cut.
func yieldToChineseSingles(blocks []block, mapping Mapping, zh map[int]Cue) {
	single := func(b block) bool {
		return len(b.enIDs) == 0 && len(b.zhIDs) > 0 && !b.isLyric && b.position == nil &&
			!groupPlacementKind(mapping, b.group, zh)
	}
	paired := func(b block) bool {
		return len(b.enIDs) > 0 && len(b.zhIDs) > 0 && !b.isLyric && b.position == nil
	}
	for i := range blocks {
		if !single(blocks[i]) || blocks[i].recheckApplied {
			continue
		}
		s := &blocks[i]
		for j := range blocks {
			if j == i || !paired(blocks[j]) || blocks[j].top != s.top || blocks[j].recheckApplied {
				continue
			}
			p := &blocks[j]
			// Measured on the centisecond times the subtitle file shows.
			overlap := min(roundCentiseconds(s.end), roundCentiseconds(p.end)) -
				max(roundCentiseconds(s.start), roundCentiseconds(p.start))
			if overlap <= 0 || overlap > singleOverlapMS {
				continue
			}
			switch {
			case p.start < s.start && p.end < s.end:
				// The single comes after the pair. The pair yields its end unless
				// that would leave it under minYieldedPairMS; then the single is
				// cut to start where the pair ends.
				if s.start-p.start >= minYieldedPairMS {
					p.end = s.start
				} else {
					s.start = p.end
				}
			case p.start > s.start && p.end > s.end:
				// The single comes before the pair. English keeps its timeline, so
				// the pair's start never moves; the single's end is cut to it.
				// A cut that leaves the single no positive duration on ASS
				// centiseconds keeps both original times.
				if roundCentiseconds(p.start) > roundCentiseconds(s.start) {
					s.end = p.start
				}
			}
		}
	}
}

func verifySourceCoverage(blocks []block, en, zh map[int]Cue) error {
	enSeen := make(map[int]bool, len(en))
	zhSeen := make(map[int]bool, len(zh))
	for _, b := range blocks {
		for _, id := range b.enIDs {
			if _, ok := en[id]; !ok {
				return fmt.Errorf("renderer references unknown en cue %d", id)
			}
			enSeen[id] = true
		}
		for _, id := range b.zhIDs {
			if _, ok := zh[id]; !ok {
				return fmt.Errorf("renderer references unknown zh cue %d", id)
			}
			zhSeen[id] = true
		}
	}
	for id := range en {
		if !enSeen[id] {
			return fmt.Errorf("renderer lost en source cue %d", id)
		}
	}
	for id := range zh {
		if !zhSeen[id] {
			return fmt.Errorf("renderer lost zh source cue %d", id)
		}
	}
	return nil
}

// oneSidedText returns the text a one-sided cue renders on. It joins the source
// into one display line when the flattened text fits the language limit, and
// otherwise keeps exactly the source line breaks without adding wraps. The
// flattening keeps speaker marks, so a "- A - B" dialogue line stays readable.
func oneSidedText(plain, display string, limit int, chinese bool) string {
	source := strings.TrimSpace(plain)
	if source == "" {
		// A format-only cue has no source lines; the display text is all it has.
		return display
	}
	flattened := joinCueLines(source)
	if limit > 0 && countedLengthFor(flattened, chinese) <= limit {
		return flattened
	}
	return source
}

// joinCueLines flattens a cue's meaningful source lines with single spaces.
func joinCueLines(text string) string {
	return strings.Join(sourceLines(text), " ")
}

func hasMusicSymbol(text string) bool {
	return strings.ContainsAny(text, "♪♫")
}

func cueIsMixedLyric(cue Cue) (lyricLines, spokenLines []string, isMixed bool) {
	lines := sourceLines(cue.Plain)
	if len(lines) <= 1 {
		return nil, nil, false
	}
	dashCount := 0
	for _, l := range lines {
		if startsWithDash(l) {
			dashCount++
		}
	}
	if !cue.MultiSpeaker && dashCount < 2 && (len(lines) != 2 || !startsWithDash(lines[1])) {
		return nil, nil, false
	}
	var lyrics, spoken []string
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed == "" {
			continue
		}
		if hasMusicSymbol(trimmed) {
			lyrics = append(lyrics, l)
		} else {
			spoken = append(spoken, l)
		}
	}
	if len(lyrics) > 0 && len(spoken) > 0 {
		return lyrics, spoken, true
	}
	return nil, nil, false
}

func cueIsLyric(cue Cue) bool {
	lines := sourceLines(cue.Plain)
	if len(lines) == 0 {
		return false
	}
	hasMusic := false
	for _, l := range lines {
		if hasMusicSymbol(l) {
			hasMusic = true
			break
		}
	}
	if !hasMusic {
		return false
	}
	_, _, isMixed := cueIsMixedLyric(cue)
	return !isMixed
}

func cuesAreLyrics(cues []Cue) bool {
	if len(cues) == 0 {
		return false
	}
	for _, cue := range cues {
		if !cueIsLyric(cue) {
			return false
		}
	}
	return true
}

func cueHasVisibleSpeakerRows(c Cue) bool {
	if c.MultiSpeaker || cueHasMixedLyric(c) || countDashes(c) >= 2 {
		return true
	}
	lines := sourceLines(c.Plain)
	return len(lines) > 1 && startsWithDash(lines[1])
}

// buildBlocks anchors groups to English timing. Short text can stay joined;
// longer Chinese groups reuse source internal cuts instead of a full hold.
func buildBlocks(mapping Mapping, en, zh map[int]Cue, cfg config.LLMConfig, words WordTimings, checks *splitChecks) []block {
	var result []block
	rechecksByKey := make(map[string]SemanticRecheck, len(mapping.SemanticRechecks))
	for _, r := range mapping.SemanticRechecks {
		rechecksByKey[recheckGroupKey(r.ENIDs, r.ZHIDs)] = r
	}
	for groupIndex, group := range mapping.Groups {
		es := cuesFor(en, group.ENIDs)
		zs := cuesFor(zh, group.ZHIDs)
		if len(es) == 0 && len(zs) == 0 {
			continue
		}
		anchor := firstCue(es, zs)
		// Two distinct positions: the raw source side (cue Top flags) and the
		// effective pre-policy side (model layer, or a source Top flag). The raw
		// side alone breaks contiguous on-screen runs; the effective side drives
		// migration, per the approved bidirectional rule. On-screen text ignores
		// the model layer: it starts at its source position and moves only when
		// it collides with speech, so equal labels do not land on different
		// sides depending on one window's answer.
		sourceTop := false
		top := group.Layer == LayerTop && !groupPlacementKind(mapping, groupIndex, zh)
		for _, cue := range append(append([]Cue{}, es...), zs...) {
			if cue.Top {
				sourceTop = true
				top = true
			}
		}
		groupIsLyric := false
		if (len(es) > 0 && cuesAreLyrics(es)) || (len(zs) > 0 && cuesAreLyrics(zs)) {
			groupIsLyric = true
			top = true
		}
		native := anchor
		for _, cue := range append(append([]Cue{}, es...), zs...) {
			if cue.Position != nil || cue.Alignment != 2 {
				native = cue
				break
			}
		}
		alignment := native.Alignment
		if top && alignment < 7 {
			alignment = 8
		}

		blockPos := native.Position
		if top != sourceTop {
			blockPos = nil
		}

		key := recheckGroupKey(group.ENIDs, group.ZHIDs)
		r, hasRecheck := rechecksByKey[key]
		if hasRecheck && !r.Uncertain {
			recheckBlocks := buildRecheckBlocks(groupIndex, group, es, zs, r, en, zh, sourceTop, alignment, blockPos)
			if len(recheckBlocks) > 0 {
				result = append(result, recheckBlocks...)
				continue
			}
		} else if hasRecheck && r.Uncertain {
			// Special uncertain fallback only.
			// Set recheckApplied so screen plans cannot override visible source breaks.
			makeGroupBlock := func(start, end int, ee, zz []Cue, strategy string) block {
				return block{
					start: start, end: end,
					enIDs: cueIDs(ee), zhIDs: cueIDs(zz), group: groupIndex,
					strategy: strategy, top: top, sourceTop: sourceTop,
					alignment: alignment, position: blockPos,
					isLyric:           groupIsLyric,
					recheckApplied:    true,
					uncertainFallback: true,
				}
			}
			if len(es) == 1 && len(zs) == 1 {
				b := makeGroupBlock(es[0].Start, es[0].End, es, zs, strategyOneToOne)
				if cueHasVisibleSpeakerRows(es[0]) {
					text := es[0].Plain
					b.enText = &text
					b.enFlattened = true
				}
				if cueHasVisibleSpeakerRows(zs[0]) {
					text := zs[0].Plain
					b.zhText = &text
					b.zhFlattened = true
				}
				result = append(result, b)
				continue
			}

			// When multiple source cues or language windows differ across multiple cues:
			// Sweep original cue start/end times on both sides and show precisely the currently
			// active source cues, preserving explicit EN and ZH rows, English/Chinese-only intervals,
			// no FillGap extension.
			timePoints := make([]int, 0, (len(es)+len(zs))*2)
			for _, c := range es {
				timePoints = append(timePoints, c.Start, c.End)
			}
			for _, c := range zs {
				timePoints = append(timePoints, c.Start, c.End)
			}
			sort.Ints(timePoints)
			var boundaries []int
			for i, t := range timePoints {
				if i == 0 || t != timePoints[i-1] {
					boundaries = append(boundaries, t)
				}
			}

			sameCues := func(a, b []Cue) bool {
				if len(a) != len(b) {
					return false
				}
				for i := range a {
					if a[i].ID != b[i].ID {
						return false
					}
				}
				return true
			}

			type cueInterval struct {
				start, end int
				en         []Cue
				zh         []Cue
			}
			var intervals []cueInterval
			for i := 0; i+1 < len(boundaries); i++ {
				start, end := boundaries[i], boundaries[i+1]
				if roundCentiseconds(start) >= roundCentiseconds(end) {
					continue
				}
				var activeES, activeZS []Cue
				for _, c := range es {
					if c.Start <= start && c.End >= end {
						activeES = append(activeES, c)
					}
				}
				for _, c := range zs {
					if c.Start <= start && c.End >= end {
						activeZS = append(activeZS, c)
					}
				}
				if len(activeES) == 0 && len(activeZS) == 0 {
					continue
				}
				if len(intervals) > 0 &&
					roundCentiseconds(intervals[len(intervals)-1].end) >= roundCentiseconds(start) &&
					sameCues(intervals[len(intervals)-1].en, activeES) &&
					sameCues(intervals[len(intervals)-1].zh, activeZS) {
					intervals[len(intervals)-1].end = end
				} else {
					intervals = append(intervals, cueInterval{start: start, end: end, en: activeES, zh: activeZS})
				}
			}

			formatActiveText := func(cues []Cue) (string, bool) {
				if len(cues) == 0 {
					return "", false
				}
				var parts []string
				hasExplicit := false
				for _, c := range cues {
					if cueHasVisibleSpeakerRows(c) {
						hasExplicit = true
						parts = append(parts, c.Plain)
					} else {
						parts = append(parts, c.Display)
					}
				}
				if hasExplicit || len(cues) > 1 {
					return strings.Join(parts, "\n"), true
				}
				return parts[0], false
			}

			for _, inv := range intervals {
				splitStrat := strategyTimedSplit
				if len(inv.en) == 0 || len(inv.zh) == 0 {
					splitStrat = strategyOneSided
				}
				b := makeGroupBlock(inv.start, inv.end, inv.en, inv.zh, splitStrat)
				if text, override := formatActiveText(inv.en); override {
					b.enText = &text
					b.enFlattened = true
				}
				if text, override := formatActiveText(inv.zh); override {
					b.zhText = &text
					b.zhFlattened = true
				}
				result = append(result, b)
			}
			continue
		}

		makeBlock := func(start, end int, ee, zz []Cue, strategy string) block {
			return block{
				start: start, end: end,
				enIDs: cueIDs(ee), zhIDs: cueIDs(zz), group: groupIndex,
				strategy: strategy, top: top, sourceTop: sourceTop,
				alignment: alignment, position: blockPos,
				isLyric: groupIsLyric,
			}
		}
		oneSided := func(cue Cue, isEN bool) block {
			ee, zz := []Cue{}, []Cue{}
			cueLyric := cueIsLyric(cue)
			cueTop := top || cueLyric
			cueAlign := alignment
			if cueTop && cueAlign < 7 {
				cueAlign = 8
			}
			if isEN {
				ee = append(ee, cue)
			} else {
				zz = append(zz, cue)
			}
			b := makeBlock(cue.Start, cue.End, ee, zz, strategyOneSided)
			b.top = cueTop
			b.alignment = cueAlign
			b.isLyric = cueLyric
			if b.top != b.sourceTop {
				b.position = nil
			}
			return b
		}

		// A cue with no counterpart (sign text, lyrics without translation) is
		// never split. Its flattened text goes on one line when it fits that
		// language's character limit; otherwise it keeps exactly its source line
		// breaks and gains none. The text override carries the chosen text,
		// because layout re-reads the flattened Display and would wrap it again.
		if len(es) == 0 || len(zs) == 0 {
			for _, cue := range es {
				b := oneSided(cue, true)
				text := oneSidedText(cue.Plain, cue.Display, cfg.EnChars, false)
				b.enText, b.enFlattened = &text, true
				result = append(result, b)
			}
			for _, cue := range zs {
				b := oneSided(cue, false)
				text := oneSidedText(cue.Plain, cue.Display, cfg.ZhChars, true)
				b.zhText, b.zhFlattened = &text, true
				result = append(result, b)
			}
			continue
		}
		if len(es) == 1 && len(zs) == 1 {
			if lyrics, spoken, isMixed := cueIsMixedLyric(es[0]); isMixed && !hasMusicSymbol(zs[0].Plain) {
				sStart := zs[0].Start
				sEnd := es[0].End
				assStart := roundCentiseconds(sStart)
				assEnd := roundCentiseconds(sEnd)
				dur := assEnd - assStart
				floor := minFallbackFragmentMS
				nativeDur := roundCentiseconds(zs[0].End) - roundCentiseconds(zs[0].Start)
				if nativeDur < floor {
					floor = nativeDur
				}
				if sStart > es[0].Start && sStart < es[0].End && dur >= floor && dur > 0 {
					lyricText := strings.Join(lyrics, "\n")
					spokenText := strings.Join(spoken, "\n")
					zhText := zs[0].Plain

					lyricBlock := block{
						start:           es[0].Start,
						end:             es[0].End,
						enIDs:           []int{es[0].ID},
						zhIDs:           nil,
						group:           groupIndex,
						strategy:        strategyOneToOne,
						top:             true,
						sourceTop:       sourceTop,
						alignment:       8,
						position:        nil,
						enText:          &lyricText,
						enSeparatedRole: true,
						enFlattened:     false,
						isLyric:         true,
					}
					var spokenPos *[2]float64
					if !sourceTop {
						spokenPos = native.Position
					}
					spokenBlock := block{
						start:           sStart,
						end:             sEnd,
						enIDs:           []int{es[0].ID},
						zhIDs:           []int{zs[0].ID},
						group:           groupIndex,
						strategy:        strategyOneToOne,
						top:             false,
						sourceTop:       sourceTop,
						alignment:       2,
						position:        spokenPos,
						enText:          &spokenText,
						zhText:          &zhText,
						enSeparatedRole: true,
						enFlattened:     false,
						zhFlattened:     false,
						isLyric:         false,
					}
					result = append(result, lyricBlock, spokenBlock)
					continue
				}
			}
			if lyrics, spoken, isMixed := cueIsMixedLyric(zs[0]); isMixed && !hasMusicSymbol(es[0].Plain) {
				sStart := es[0].Start
				sEnd := zs[0].End
				assStart := roundCentiseconds(sStart)
				assEnd := roundCentiseconds(sEnd)
				dur := assEnd - assStart
				floor := minFallbackFragmentMS
				nativeDur := roundCentiseconds(es[0].End) - roundCentiseconds(es[0].Start)
				if nativeDur < floor {
					floor = nativeDur
				}
				if sStart > zs[0].Start && sStart < zs[0].End && dur >= floor && dur > 0 {
					lyricText := strings.Join(lyrics, "\n")
					spokenText := strings.Join(spoken, "\n")
					enText := es[0].Plain

					lyricBlock := block{
						start:           zs[0].Start,
						end:             zs[0].End,
						enIDs:           nil,
						zhIDs:           []int{zs[0].ID},
						group:           groupIndex,
						strategy:        strategyOneToOne,
						top:             true,
						sourceTop:       sourceTop,
						alignment:       8,
						position:        nil,
						zhText:          &lyricText,
						zhSeparatedRole: true,
						zhFlattened:     false,
						isLyric:         true,
					}
					var spokenPos *[2]float64
					if !sourceTop {
						spokenPos = native.Position
					}
					spokenBlock := block{
						start:           sStart,
						end:             sEnd,
						enIDs:           []int{es[0].ID},
						zhIDs:           []int{zs[0].ID},
						group:           groupIndex,
						strategy:        strategyOneToOne,
						top:             false,
						sourceTop:       sourceTop,
						alignment:       2,
						position:        spokenPos,
						enText:          &enText,
						zhText:          &spokenText,
						zhSeparatedRole: true,
						enFlattened:     false,
						zhFlattened:     false,
						isLyric:         false,
					}
					result = append(result, lyricBlock, spokenBlock)
					continue
				}
			}
			result = append(result, makeBlock(es[0].Start, es[0].End, es, zs, strategyOneToOne))
			continue
		}

		enJoinable := joinable(es, cfg.EnChars, cfg.EnGapMS, cfg.SpanMS, false)
		zhJoinable := joinable(zs, cfg.ZhChars, cfg.ZhGapMS, 0, true)
		if (len(es) == 1 || len(zs) == 1) && enJoinable && zhJoinable {
			result = append(result, makeBlock(es[0].Start, maxEnd(es), es, zs, strategyShortJoin))
			continue
		}
		// A two-speaker Chinese cue that does not fit one line with the joined
		// English follows the English cues, one speaker line per cue.
		if len(zs) == 1 && zs[0].MultiSpeaker && len(es) > 1 {
			if lines := speakerLines(es, zs[0], cfg, checks); lines != nil {
				for i, cue := range es {
					end := cue.End
					if i+1 < len(es) && es[i+1].Start-end > 0 && es[i+1].Start-end <= cfg.FillGapMS {
						end = es[i+1].Start
					}
					b := makeBlock(cue.Start, end, []Cue{cue}, zs, strategyTimedSplit)
					// A speaker line is one source line: it keeps its length.
					b.zhText, b.zhSeparatedRole, b.zhFlattened = &lines[i], true, true
					result = append(result, b)
				}
				continue
			}
		}
		if len(zs) == 1 && zs[0].MultiSpeaker && enJoinable {
			result = append(result, makeBlock(es[0].Start, maxEnd(es), es, zs, strategyShortJoin))
			continue
		}
		if len(es) == 1 && es[0].MultiSpeaker && zhJoinable {
			result = append(result, makeBlock(es[0].Start, es[0].End, es, zs, strategyShortJoin))
			continue
		}
		if len(es) == 1 {
			if zhJoinable && !es[0].MultiSpeaker {
				result = append(result, makeBlock(es[0].Start, es[0].End, es, zs, strategyOneToOne))
				continue
			}
			if options := textOptions(es[0].Display, es[0].Plain, cfg.EnChars, "en"); len(options) > 0 && !es[0].MultiSpeaker {
				// The English line must split, so each English part is shown with
				// Chinese: one part per Chinese cue in confirmed order, or else the
				// Chinese stays whole over the English parts.
				if parts, bounds := oneToNParts(es[0], zs, options, words, checks); parts != nil {
					for i, cue := range zs {
						b := makeBlock(bounds[i], bounds[i+1], es, []Cue{cue}, strategyTimedSplit)
						b.enText = &parts[i]
						result = append(result, b)
					}
				} else {
					result = append(result, makeBlock(es[0].Start, es[0].End, es, zs, strategyOneToNReview))
				}
				continue
			}
			// Native Chinese starts supply internal boundaries, never outer bounds.
			bounds := []int{es[0].Start}
			for _, cue := range zs[1:] {
				bounds = append(bounds, cue.Start)
			}
			bounds = append(bounds, es[0].End)
			valid := true
			for i := 0; i+1 < len(bounds); i++ {
				if bounds[i] >= bounds[i+1] {
					valid = false
					break
				}
			}
			if valid {
				for i, cue := range zs {
					result = append(result, makeBlock(bounds[i], bounds[i+1], es, []Cue{cue}, strategyOneToN))
				}
			} else {
				result = append(result, makeBlock(es[0].Start, es[0].End, es, zs, strategyOneToNReview))
			}
			continue
		}

		var slices []block
		for i, cue := range es {
			end := cue.End
			if i+1 < len(es) && es[i+1].Start-end > 0 && es[i+1].Start-end <= cfg.FillGapMS {
				end = es[i+1].Start
			}
			strategy := strategyMNReview
			if len(zs) == 1 {
				strategy = strategyNToOne
			}
			slices = append(slices, makeBlock(cue.Start, end, []Cue{cue}, zs, strategy))
		}
		if len(zs) > 1 && !zhJoinable {
			slices = splitChineseSources(slices, zs, en, checks)
		}
		slices = mergeDialogueCounterpartSlices(slices, en, zh, cfg)
		result = append(result, slices...)
	}
	return result
}

func mergeDialogueCounterpartSlices(slices []block, en, zh map[int]Cue, cfg config.LLMConfig) []block {
	if len(slices) <= 1 {
		return slices
	}
	var out []block
	i := 0
	for i < len(slices) {
		b := slices[i]
		if len(b.zhIDs) == 1 {
			zcue := zh[b.zhIDs[0]]
			if zcue.MultiSpeaker {
				j := i + 1
				candidateEN := cuesFor(en, b.enIDs)
				seenEN := make(map[int]bool)
				for _, id := range b.enIDs {
					seenEN[id] = true
				}
				for j < len(slices) {
					next := slices[j]
					if len(next.zhIDs) != 1 || next.zhIDs[0] != b.zhIDs[0] {
						break
					}
					var newEN []Cue
					for _, id := range next.enIDs {
						if !seenEN[id] {
							seenEN[id] = true
							newEN = append(newEN, en[id])
						}
					}
					if len(newEN) == 0 {
						j++
						continue
					}
					combinedEN := append(append([]Cue(nil), candidateEN...), newEN...)
					if !joinable(combinedEN, cfg.EnChars, cfg.EnGapMS, cfg.SpanMS, false) {
						break
					}
					candidateEN = combinedEN
					j++
				}
				if j > i+1 {
					merged := b
					merged.start = slices[i].start
					merged.end = slices[j-1].end
					merged.enIDs = cueIDs(candidateEN)
					merged.strategy = strategyShortJoin
					out = append(out, merged)
					i = j
					continue
				}
			}
		}
		if len(b.enIDs) == 1 {
			ecue := en[b.enIDs[0]]
			if ecue.MultiSpeaker {
				j := i + 1
				candidateZH := cuesFor(zh, b.zhIDs)
				seenZH := make(map[int]bool)
				for _, id := range b.zhIDs {
					seenZH[id] = true
				}
				for j < len(slices) {
					next := slices[j]
					if len(next.enIDs) != 1 || next.enIDs[0] != b.enIDs[0] {
						break
					}
					var newZH []Cue
					for _, id := range next.zhIDs {
						if !seenZH[id] {
							seenZH[id] = true
							newZH = append(newZH, zh[id])
						}
					}
					if len(newZH) == 0 {
						j++
						continue
					}
					combinedZH := append(append([]Cue(nil), candidateZH...), newZH...)
					if !joinable(combinedZH, cfg.ZhChars, cfg.ZhGapMS, 0, true) {
						break
					}
					candidateZH = combinedZH
					j++
				}
				if j > i+1 {
					merged := b
					merged.start = slices[i].start
					merged.end = slices[j-1].end
					merged.zhIDs = cueIDs(candidateZH)
					merged.strategy = strategyShortJoin
					out = append(out, merged)
					i = j
					continue
				}
			}
		}
		out = append(out, b)
		i++
	}
	return out
}

func cuesFor(index map[int]Cue, ids []int) []Cue {
	result := make([]Cue, 0, len(ids))
	for _, id := range ids {
		if cue, ok := index[id]; ok {
			result = append(result, cue)
		}
	}
	return result
}

func cueIDs(cues []Cue) []int {
	ids := make([]int, len(cues))
	for i, cue := range cues {
		ids[i] = cue.ID
	}
	return ids
}

func firstCue(groups ...[]Cue) Cue {
	for _, group := range groups {
		if len(group) > 0 {
			return group[0]
		}
	}
	return Cue{}
}

func maxEnd(cues []Cue) int {
	end := 0
	for _, cue := range cues {
		if cue.End > end {
			end = cue.End
		}
	}
	return end
}

// joinable ports the prototype's short-join predicate. span 0 disables the span
// check (Chinese uses only the character and gap limits). The separator space
// between two cues counts for English only, as the Chinese limit skips spaces.
func joinable(cues []Cue, limit, gap, span int, chinese bool) bool {
	if len(cues) == 0 {
		return false
	}
	total := 0
	for i, cue := range cues {
		if i > 0 && !chinese {
			total++
		}
		if i > 0 && cue.Start-cues[i-1].End < 0 {
			return false
		}
		total += countedLengthFor(cue.Display, chinese)
		if len(cues) > 1 && cue.MultiSpeaker {
			return false
		}
	}
	if total > limit {
		return false
	}
	if span > 0 && cues[len(cues)-1].End-cues[0].Start > span {
		return false
	}
	for i := 1; i < len(cues); i++ {
		g := cues[i].Start - cues[i-1].End
		if g < 0 || g > gap {
			return false
		}
		if startsWithDash(cues[i].Display) {
			return false
		}
	}
	return true
}

func startsWithDash(text string) bool {
	text = strings.TrimLeft(text, " \t\r\n")
	return strings.HasPrefix(text, "-") || strings.HasPrefix(text, "–") || strings.HasPrefix(text, "—")
}

// wrapSourceDisplay retains original single lines, speaker boundaries, and
// the original line breaks of a whole cue whose lines each fit the limit.
// Joined source cues still use the configured display limits.
func wrapSourceDisplay(text, lang string, limit int, balanced bool, cues []Cue) string {
	chinese := lang == "zh"
	if len(cues) == 1 && compactText(cues[0].Plain, false) == compactText(text, false) {
		if sourceSingleLine(cues[0].Plain) {
			return text
		}
		if countedLengthFor(text, chinese) > limit && (cues[0].MultiSpeaker || linesWithin(cues[0].Plain, limit, chinese)) {
			return cues[0].Plain
		}
	}
	return wrapDisplay(text, lang, limit, balanced)
}

// linesWithin reports whether every source line of text fits the limit.
func linesWithin(text string, limit int, chinese bool) bool {
	for _, line := range sourceLines(text) {
		if countedLengthFor(line, chinese) > limit {
			return false
		}
	}
	return true
}

// wrapDisplay is a deterministic presentation transform. It only inserts line
// breaks and drops the spaces around them; no other character is added or
// removed.
func wrapDisplay(text, lang string, limit int, balanced bool) string {
	if limit <= 0 {
		return text
	}
	chinese := lang == "zh"
	var result []string
	for _, line := range strings.Split(text, "\n") {
		if balanced && chinese && countedLengthFor(line, true) > limit {
			result = append(result, wrapBalancedZH(line, limit)...)
			continue
		}
		result = append(result, wrapLine(line, lang, limit)...)
	}
	return strings.Join(result, "\n")
}

func wrapLine(line, lang string, limit int) []string {
	var result []string
	chinese := lang == "zh"
	runes := []rune(line)
	for countedLengthFor(string(runes), chinese) > limit {
		end := limitIndex(runes, limit, chinese)
		if lang == "en" {
			cut := lastSpaceCut(runes, end)
			if cut <= 0 || !isSafeCutIndex(runes, cut) {
				break
			}
			result = append(result, string(runes[:cut]))
			runes = trimLeftSpace(runes[cut:])
			continue
		}
		// A Chinese space separates clauses and costs nothing toward the limit, so
		// wrapping there reads better than cutting at the character that fills
		// the limit. Fall back to the limit position when no space is near.
		cut := -1
		if chinese {
			if space := lastSpaceCut(runes, end); space > 0 && isSafeCutIndex(runes, space) {
				cut = space
			}
		}
		// Never split a grapheme cluster (ZWJ emoji, combining marks, variation
		// selectors, flag pairs). If no safe cut exists at or before the limit,
		// keep the whole remaining text rather than corrupting it.
		if cut < 0 {
			cut = end
			for cut > 0 && !isSafeCutIndex(runes, cut) {
				cut--
			}
		}
		if cut <= 0 {
			break
		}
		result = append(result, strings.TrimRight(string(runes[:cut]), " \t"))
		runes = trimLeftSpace(runes[cut:])
	}
	result = append(result, string(runes))
	return result
}

// lastSpaceCut finds the last space in [limit/2, limit]. It returns a rune
// index so callers never slice a UTF-8 string by a byte offset.
func lastSpaceCut(runes []rune, limit int) int {
	low := limit / 2
	cut := -1
	for i := low; i <= limit && i < len(runes); i++ {
		if runes[i] == ' ' || runes[i] == '\t' {
			cut = i
		}
	}
	return cut
}

func trimLeftSpace(runes []rune) []rune {
	for len(runes) > 0 && (runes[0] == ' ' || runes[0] == '\t') {
		runes = runes[1:]
	}
	return runes
}

// wrapBalancedZH ports the prototype's balanced Chinese wrapping: choose the
// number of remaining lines, cut near an even width without leaving closing
// punctuation at the start of a line, and never exceed the limit.
func wrapBalancedZH(line string, limit int) []string {
	runes := []rune(line)
	var result []string
	remaining := int(math.Ceil(float64(countedLengthFor(line, true)) / float64(limit)))
	closing := "，。！？；：、…）】》”’"
	isClosing := func(r rune) bool { return strings.ContainsRune(closing, r) }
	isBreakSpace := unicodeIsSpace
	for remaining > 1 && len(runes) > 0 {
		counted := countedPrefix(runes, true)
		total := counted[len(runes)]
		width := int(math.Ceil(float64(total) / float64(remaining)))
		best := -1
		for i := 1; i < len(runes); i++ {
			if counted[i] > limit || total-counted[i] > (remaining-1)*limit {
				continue
			}
			if absInt(counted[i]-width) > 8 {
				continue
			}
			if isClosing(runes[i]) {
				continue
			}
			if isBreakSpace(runes[i-1]) || isClosing(runes[i-1]) {
				if best < 0 || absInt(counted[i]-width) < absInt(counted[best]-width) {
					best = i
				}
			}
		}
		cut := best
		if cut < 0 {
			cut = limitIndex(runes, width, true)
		}
		// Closing punctuation does not count, so it can always join the line.
		for cut < len(runes) && isClosing(runes[cut]) {
			cut++
		}
		result = append(result, strings.TrimRight(string(runes[:cut]), " \t"))
		runes = trimLeftSpace(runes[cut:])
		remaining--
	}
	if len(runes) > 0 {
		result = append(result, string(runes))
	}
	return result
}

func unicodeIsSpace(r rune) bool {
	return r == ' ' || r == '\t' || r == '　' || r == '\n' || r == '\r'
}

func absInt(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func isSpeakerLine(line string) bool {
	line = strings.TrimSpace(line)
	return strings.HasPrefix(line, "-") || strings.HasPrefix(line, "–") || strings.HasPrefix(line, "—")
}

func hasTwoSpeakers(lines []string) bool {
	if len(lines) != 2 {
		return false
	}
	return isSpeakerLine(lines[0]) && isSpeakerLine(lines[1])
}

// mergeSeparator joins two source lines that stay on one display line,
// preserving the original text and order.
const mergeSeparator = " "

// sideSpeakerEvidence returns the text and two-speaker evidence for one side of
// a block. Only a role separated out of a mixed lyric cue is judged by its own
// override text; any other override is source-preserving and keeps the source
// cue's speaker evidence.
func sideSpeakerEvidence(text *string, separated bool, cue Cue, hasCue bool) (string, bool) {
	if separated && text != nil {
		return *text, cueSpeakerLines(sourceLines(*text)) >= 2
	}
	if hasCue {
		return cue.Plain, cue.MultiSpeaker
	}
	return "", false
}

// blockIsDialogue reports whether any source cue of the block carries two
// speakers in one event.
func blockIsDialogue(b block, en, zh map[int]Cue) bool {
	enMulti, zhMulti := blockSpeakerSides(b, en, zh)
	return enMulti || zhMulti
}

// blockSpeakerSides reports for each language whether the block shows two
// speakers in one event.
func blockSpeakerSides(b block, en, zh map[int]Cue) (bool, bool) {
	var enCue Cue
	var hasEn bool
	if len(b.enIDs) == 1 {
		enCue, hasEn = en[b.enIDs[0]]
	}
	var zhCue Cue
	var hasZh bool
	if len(b.zhIDs) == 1 {
		zhCue, hasZh = zh[b.zhIDs[0]]
	}
	_, enMulti := sideSpeakerEvidence(b.enText, b.enSeparatedRole, enCue, hasEn)
	if !b.enSeparatedRole {
		for _, cue := range cuesFor(en, b.enIDs) {
			if cue.MultiSpeaker {
				enMulti = true
				break
			}
		}
	}
	_, zhMulti := sideSpeakerEvidence(b.zhText, b.zhSeparatedRole, zhCue, hasZh)
	if !b.zhSeparatedRole {
		for _, cue := range cuesFor(zh, b.zhIDs) {
			if cue.MultiSpeaker {
				zhMulti = true
				break
			}
		}
	}
	return enMulti, zhMulti
}

// blockDisplayText returns the current text of a block without re-wrapping text
// that a policy already fixed on purpose (merged or partitioned lines).
func blockDisplayText(b block, en, zh map[int]Cue, geometry renderGeometry) (string, string) {
	ztext := joinCues(zh, b.zhIDs, " ")
	etext := joinCues(en, b.enIDs, " ")
	if b.zhText != nil {
		ztext = *b.zhText
	}
	if b.enText != nil {
		etext = *b.enText
	}
	if !b.zhFlattened && ztext != "" {
		ztext = wrapSourceDisplay(ztext, "zh", geometry.zhChars, false, cuesFor(zh, b.zhIDs))
	}
	if !b.enFlattened && etext != "" {
		etext = wrapSourceDisplay(etext, "en", geometry.enChars, false, cuesFor(en, b.enIDs))
	}
	return ztext, etext
}

// emWidthBound is an upper bound for the width of text when the font file
// cannot be measured: no glyph of a CJK subtitle font is wider than one em.
// Bold glyphs get a 10% margin.
func emWidthBound(text string, size float64, style config.ASSStyleConfig) float64 {
	n := float64(utf8.RuneCountInString(text))
	width := n * size
	if style.ScaleX > 0 {
		width = width * float64(style.ScaleX) / 100
	}
	if style.Bold {
		width *= 1.1
	}
	return width + max(n-1, 0)*style.Spacing
}

// applySpatialPolicies executes multi-speaker spatial layout and on-screen text
// collision avoidance across the full episode timeline.
// groupPlacementKind reports whether a group is placed and avoided like on-screen
// text. Besides the model's own on_screen_text decision, a group with no English
// text whose every Chinese cue is wholly wrapped in full-width parentheses is
// display text: a sign, a note, a screen readout. The explicit unknown kind keeps
// old mappings without a kind on their current behavior, and a dialogue kind is
// the model's own answer, which stands.
func groupPlacementKind(mapping Mapping, group int, zh map[int]Cue) bool {
	if group < 0 || group >= len(mapping.Groups) {
		return false
	}
	g := mapping.Groups[group]
	if g.Kind == KindOnScreenText {
		return true
	}
	if g.Kind != KindUnknown || len(g.ENIDs) > 0 || len(g.ZHIDs) == 0 {
		return false
	}
	for _, cue := range cuesFor(zh, g.ZHIDs) {
		if !parenthesizedText(cue.Plain) {
			return false
		}
	}
	return true
}

// parenthesizedText reports whether the whole trimmed text is wrapped in one
// pair of full-width parentheses.
func parenthesizedText(plain string) bool {
	text := []rune(strings.TrimSpace(plain))
	if len(text) < 2 || text[0] != '（' || text[len(text)-1] != '）' {
		return false
	}
	// The whole text must be ONE parenthetical: the bracket opened at the first
	// rune has to close exactly at the last one. "（低声）快走（关门）" opens and
	// closes twice, so its main text is not parenthesized. Nesting is allowed.
	depth := 0
	for i, r := range text {
		switch r {
		case '（':
			depth++
		case '）':
			depth--
			if depth == 0 && i != len(text)-1 {
				return false
			}
			if depth < 0 {
				return false
			}
		}
	}
	return depth == 0
}

func applySpatialPolicies(blocks []block, mapping Mapping, en, zh map[int]Cue, geometry renderGeometry, style config.ASSStyleConfig, checks *splitChecks) []block {
	isOnScreenText := func(b block) bool {
		return groupPlacementKind(mapping, b.group, zh)
	}

	// Pass 1: Contiguous on-screen text grouping and relocation.
	var ostIndices []int
	for i, b := range blocks {
		if isOnScreenText(b) {
			ostIndices = append(ostIndices, i)
		}
	}
	sort.SliceStable(ostIndices, func(i, j int) bool {
		return blocks[ostIndices[i]].start < blocks[ostIndices[j]].start
	})

	var ostGroups [][]int
	for _, idx := range ostIndices {
		b := blocks[idx]
		if len(ostGroups) == 0 {
			ostGroups = append(ostGroups, []int{idx})
			continue
		}
		lastGroup := ostGroups[len(ostGroups)-1]
		firstInGroup := blocks[lastGroup[0]]
		maxEndSoFar := 0
		for _, gIdx := range lastGroup {
			if blocks[gIdx].end > maxEndSoFar {
				maxEndSoFar = blocks[gIdx].end
			}
		}
		if b.sourceTop == firstInGroup.sourceTop && b.top == firstInGroup.top && b.start-maxEndSoFar <= 500 {
			ostGroups[len(ostGroups)-1] = append(ostGroups[len(ostGroups)-1], idx)
		} else {
			ostGroups = append(ostGroups, []int{idx})
		}
	}

	for _, g := range ostGroups {
		nativeSide := blocks[g[0]].top
		oppositeSide := !nativeSide

		hasSpeechCollision := false
		for _, idx := range g {
			b := blocks[idx]
			bStart := roundCentiseconds(b.start)
			bEnd := roundCentiseconds(b.end)
			for _, other := range blocks {
				if isOnScreenText(other) || (other.isLyric && !other.sourceTop) || other.top != nativeSide {
					continue
				}
				oStart := roundCentiseconds(other.start)
				oEnd := roundCentiseconds(other.end)
				if max(bStart, oStart) < min(bEnd, oEnd) {
					hasSpeechCollision = true
					break
				}
			}
			if hasSpeechCollision {
				break
			}
		}
		if !hasSpeechCollision {
			continue
		}

		destFree := true
		for _, idx := range g {
			b := blocks[idx]
			bStart := roundCentiseconds(b.start)
			bEnd := roundCentiseconds(b.end)
			for j, other := range blocks {
				inSameGroup := false
				for _, gIdx := range g {
					if j == gIdx {
						inSameGroup = true
						break
					}
				}
				if inSameGroup {
					continue
				}
				if other.top == oppositeSide {
					oStart := roundCentiseconds(other.start)
					oEnd := roundCentiseconds(other.end)
					if max(bStart, oStart) < min(bEnd, oEnd) {
						destFree = false
						break
					}
				}
			}
			if !destFree {
				break
			}
		}

		if destFree {
			for _, idx := range g {
				blocks[idx].top = oppositeSide
				blocks[idx].position = nil
				if oppositeSide {
					blocks[idx].alignment = 8
				} else {
					blocks[idx].alignment = 2
				}
			}
		}
	}

	// Pass 1.5: Lyric top collision avoidance.
	for i := range blocks {
		b := &blocks[i]
		if !b.isLyric || b.sourceTop || !b.top {
			continue
		}
		bStart := roundCentiseconds(b.start)
		bEnd := roundCentiseconds(b.end)
		topOccupied := false
		for j := range blocks {
			if i == j {
				continue
			}
			other := blocks[j]
			if !other.top || (other.isLyric && !other.sourceTop) {
				continue
			}
			oStart := roundCentiseconds(other.start)
			oEnd := roundCentiseconds(other.end)
			if max(bStart, oStart) < min(bEnd, oEnd) {
				topOccupied = true
				break
			}
		}
		if topOccupied {
			b.top = false
			b.alignment = 2
			b.position = nil
		}
	}

	// Pass 2: late width merge.
	//
	// Ordinary subtitles may merge past the configured cap here only because
	// temporal splitting already ran (splitLongBlocks) and this is the late
	// resort; the merge still needs reliable measured width. Both 3-line and
	// 4-line cases attempt to merge any 2-line language side. A side that shows
	// two speakers passes its character cap only when that measurement itself
	// proves the merged line fits; an unreliable measurement keeps the cap.
	// Lines without speaker marks count as two speakers only when the other side
	// has them and the group pairs one cue with one cue; a Chinese line held over
	// from an earlier English cue keeps the layout it had there.
	usableWidth := geometry.canvasWidth - geometry.marginL - geometry.marginR
	for i := range blocks {
		b := &blocks[i]
		if b.uncertainFallback {
			continue
		}
		ztext, etext := blockDisplayText(*b, en, zh, geometry)
		zhLines := sourceLines(ztext)
		enLines := sourceLines(etext)
		totalLines := len(zhLines) + len(enLines)
		if totalLines != 3 && totalLines != 4 {
			continue
		}
		enMulti, zhMulti := blockSpeakerSides(*b, en, zh)
		paired := b.group >= 0 && b.group < len(mapping.Groups) &&
			len(mapping.Groups[b.group].ENIDs) == 1 && len(mapping.Groups[b.group].ZHIDs) == 1
		mergeSide := func(lines []string, font string, size float64, cap int, isZH, multi bool) {
			merged := lines[0] + mergeSeparator + lines[1]
			// A side passes its character cap only when the measurement itself
			// proves the merged line fits; an unreliable measurement keeps the cap.
			if multi && countedLengthFor(merged, isZH) > cap {
				if _, ok := MeasureTextWidth(merged, font, size, style.ScaleX, style.Spacing, style.Bold, ""); !ok {
					return
				}
			}
			w, ok := MeasureTextWidth(merged, font, size, style.ScaleX, style.Spacing, style.Bold, "")
			if !ok && isZH && !multi {
				// A held ordinary Chinese line may exceed zh_chars and stay on
				// one line when even the upper bound fits the screen.
				w, ok = emWidthBound(merged, size, style), true
			}
			if ok && w*geometry.widthFactor() <= usableWidth {
				if isZH {
					b.zhText = &merged
					b.zhFlattened = true
				} else {
					b.enText = &merged
					b.enFlattened = true
				}
			}
		}
		if len(zhLines) == 2 {
			mergeSide(zhLines, style.PrimaryFont, geometry.zhFontSize, geometry.zhChars, true, zhMulti || (enMulti && paired))
		}
		if len(enLines) == 2 {
			mergeSide(enLines, style.SecondaryFont, geometry.enFontSize, geometry.enChars, false, enMulti || (zhMulti && paired))
		}
	}

	// Pass 3: last-resort top/bottom partitioning of two segments.
	//
	// Ordinary subtitles need only reliable two-segment grouping; dialogue
	// subtitles additionally need trustworthy two-speaker attribution.
	var finalBlocks []block
	for i := range blocks {
		b := blocks[i]
		// Partitioning needs reliable two-segment grouping, so it only applies to
		// a 1:1 block whose two sides each keep exactly their native two source
		// lines. Blocks projected by temporal splitting, or slices of a larger
		// group where the other language holds, have no such segmentation;
		// splitting their wrapped lines would duplicate content.
		if b.zhText != nil || b.enText != nil || len(b.enIDs) != 1 || len(b.zhIDs) != 1 {
			finalBlocks = append(finalBlocks, b)
			continue
		}
		if len(sourceLines(en[b.enIDs[0]].Plain)) != 2 || len(sourceLines(zh[b.zhIDs[0]].Plain)) != 2 {
			finalBlocks = append(finalBlocks, b)
			continue
		}
		ztext, etext := blockDisplayText(b, en, zh, geometry)
		zhLines := sourceLines(ztext)
		enLines := sourceLines(etext)
		if len(zhLines) != 2 || len(enLines) != 2 {
			finalBlocks = append(finalBlocks, b)
			continue
		}
		if blockIsDialogue(b, en, zh) && (!hasTwoSpeakers(zhLines) || !hasTwoSpeakers(enLines)) {
			finalBlocks = append(finalBlocks, b)
			continue
		}
		// Ordinary lines pair by position only after the order check confirmed
		// that each Chinese line translates the English line shown with it.
		if !blockIsDialogue(b, en, zh) && !checks.confirmed(zhLines, enLines) {
			finalBlocks = append(finalBlocks, b)
			continue
		}
		destSide := !b.top
		bStart, bEnd := roundCentiseconds(b.start), roundCentiseconds(b.end)
		overlaps := func(start, end int) bool { return max(bStart, start) < min(bEnd, end) }
		destFree := true
		for j, other := range blocks {
			if j == i || other.top != destSide {
				continue
			}
			if overlaps(roundCentiseconds(other.start), roundCentiseconds(other.end)) {
				destFree = false
				break
			}
		}
		if destFree {
			for _, fb := range finalBlocks {
				if fb.top == destSide && overlaps(roundCentiseconds(fb.start), roundCentiseconds(fb.end)) {
					destFree = false
					break
				}
			}
		}
		if !destFree {
			finalBlocks = append(finalBlocks, b)
			continue
		}

		// First segment Chinese+English on the opposite side, second stays native.
		b1 := b
		b1.top = true
		b1.alignment = 8
		b1.position = nil
		t1Zh, t1En := zhLines[0], enLines[0]
		b1.zhText, b1.enText = &t1Zh, &t1En
		b1.zhFlattened, b1.enFlattened = true, true

		b2 := b
		b2.top = false
		b2.alignment = 2
		b2.position = nil
		t2Zh, t2En := zhLines[1], enLines[1]
		b2.zhText, b2.enText = &t2Zh, &t2En
		b2.zhFlattened, b2.enFlattened = true, true

		finalBlocks = append(finalBlocks, b1, b2)
	}

	return finalBlocks
}

// layoutBlocks builds text, wrapping and collision-free vertical bands, then
// invokes emit for each event. The returned audits mirror the prototype
// bands/positions for tests.
func layoutBlocks(blocks []block, en, zh map[int]Cue, geometry renderGeometry, emit func(block, int, int, string, string, int, float64, float64, string, bool) error) ([]renderedEvent, error) {
	type layout struct {
		zhText string
		enText string
		height float64
	}
	layouts := make([]layout, len(blocks))
	holdHeights := map[int]float64{}
	for i, b := range blocks {
		hold := b.strategy == strategyMNReview
		ztext := joinCues(zh, b.zhIDs, " ")
		etext := joinCues(en, b.enIDs, " ")
		if b.zhFlattened && b.zhText != nil {
			ztext = *b.zhText
		} else {
			if b.zhText != nil {
				ztext = *b.zhText
			}
			if ztext != "" {
				ztext = wrapSourceDisplay(ztext, "zh", geometry.zhChars, hold, cuesFor(zh, b.zhIDs))
			}
		}
		if b.enFlattened && b.enText != nil {
			etext = *b.enText
		} else {
			if b.enText != nil {
				etext = *b.enText
			}
			if etext != "" {
				etext = wrapSourceDisplay(etext, "en", geometry.enChars, false, cuesFor(en, b.enIDs))
			}
		}
		height := float64(lineCount(ztext))*geometry.zhLineHeight() +
			float64(lineCount(etext))*geometry.enLineHeight() + geometry.blockPadding
		layouts[i] = layout{zhText: ztext, enText: etext, height: height}
		if hold {
			if height > holdHeights[b.group] {
				holdHeights[b.group] = height
			}
		}
	}

	lanes := map[bool][]band{}
	events := make([]renderedEvent, 0, len(blocks))
	for i, b := range blocks {
		if layouts[i].zhText == "" && layouts[i].enText == "" {
			emptySource := true
			for _, cue := range append(cuesFor(en, b.enIDs), cuesFor(zh, b.zhIDs)...) {
				if strings.TrimSpace(cue.Plain) != "" || strings.TrimSpace(cue.Display) != "" {
					emptySource = false
					break
				}
			}
			if emptySource {
				continue
			}
		}
		// ASS rounds to centiseconds; use that same clock for collision checks.
		start := roundCentiseconds(b.start)
		end := roundCentiseconds(b.end)
		if start >= end {
			return nil, fmt.Errorf("event duration vanishes at ASS centisecond precision (group %d)", b.group)
		}
		height := layouts[i].height
		hold := b.strategy == strategyMNReview
		if hold {
			height = holdHeights[b.group]
		}
		var active []band
		for _, list := range lanes {
			for _, candidate := range list {
				if candidate.end > start {
					active = append(active, candidate)
				}
			}
		}
		align := b.alignment
		x := channelX(align, geometry.marginL, geometry.canvasWidth, geometry.marginR)
		var lo, initial float64
		if b.position != nil {
			x = b.position[0]
			y := geometry.adaptPositionY(b.position[1], align)
			switch {
			case align >= 7:
				lo = y
			case align <= 3:
				lo = y - height
			default:
				lo = y - height/2
			}
		} else {
			if b.top {
				lo = geometry.marginV
			} else {
				lo = geometry.canvasHeight - geometry.marginV - height
			}
		}
		initial = lo
		// Bands are independent vertical reservations. Moving away from the
		// encoded frame strictly decreases the set of bands that can still
		// collide, so this loop terminates. With a non-negative margin an
		// exhausted safe area is an error, matching the prototype; adaptive
		// negative margins may extend into player-added bars.
		for collides(lo, height, active) {
			if b.top {
				lo = maxHi(lo, height, active) + geometry.collisionGap
			} else {
				lo = minLo(lo, height, active) - height - geometry.collisionGap
			}
		}
		// Every band must at least overlap the visible playback canvas; an
		// adaptive margin may place text partly in a player-added bar, which
		// is still visible, but a block entirely outside would be silently
		// lost and is an explicit error.
		if lo+height <= geometry.visibleMinY() || lo >= geometry.visibleMaxY() {
			return nil, fmt.Errorf("subtitle block for group %d falls outside the visible playback canvas", b.group)
		}
		y := lo + height
		if align >= 7 {
			y = lo
		} else if align > 3 {
			y = lo + height/2
		}
		lanes[b.top] = pruneLanes(lanes[b.top], start)
		lanes[b.top] = append(lanes[b.top], band{end: end, lo: lo, hi: lo + height})

		zstyle, estyle := "Default", "Eng"
		if b.top {
			zstyle, estyle = "TopDefault", "TopEng"
		}
		zhText, err := safeASSText(layouts[i].zhText)
		if err != nil {
			return nil, fmt.Errorf("group %d Chinese text: %w", b.group, err)
		}
		enText, err := safeASSText(layouts[i].enText)
		if err != nil {
			return nil, fmt.Errorf("group %d English text: %w", b.group, err)
		}
		text := zhText
		styleName := estyle
		if zhText != "" {
			styleName = zstyle
		}
		if enText != "" {
			if text != "" {
				text += `\N{\r` + estyle + `}` + enText
			} else {
				text = enText
			}
		}
		hasPos := b.position != nil
		if hasPos && hold {
			// Explicit top-anchoring is only applied when an absolute position tag
			// is emitted. Without \pos, \an8 would move standard bottom dialogue
			// to the top of the screen; standard bottom dialogue inherits Alignment: 2.
			horizontal := (align - 1) % 3
			align = horizontal + 7
			y = lo
		}
		wrap := ""
		if hold {
			// Explicit wrapping only applies to the held Chinese block; \q2
			// prevents the player from rebalancing it.
			wrap = `\q2`
		}
		if err := emit(b, start, end, text, styleName, align, x, y, wrap, hasPos); err != nil {
			return nil, err
		}
		events = append(events, renderedEvent{
			group: b.group, strategy: b.strategy,
			enIDs: append([]int(nil), b.enIDs...), zhIDs: append([]int(nil), b.zhIDs...),
			zhMerged: b.zhFlattened, enMerged: b.enFlattened,
			assStart: start, assEnd: end,
			zhText: layouts[i].zhText, enText: layouts[i].enText,
			band: [2]float64{lo, lo + height}, position: [2]float64{x, y},
			alignment: align, shifted: lo != initial,
		})
	}
	return events, nil
}

type band struct {
	end int
	lo  float64
	hi  float64
}

type renderedEvent struct {
	group     int
	strategy  string
	enIDs     []int
	zhIDs     []int
	zhMerged  bool
	enMerged  bool
	assStart  int
	assEnd    int
	zhText    string
	enText    string
	band      [2]float64
	position  [2]float64
	alignment int
	shifted   bool
}

func collides(lo, height float64, active []band) bool {
	for _, a := range active {
		if lo < a.hi && lo+height > a.lo {
			return true
		}
	}
	return false
}

func maxHi(lo, height float64, active []band) float64 {
	result := lo
	for _, a := range active {
		if lo < a.hi && lo+height > a.lo && a.hi > result {
			result = a.hi
		}
	}
	return result
}

func minLo(lo, height float64, active []band) float64 {
	result := lo
	first := true
	for _, a := range active {
		if lo < a.hi && lo+height > a.lo && (first || a.lo < result) {
			result = a.lo
			first = false
		}
	}
	return result
}

func pruneLanes(lanes []band, start int) []band {
	result := lanes[:0]
	for _, a := range lanes {
		if a.end > start {
			result = append(result, a)
		}
	}
	return result
}

// channelX returns the horizontal anchor for an alignment, matching the
// prototype's margin-aware horizontal channels.
func channelX(align int, marginL, canvasWidth, marginR float64) float64 {
	switch (align - 1) % 3 {
	case 0:
		return marginL
	case 2:
		return canvasWidth - marginR
	default:
		return canvasWidth / 2
	}
}

func joinCues(index map[int]Cue, ids []int, separator string) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		if cue, ok := index[id]; ok {
			parts = append(parts, cue.Display)
		}
	}
	return strings.Join(parts, separator)
}

func lineCount(text string) int {
	if text == "" {
		return 0
	}
	return strings.Count(text, "\n") + 1
}

var assCommandLike = regexp.MustCompile(`\\[Nnh]|\{[^}]*\\`)

// safeASSText converts display newlines to hard breaks. Literal text that
// resembles an ASS command is rejected rather than silently mutated, matching
// the prototype's zero-hallucination policy.
func safeASSText(text string) (string, error) {
	if assCommandLike.MatchString(text) {
		return "", errors.New("literal text resembles ASS commands; requires explicit escaping policy")
	}
	return strings.ReplaceAll(text, "\n", `\N`), nil
}

// assWriter serializes the ASS header and dialogue lines.
type assWriter struct {
	sb strings.Builder
}

func (w *assWriter) writeHeader(style config.ASSStyleConfig) {
	wrapStyle := style.WrapStyle
	if wrapStyle == "" {
		wrapStyle = "0"
	}
	fmt.Fprintf(&w.sb, `[Script Info]
; Script generated by fusionn native LLM renderer
%s
Title: Bilingual Subtitle
ScriptType: v4.00+
WrapStyle: %s
ScaledBorderAndShadow: yes
Collisions: Normal
PlayResX: %d
PlayResY: %d
YCbCr Matrix: TV.709

[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding
`, HeaderMarker, wrapStyle, style.PlayResX, style.PlayResY)

	writeStyle := func(name, font string, size int, color string, alignment int) {
		bold := 0
		if style.Bold {
			bold = 1
		}
		scaleX := style.ScaleX
		if scaleX == 0 {
			scaleX = 100
		}
		outlineColor := style.OutlineColor
		if outlineColor == "" {
			outlineColor = "&H00000000"
		}
		backColor := style.BackColor
		if backColor == "" {
			backColor = "&H00000000"
		}
		fmt.Fprintf(&w.sb, "Style: %s,%s,%d,%s,&H000000FF,%s,%s,%d,0,0,0,%d,100,%.1f,0,%d,%.1f,%.1f,%d,%d,%d,%d,1\n",
			name, font, size, color, outlineColor, backColor, bold, scaleX, style.Spacing,
			style.BorderStyle, style.Outline, style.Shadow, alignment, style.MarginLeft, style.MarginRight, style.MarginV)
	}
	alignment := style.Alignment
	if alignment < 1 || alignment > 9 {
		alignment = 2
	}
	topAlignment := alignment
	if alignment < 4 {
		topAlignment = alignment + 6
	}
	writeStyle("Default", style.PrimaryFont, style.PrimarySize, style.PrimaryColor, alignment)
	writeStyle("Eng", style.SecondaryFont, style.SecondarySize, style.SecondaryColor, alignment)
	writeStyle("TopDefault", style.PrimaryFont, style.PrimarySize, style.PrimaryColor, topAlignment)
	writeStyle("TopEng", style.SecondaryFont, style.SecondarySize, style.SecondaryColor, topAlignment)
	w.sb.WriteString("\n[Events]\n")
	w.sb.WriteString("Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n")
}

func (w *assWriter) writeDialogue(start, end int, styleName, text string, align int, x, y float64, wrap string, hasPos bool) error {
	if text == "" {
		return errors.New("refusing to write an empty dialogue event")
	}
	var tagParts []string
	if wrap != "" {
		tagParts = append(tagParts, wrap)
	}
	if hasPos {
		tagParts = append(tagParts, fmt.Sprintf(`\an%d\pos(%s,%s)`, align, formatNumber(x), formatNumber(y)))
	} else {
		isTopStyle := strings.HasPrefix(styleName, "Top")
		if (isTopStyle && align != 8) || (!isTopStyle && align != 2) {
			tagParts = append(tagParts, fmt.Sprintf(`\an%d`, align))
		}
	}
	var tags string
	if len(tagParts) > 0 {
		tags = "{" + strings.Join(tagParts, "") + "}"
	}
	fmt.Fprintf(&w.sb, "Dialogue: 0,%s,%s,%s,,0,0,0,,%s%s\n",
		assTime(start), assTime(end), styleName, tags, text)
	return nil
}

func (w *assWriter) String() string { return w.sb.String() }

// roundCentiseconds matches Python's round(ms / 10) * 10, including its
// ties-to-even behavior. Go's math.Round is ties-away-from-zero.
func roundCentiseconds(ms int) int {
	cs := math.RoundToEven(float64(ms) / 10)
	return int(cs) * 10
}

// assTime formats milliseconds as H:MM:SS.cc (centiseconds), matching pysubs2.
func assTime(ms int) string {
	if ms < 0 {
		ms = 0
	}
	cs := ms / 10
	seconds := cs / 100
	centis := cs % 100
	minutes := seconds / 60
	seconds %= 60
	hours := minutes / 60
	minutes %= 60
	return fmt.Sprintf("%d:%02d:%02d.%02d", hours, minutes, seconds, centis)
}

func formatNumber(v float64) string {
	if v == math.Trunc(v) {
		return strconv.FormatFloat(v, 'f', 0, 64)
	}
	return strconv.FormatFloat(v, 'f', 2, 64)
}
