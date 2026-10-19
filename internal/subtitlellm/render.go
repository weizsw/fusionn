package subtitlellm

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

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
)

// block is one rendered bilingual event before positioning.
type block struct {
	start     int
	end       int
	enIDs     []int
	zhIDs     []int
	group     int
	strategy  string
	top       bool
	alignment int
	position  *[2]float64
}

// Render produces the bilingual ASS file from an immutable source bundle and
// its ID-only mapping. Text and timestamps always come from the bundle; the
// mapping only chooses which cues share an event. The output canvas is the
// configured ASS play resolution and equals width x height for 16:9 sources;
// explicit positions are placed on that canvas using the reference playback
// geometry.
func Render(bundle Bundle, mapping Mapping, llmcfg config.LLMConfig, style config.ASSStyleConfig, width, height int) ([]byte, error) {
	if err := validateRenderInputs(bundle, mapping, llmcfg, style, width, height); err != nil {
		return nil, err
	}

	en, zh, err := cueIndex(bundle)
	if err != nil {
		return nil, err
	}
	blocks := buildBlocks(mapping, en, zh, llmcfg)
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
		return nil, err
	}

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
	var out assWriter
	out.writeHeader(effective)
	if _, err := layoutBlocks(blocks, en, zh, geometry, func(b block, start, end int, text string, styleName string, align int, x, y float64, wrap string) error {
		return out.writeDialogue(start, end, styleName, text, align, x, y, wrap)
	}); err != nil {
		return nil, err
	}
	return []byte(out.String()), nil
}

// renderBaseStyle returns the style used to render events. The renderer always
// writes the base style names (Default, Eng, Default_1, TopDefault, TopEng)
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
	scale := referencePlaybackCanvasScale(width, height)
	if scale == 1 || scale <= 0 {
		return style
	}
	style.PrimarySize = int(math.Round(float64(style.PrimarySize) * scale))
	style.SecondarySize = int(math.Round(float64(style.SecondarySize) * scale))
	style.MarginV = adaptedMarginV(style.MarginV, width, height, style.PlayResY)
	if style.MarginLeft > 0 {
		style.MarginLeft = int(math.Round(float64(style.MarginLeft) * scale))
	}
	if style.MarginRight > 0 {
		style.MarginRight = int(math.Round(float64(style.MarginRight) * scale))
	}
	return style
}

// referencePlaybackCanvasScale is the shared 16:9 output-canvas scale: 1 for
// frames at least as tall as width*9/16, otherwise the ratio needed to fit the
// frame inside the reference canvas (wide/letterboxed films).
func referencePlaybackCanvasScale(width, height int) float64 {
	canvasHeight := float64(width) * 9 / 16
	if float64(height) >= canvasHeight {
		return 1
	}
	return canvasHeight / float64(height)
}

// adaptedMarginV preserves the configured distance above the bottom of the
// centered 16:9 playback canvas when the encoded frame is wider than 16:9.
func adaptedMarginV(configuredMarginV, width, height, playResY int) int {
	scale := referencePlaybackCanvasScale(width, height)
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
	scale := referencePlaybackCanvasScale(g.frameWidth, g.frameHeight)
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
	scale := referencePlaybackCanvasScale(g.frameWidth, g.frameHeight)
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

func validateRenderInputs(bundle Bundle, mapping Mapping, llmcfg config.LLMConfig, style config.ASSStyleConfig, width, height int) error {
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

// buildBlocks turns mapping groups into timed events. It is a direct port of
// the prototype build_blocks: English timing is the outer anchor, 1:N borrows
// Chinese internal cuts, and M:N keeps the complete Chinese group on every
// English slice.
func buildBlocks(mapping Mapping, en, zh map[int]Cue, cfg config.LLMConfig) []block {
	var result []block
	for groupIndex, group := range mapping.Groups {
		es := cuesFor(en, group.ENIDs)
		zs := cuesFor(zh, group.ZHIDs)
		if len(es) == 0 && len(zs) == 0 {
			continue
		}
		anchor := firstCue(es, zs)
		top := group.Layer == LayerTop
		for _, cue := range append(append([]Cue{}, es...), zs...) {
			if cue.Top {
				top = true
			}
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

		makeBlock := func(start, end int, ee, zz []Cue, strategy string) block {
			return block{
				start: start, end: end,
				enIDs: cueIDs(ee), zhIDs: cueIDs(zz), group: groupIndex,
				strategy: strategy, top: top, alignment: alignment,
				position: native.Position,
			}
		}
		oneSided := func(cue Cue, isEN bool) block {
			ee, zz := []Cue{}, []Cue{}
			if isEN {
				ee = append(ee, cue)
			} else {
				zz = append(zz, cue)
			}
			return makeBlock(cue.Start, cue.End, ee, zz, strategyOneSided)
		}

		if len(es) == 0 || len(zs) == 0 {
			for _, cue := range es {
				result = append(result, oneSided(cue, true))
			}
			for _, cue := range zs {
				result = append(result, oneSided(cue, false))
			}
			continue
		}
		if len(es) == 1 && len(zs) == 1 {
			result = append(result, makeBlock(es[0].Start, es[0].End, es, zs, strategyOneToOne))
			continue
		}

		enJoinable := joinable(es, cfg.EnChars, cfg.EnGapMS, cfg.SpanMS)
		zhJoinable := joinable(zs, cfg.ZhChars, cfg.ZhGapMS, 0)
		if (len(es) == 1 || len(zs) == 1) && enJoinable && zhJoinable {
			result = append(result, makeBlock(es[0].Start, maxEnd(es), es, zs, strategyShortJoin))
			continue
		}
		if len(es) == 1 {
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

		// M:N: the complete Chinese group stays in source order on every slice.
		for i, cue := range es {
			end := cue.End
			if i+1 < len(es) && es[i+1].Start-end > 0 && es[i+1].Start-end <= cfg.FillGapMS {
				end = es[i+1].Start
			}
			strategy := strategyMNReview
			if len(zs) == 1 {
				strategy = strategyNToOne
			}
			result = append(result, makeBlock(cue.Start, end, []Cue{cue}, zs, strategy))
		}
	}
	return result
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
// check (Chinese uses only the character and gap limits).
func joinable(cues []Cue, limit, gap, span int) bool {
	if len(cues) == 0 {
		return false
	}
	total := 0
	for i, cue := range cues {
		if i > 0 {
			total++
			if cue.Start-cues[i-1].End < 0 {
				return false
			}
		}
		total += len([]rune(cue.Display))
		if cue.MultiSpeaker {
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

// wrapDisplay is a deterministic presentation transform. It only inserts line
// breaks; no character is added or removed.
func wrapDisplay(text, lang string, limit int, balanced bool) string {
	if limit <= 0 {
		return text
	}
	var result []string
	for _, line := range strings.Split(text, "\n") {
		if balanced && lang == "zh" && len([]rune(line)) > limit {
			result = append(result, wrapBalancedZH(line, limit)...)
			continue
		}
		result = append(result, wrapLine(line, lang, limit)...)
	}
	return strings.Join(result, "\n")
}

func wrapLine(line, lang string, limit int) []string {
	var result []string
	runes := []rune(line)
	for len(runes) > limit {
		if lang == "en" {
			cut := lastSpaceCut(runes, limit)
			if cut <= 0 {
				break
			}
			result = append(result, string(runes[:cut]))
			runes = trimLeftSpace(runes[cut:])
			continue
		}
		result = append(result, string(runes[:limit]))
		runes = runes[limit:]
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
	remaining := int(math.Ceil(float64(len(runes)) / float64(limit)))
	closing := "，。！？；：、…）】》”’"
	isClosing := func(r rune) bool { return strings.ContainsRune(closing, r) }
	isBreakSpace := func(r rune) bool { return unicodeIsSpace(r) }
	for remaining > 1 && len(runes) > 0 {
		width := int(math.Ceil(float64(len(runes)) / float64(remaining)))
		low := len(runes) - (remaining-1)*limit
		if low < 1 {
			low = 1
		}
		high := limit
		if high > len(runes)-1 {
			high = len(runes) - 1
		}
		best := -1
		for i := low; i <= high; i++ {
			if absInt(i-width) > 8 {
				continue
			}
			if isClosing(runes[i]) {
				continue
			}
			if isBreakSpace(runes[i-1]) || isClosing(runes[i-1]) {
				if best < 0 || absInt(i-width) < absInt(best-width) {
					best = i
				}
			}
		}
		cut := best
		if cut < 0 {
			cut = width
		}
		if cut > len(runes) {
			cut = len(runes)
		}
		for cut < limit && cut < len(runes) && isClosing(runes[cut]) {
			cut++
		}
		result = append(result, string(runes[:cut]))
		runes = runes[cut:]
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

// layoutBlocks builds text, wrapping and collision-free vertical bands, then
// invokes emit for each event. The returned audits mirror the prototype
// bands/positions for tests.
func layoutBlocks(blocks []block, en, zh map[int]Cue, geometry renderGeometry, emit func(block, int, int, string, string, int, float64, float64, string) error) ([]renderedEvent, error) {
	type layout struct {
		zhText string
		enText string
		height float64
	}
	layouts := make([]layout, len(blocks))
	holdHeights := map[int]float64{}
	for i, b := range blocks {
		ztext := joinCues(zh, b.zhIDs, "　")
		etext := joinCues(en, b.enIDs, " ")
		hold := b.strategy == strategyMNReview
		if ztext != "" {
			ztext = wrapDisplay(ztext, "zh", geometry.zhChars, hold)
		}
		if etext != "" {
			etext = wrapDisplay(etext, "en", geometry.enChars, false)
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
		if hold {
			// Python's (align - 1) % 3 is non-negative for 1..9; keep the same
			// horizontal channel and anchor the block from its top.
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
		if err := emit(b, start, end, text, styleName, align, x, y, wrap); err != nil {
			return nil, err
		}
		events = append(events, renderedEvent{
			group: b.group, strategy: b.strategy,
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
	writeStyle("Default_1", style.SecondaryFont, style.SecondarySize, style.SecondaryColor, alignment)
	writeStyle("TopDefault", style.PrimaryFont, style.PrimarySize, style.PrimaryColor, topAlignment)
	writeStyle("TopEng", style.SecondaryFont, style.SecondarySize, style.SecondaryColor, topAlignment)
	w.sb.WriteString("\n[Events]\n")
	w.sb.WriteString("Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n")
}

func (w *assWriter) writeDialogue(start, end int, styleName, text string, align int, x, y float64, wrap string) error {
	if text == "" {
		return errors.New("refusing to write an empty dialogue event")
	}
	fmt.Fprintf(&w.sb, "Dialogue: 0,%s,%s,%s,,0,0,0,,{%s\\an%d\\pos(%s,%s)}%s\n",
		assTime(start), assTime(end), styleName, wrap, align, formatNumber(x), formatNumber(y), text)
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
