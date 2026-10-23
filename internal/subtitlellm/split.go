package subtitlellm

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/fusionn/internal/config"
)

// WordTiming locates a word in the video's millisecond clock.
type WordTiming struct {
	Text    string `json:"text"`
	StartMS int    `json:"start_ms"`
	EndMS   int    `json:"end_ms"`
}

// WordTimings indexes optional English word timestamps by source cue ID.
type WordTimings map[int][]WordTiming

// minSyncFragmentMS is the minimum reading time per fragment for synchronized
// temporal splitting of native 2+2 line boundaries.
const minSyncFragmentMS = 1200

// minFallbackFragmentMS is the anti-flash floor for per-language fallback fragments.
// Existing short source events retain their original timing.
const minFallbackFragmentMS = 800

// WordAlignmentCues selects long English speech for optional audio alignment.
// It includes cues exceeding the character cap as well as 2-line English dialogue
// eligible for 2+2 synchronized splitting with a matching 2-line Chinese cue.
func WordAlignmentCues(bundle Bundle, cfg config.LLMConfig) []Cue {
	var result []Cue
	zh2Lines := make(map[int]bool)
	for _, z := range bundle.ZH {
		if !z.MultiSpeaker && len(sourceLines(z.Plain)) == 2 {
			zh2Lines[z.ID] = true
		}
	}
	for _, cue := range bundle.EN {
		if cue.MultiSpeaker || cue.End-cue.Start > 30000 {
			continue
		}
		if roundCentiseconds(cue.End)-roundCentiseconds(cue.Start) >= 2*minFallbackFragmentMS && len(textOptions(cue.Display, cue.Plain, cfg.EnChars, "en")) > 0 {
			result = append(result, cue)
			continue
		}
		if roundCentiseconds(cue.End)-roundCentiseconds(cue.Start) >= 2*minSyncFragmentMS && len(sourceLines(cue.Plain)) == 2 {
			hasMatchingZH := false
			for _, z := range bundle.ZH {
				if zh2Lines[z.ID] && (absInt(cue.Start-z.Start) <= 4000 || (cue.Start < z.End && z.Start < cue.End)) {
					hasMatchingZH = true
					break
				}
			}
			if hasMatchingZH {
				result = append(result, cue)
			}
		}
	}
	return result
}

type textPiece struct {
	start, end int
	text       string
}

type displayPlan struct {
	group      int
	ids        []int
	top        bool
	start, end int
	text       string
	plain      string
	protected  bool
	windows    [][2]int
	pieces     []textPiece
	// wordTimed reports that the pieces were cut at word timings.
	wordTimed bool
}

func displayKey(group int, ids []int, top bool) string {
	return fmt.Sprintf("%d:%v:%t", group, ids, top)
}

func cueSpeakerLines(lines []string) int {
	count := 0
	for _, l := range lines {
		if startsWithDash(l) {
			count++
		}
	}
	return count
}

// sourceLines splits source text into meaningful trimmed lines, ignoring
// whitespace and empty lines.
func sourceLines(plain string) []string {
	var lines []string
	for _, line := range strings.Split(plain, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	return lines
}

// trySyncSplit attempts synchronized temporal splitting at original line
// boundaries when native Chinese and English both have two meaningful lines.
// It requires >= 1.2s per fragment on the real candidate timestamps. Lines
// that fit side by side stay together, because the source shows them at once,
// and a split needs a confirmed order check.
func trySyncSplit(b block, enCue, zhCue Cue, words WordTimings, cfg config.LLMConfig, checks *splitChecks) ([]block, bool) {
	enText, enMulti := sideSpeakerEvidence(b.enText, b.enSeparatedRole, enCue, true)
	enDisplay := enCue.Display
	if b.enSeparatedRole && b.enText != nil {
		enDisplay = *b.enText
	}
	zhText, zhMulti := sideSpeakerEvidence(b.zhText, b.zhSeparatedRole, zhCue, true)
	if enMulti || zhMulti {
		return nil, false
	}
	enLines := sourceLines(enText)
	zhLines := sourceLines(zhText)
	if len(enLines) != 2 || len(zhLines) != 2 {
		return nil, false
	}
	if countedLength(strings.Join(enLines, " ")) <= cfg.EnChars && countedLength(strings.Join(zhLines, " ")) <= cfg.ZhChars {
		return nil, false
	}
	totalDuration := roundCentiseconds(b.end) - roundCentiseconds(b.start)
	// Ask before checking the duration or searching for a cut: Align asks
	// without word timings, which can make a cut possible at render time, and
	// the answer also gates the last-resort top/bottom partition.
	if !checks.confirmed(zhLines, enLines) || totalDuration < 2*minSyncFragmentMS {
		return nil, false
	}
	var cut int
	foundCut := false
	// Priority 1: Word alignment timestamps
	if words != nil && len(words[enCue.ID]) >= 2 {
		cuts := wordCuts(enDisplay, enLines, words[enCue.ID])
		if len(cuts) == 1 {
			c := cuts[0]
			if roundCentiseconds(c)-roundCentiseconds(b.start) >= minSyncFragmentMS &&
				roundCentiseconds(b.end)-roundCentiseconds(c) >= minSyncFragmentMS {
				cut = c
				foundCut = true
			}
		}
	}
	// Priority 2: Proportional cut based on natural text proportion
	if !foundCut {
		cuts := proportionalCuts(enLines, b.start, b.end)
		if len(cuts) == 1 {
			c := cuts[0]
			// Test the real natural proportional cut against the 1.2s floor.
			// Do NOT artificially clamp an ineligible cut into [start+1200, end-1200].
			if roundCentiseconds(c)-roundCentiseconds(b.start) >= minSyncFragmentMS &&
				roundCentiseconds(b.end)-roundCentiseconds(c) >= minSyncFragmentMS {
				cut = c
				foundCut = true
			}
		}
	}
	if !foundCut {
		return nil, false
	}
	t1En := enLines[0]
	t1Zh := zhLines[0]
	b1 := b
	b1.start = b.start
	b1.end = cut
	b1.enText = &t1En
	b1.zhText = &t1Zh
	b1.strategy = strategyTimedSplit

	t2En := enLines[1]
	t2Zh := zhLines[1]
	b2 := b
	b2.start = cut
	b2.end = b.end
	b2.enText = &t2En
	b2.zhText = &t2Zh
	b2.strategy = strategyTimedSplit

	return []block{b1, b2}, true
}

// oneToNParts pairs the parts of a long English line with the Chinese cues of
// a 1:N group, one part per cue, when the order check confirms the pairs.
// Word timings place the cuts when available; otherwise the Chinese cue
// starts do, so both languages switch at the same moment. It returns the
// English parts and the block bounds, or nil.
func oneToNParts(enCue Cue, zs []Cue, options [][]string, words WordTimings, checks *splitChecks) ([]string, []int) {
	chinese := make([]string, len(zs))
	for i, cue := range zs {
		chinese[i] = cue.Display
	}
	for _, parts := range options {
		if len(parts) != len(zs) || !checks.confirmed(chinese, parts) {
			continue
		}
		cuts := wordCuts(enCue.Display, parts, words[enCue.ID])
		if len(cuts) != len(parts)-1 {
			cuts = nil
			for _, cue := range zs[1:] {
				cuts = append(cuts, cue.Start)
			}
		}
		bounds := append(append([]int{enCue.Start}, cuts...), enCue.End)
		readable := true
		for i := 0; i+1 < len(bounds); i++ {
			readable = readable && roundCentiseconds(bounds[i+1])-roundCentiseconds(bounds[i]) >= minFallbackFragmentMS
		}
		if readable {
			return parts, bounds
		}
	}
	return nil, nil
}

// splitLongBlocks plans held text once across all of its existing slices.
// Projecting both language plans onto each slice preserves source gaps and
// overlaps, and lets a short translation stay unchanged across new cuts.
func splitLongBlocks(blocks []block, en, zh map[int]Cue, cfg config.LLMConfig, words WordTimings, checks *splitChecks) []block {
	var syncSplitResult []block
	var remaining []block

	for _, b := range blocks {
		// buildBlocks already split these blocks part by part.
		if b.strategy == strategyTimedSplit {
			syncSplitResult = append(syncSplitResult, b)
			continue
		}
		// Only true 1:1 mapping groups can undergo 2+2 synchronized splitting.
		// Slices of N:1, 1:N, or M:N groups must retain unified group planning.
		if b.strategy == strategyOneToOne && len(b.enIDs) == 1 && len(b.zhIDs) == 1 {
			enCue, zhCue := en[b.enIDs[0]], zh[b.zhIDs[0]]
			if synced, ok := trySyncSplit(b, enCue, zhCue, words, cfg, checks); ok {
				syncSplitResult = append(syncSplitResult, synced...)
				continue
			}
		}
		remaining = append(remaining, b)
	}

	plans := func(index map[int]Cue, english bool) map[string]*displayPlan {
		result := map[string]*displayPlan{}
		for _, b := range remaining {
			ids, separator := b.zhIDs, " "
			if english {
				ids, separator = b.enIDs, " "
			}
			if len(ids) == 0 {
				continue
			}
			key := displayKey(b.group, ids, b.top)
			p := result[key]
			if p == nil {
				text := joinCues(index, ids, separator)
				if english && b.enSeparatedRole && b.enText != nil {
					text = *b.enText
				} else if !english && b.zhSeparatedRole && b.zhText != nil {
					text = *b.zhText
				}
				isProtected := false
				isRoleFragment := (english && b.enSeparatedRole) || (!english && b.zhSeparatedRole)
				for _, cue := range cuesFor(index, ids) {
					if cue.MultiSpeaker {
						if isRoleFragment {
							if cueSpeakerLines(sourceLines(text)) < 2 {
								continue
							}
						}
						isProtected = true
						break
					}
				}
				if b.strategy == strategyOneToOne && len(b.enIDs) == 1 && len(b.zhIDs) == 1 {
					enCue, zhCue := en[b.enIDs[0]], zh[b.zhIDs[0]]
					enText, enMulti := sideSpeakerEvidence(b.enText, b.enSeparatedRole, enCue, true)
					enLines := sourceLines(enText)
					zhText, zhMulti := sideSpeakerEvidence(b.zhText, b.zhSeparatedRole, zhCue, true)
					zhLines := sourceLines(zhText)
					if !enMulti && !zhMulti {
						if len(enLines) == 2 && len(zhLines) == 2 {
							enMerged := strings.Join(enLines, " ")
							zhMerged := strings.Join(zhLines, " ")
							enOver := countedLength(enMerged) > cfg.EnChars
							zhOver := countedLength(zhMerged) > cfg.ZhChars
							if !enOver && !zhOver {
								isProtected = true
							} else if english && !enOver {
								isProtected = true
							} else if !english && !zhOver {
								isProtected = true
							}
						}
					}
				}
				p = &displayPlan{group: b.group, ids: ids, top: b.top, start: b.start, end: b.end, text: text, protected: isProtected}
				if isRoleFragment {
					p.plain = text
				} else if len(ids) == 1 {
					p.plain = index[ids[0]].Plain
				}
				result[key] = p
			}
			p.start = min(p.start, b.start)
			p.end = max(p.end, b.end)
			p.windows = append(p.windows, [2]int{b.start, b.end})
		}
		for _, p := range result {
			p.pieces = []textPiece{{start: p.start, end: p.end, text: p.text}}
		}
		return result
	}
	enPlans := plans(en, true)
	for _, p := range enPlans {
		var timing []WordTiming
		if len(p.ids) == 1 {
			timing = words[p.ids[0]]
		}
		planFragments(p, cfg.EnChars, "en", timing, nil, nil)
		if p.wordTimed {
			moveSwitchesToWordCuts(remaining, p)
		}
	}
	zhPlans := plans(zh, false)
	for _, p := range zhPlans {
		var english []textPiece
		for _, e := range enPlans {
			if e.group == p.group && e.top == p.top {
				english = append(english, e.pieces...)
			}
		}
		sort.Slice(english, func(i, j int) bool { return english[i].start < english[j].start })
		planFragments(p, cfg.ZhChars, "zh", nil, english, checks)
	}

	var result []block
	result = append(result, syncSplitResult...)
	for _, b := range remaining {
		ep, zp := enPlans[displayKey(b.group, b.enIDs, b.top)], zhPlans[displayKey(b.group, b.zhIDs, b.top)]
		if (ep == nil || len(ep.pieces) == 1) && (zp == nil || len(zp.pieces) == 1) {
			result = append(result, b)
			continue
		}
		bounds := []int{b.start, b.end}
		for _, p := range []*displayPlan{ep, zp} {
			if p == nil {
				continue
			}
			for _, part := range p.pieces[1:] {
				if b.start < part.start && part.start < b.end {
					bounds = append(bounds, part.start)
				}
			}
		}
		sort.Ints(bounds)
		for i := 0; i+1 < len(bounds); i++ {
			if roundCentiseconds(bounds[i]) >= roundCentiseconds(bounds[i+1]) {
				continue
			}
			child := b
			child.start, child.end = bounds[i], bounds[i+1]
			child.strategy = strategyTimedSplit
			if ep != nil {
				child.enText = pieceAt(ep.pieces, child.start)
			}
			if zp != nil {
				child.zhText = pieceAt(zp.pieces, child.start)
			}
			result = append(result, child)
		}
	}
	return result
}

func pieceAt(pieces []textPiece, at int) *string {
	for i := range pieces {
		if pieces[i].start <= at && at < pieces[i].end {
			return &pieces[i].text
		}
	}
	return nil
}

func planFragments(p *displayPlan, limit int, lang string, words []WordTiming, english []textPiece, checks *splitChecks) {
	if p.protected {
		return
	}
	if lang == "zh" {
		if segments := substantialSegments(english, p.start, p.end); len(segments) >= 2 {
			followSegments(p, segments, checks)
			return
		}
	}
	for _, texts := range textOptions(p.text, p.plain, limit, lang) {
		var candidates [][]int
		wordCandidate := false
		proportional := proportionalCuts(texts, p.start, p.end)
		if lang == "en" {
			if cuts := wordCuts(p.text, texts, words); cuts != nil {
				candidates = append(candidates, cuts)
				wordCandidate = true
			} else if snapped := snapToSwitches(proportional, chineseSwitches(p.windows)); snapped != nil {
				// A cut estimated from text length moves to a nearby
				// Chinese switch, which a person timed by ear.
				candidates = append(candidates, snapped)
			}
		}
		if lang == "zh" && roundCentiseconds(p.end)-roundCentiseconds(p.start) >= len(texts)*minFallbackFragmentMS {
			// Keep the chosen text boundaries when minimum reading times fit.
			earliest := roundCentiseconds(p.start) + minFallbackFragmentMS
			for i, cut := range proportional {
				latest := roundCentiseconds(p.end) - (len(texts)-i-1)*minFallbackFragmentMS
				proportional[i] = min(latest, max(earliest, roundCentiseconds(cut)))
				earliest = proportional[i] + minFallbackFragmentMS
			}
		}
		candidates = append(candidates, proportional)
		for c, cuts := range candidates {
			bounds := append([]int{p.start}, cuts...)
			bounds = append(bounds, p.end)
			parts := make([]textPiece, len(texts))
			for i, text := range texts {
				parts[i] = textPiece{start: bounds[i], end: bounds[i+1], text: text}
			}
			if readablePieces(parts, p.windows) {
				p.pieces = parts
				p.wordTimed = wordCandidate && c == 0
				return
			}
		}
	}
}

// chineseSwitches returns the times inside an English plan where the Chinese
// changes while the English stays: the joins of its contiguous windows.
// Blocks of one English text meet only where the Chinese side changes.
func chineseSwitches(windows [][2]int) []int {
	sorted := append([][2]int(nil), windows...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i][0] < sorted[j][0] })
	var result []int
	for i := 1; i < len(sorted); i++ {
		if sorted[i][0] == sorted[i-1][1] {
			result = append(result, sorted[i][0])
		}
	}
	return result
}

// nearestSwitch returns the switch within snapToleranceMS of cut, other than
// cut itself, or -1.
func nearestSwitch(cut int, switches []int) int {
	best := -1
	for _, at := range switches {
		distance := absInt(at - cut)
		if at != cut && distance <= snapToleranceMS && (best < 0 || distance < absInt(best-cut)) {
			best = at
		}
	}
	return best
}

// snapToSwitches moves each cut to its nearest Chinese switch within
// snapToleranceMS, keeping the cuts in order. It returns nil when no cut
// moves.
func snapToSwitches(cuts, switches []int) []int {
	snapped := append([]int(nil), cuts...)
	moved := false
	for i, cut := range cuts {
		at := nearestSwitch(cut, switches)
		if at < 0 || (i > 0 && at <= snapped[i-1]) || (i+1 < len(cuts) && at >= cuts[i+1]) {
			continue
		}
		snapped[i], moved = at, true
	}
	if !moved {
		return nil
	}
	return snapped
}

// moveSwitchesToWordCuts moves each Chinese switch inside an English plan cut
// at word timings to the word cut within snapToleranceMS, so both languages
// switch together where the words break. A move that leaves either side
// shorter than minFallbackFragmentMS is skipped.
func moveSwitchesToWordCuts(blocks []block, p *displayPlan) {
	key := displayKey(p.group, p.ids, p.top)
	switches := chineseSwitches(p.windows)
	for _, piece := range p.pieces[1:] {
		at := nearestSwitch(piece.start, switches)
		if at < 0 {
			continue
		}
		before, after := -1, -1
		for i, b := range blocks {
			if displayKey(b.group, b.enIDs, b.top) != key {
				continue
			}
			if b.end == at {
				before = i
			}
			if b.start == at {
				after = i
			}
		}
		if before < 0 || after < 0 ||
			piece.start-blocks[before].start < minFallbackFragmentMS ||
			blocks[after].end-piece.start < minFallbackFragmentMS {
			continue
		}
		blocks[before].end, blocks[after].start = piece.start, piece.start
	}
}

// substantialSegments returns the English pieces that are at least half inside
// [start, end): the English parts shown under one Chinese text.
func substantialSegments(english []textPiece, start, end int) []textPiece {
	var result []textPiece
	for _, piece := range english {
		overlap := min(end, piece.end) - max(start, piece.start)
		if overlap > 0 && 2*overlap >= piece.end-piece.start {
			result = append(result, piece)
		}
	}
	return result
}

// followSegments plans Chinese text that is shown over several English parts.
// The Chinese text splits only at its own source line breaks, one line per
// English part, switching at the English part starts, and only when the order
// check confirms the pairs. A generated cut could fall inside a word.
// Otherwise it stays whole: a Chinese part next to the wrong English part is
// worse than a long line.
func followSegments(p *displayPlan, segments []textPiece, checks *splitChecks) {
	texts := sourceLines(p.plain)
	if len(texts) != len(segments) {
		return
	}
	english := make([]string, len(segments))
	parts := make([]textPiece, len(texts))
	for i, text := range texts {
		english[i] = segments[i].text
		start, end := p.start, p.end
		if i > 0 {
			start = segments[i].start
		}
		if i+1 < len(texts) {
			end = segments[i+1].start
		}
		if start >= end {
			return
		}
		parts[i] = textPiece{start: start, end: end, text: text}
	}
	// Ask before the timing check: Align asks without word timings, which can
	// change the English part starts at render time.
	if checks.confirmed(texts, english) && readablePieces(parts, p.windows) {
		p.pieces = parts
	}
}

// sourceSingleLine identifies a source line that can exceed display limits.
// An empty plain field provides no evidence for this exception.
func sourceSingleLine(plain string) bool {
	plain = strings.TrimSpace(plain)
	return plain != "" && !strings.ContainsAny(plain, "\r\n")
}

// countedLength is the length checked against zh_chars and en_chars.
// Punctuation and symbols do not count; spaces and every other character do.
func countedLength(text string) int {
	n := 0
	for _, r := range text {
		if countsTowardLimit(r) {
			n++
		}
	}
	return n
}

func countsTowardLimit(r rune) bool {
	return !unicode.IsPunct(r) && !unicode.IsSymbol(r)
}

// countedPrefix returns p with p[i] = countedLength(string(runes[:i])).
func countedPrefix(runes []rune) []int {
	p := make([]int, len(runes)+1)
	for i, r := range runes {
		p[i+1] = p[i]
		if countsTowardLimit(r) {
			p[i+1]++
		}
	}
	return p
}

// limitIndex returns the largest rune index i such that runes[:i] counts at
// most limit characters, so punctuation after the last counted character
// stays with it.
func limitIndex(runes []rune, limit int) int {
	count := 0
	for i, r := range runes {
		if countsTowardLimit(r) {
			if count == limit {
				return i
			}
			count++
		}
	}
	return len(runes)
}

// textOptions keeps original single lines. It considers source line breaks
// when the flattened multiline text exceeds the configured limit.
func textOptions(text, plain string, limit int, lang string) [][]string {
	if limit <= 0 || countedLength(text) <= limit || sourceSingleLine(plain) {
		return nil
	}
	var result [][]string
	lines := sourceLines(plain)
	if len(lines) > 1 && compactText(strings.Join(lines, ""), false) == compactText(text, false) && partsFit(lines, limit) {
		result = append(result, lines)
	}
	if balanced := balancedParts(text, limit, lang); len(balanced) > 1 && partsFit(balanced, limit) {
		if len(result) == 0 || strings.Join(result[0], "\n") != strings.Join(balanced, "\n") {
			result = append(result, balanced)
		}
	}
	return result
}

func partsFit(parts []string, limit int) bool {
	for _, part := range parts {
		if strings.TrimSpace(part) == "" || countedLength(part) > limit {
			return false
		}
	}
	return true
}

func balancedParts(text string, limit int, lang string) []string {
	count := (countedLength(text) + limit - 1) / limit
	maxParts := utf8.RuneCountInString(text)
	if lang == "en" {
		fields := strings.Fields(text)
		maxParts = len(fields)
		for _, field := range fields {
			if countedLength(field) > limit {
				return nil
			}
		}
	}
	for ; count <= maxParts; count++ {
		if result := balancedPartCount(text, limit, count, lang); result != nil {
			return result
		}
	}
	return nil
}

func balancedPartCount(text string, limit, count int, lang string) []string {
	runes := []rune(text)
	var result []string
	for remaining := count; remaining > 1; remaining-- {
		counted := countedPrefix(runes)
		total := counted[len(runes)]
		target := total / remaining
		best, score := -1, int(^uint(0)>>1)
		for i := 1; i < len(runes); i++ {
			if counted[i] > limit || total-counted[i] > (remaining-1)*limit {
				continue
			}
			if !isSafeCutIndex(runes, i) {
				continue
			}
			if lang == "en" && !unicode.IsSpace(runes[i]) {
				continue
			}
			if strings.ContainsRune("，。！？；：、…）】》”’", runes[i]) || unicode.IsMark(runes[i]) {
				continue
			}
			s := absInt(counted[i]-target) * 4
			if strings.ContainsRune(",.;:!?，。！？；：、", runes[i-1]) {
				s -= 12
			} else if unicode.IsSpace(runes[i-1]) || unicode.IsSpace(runes[i]) {
				s -= 4
			}
			if s < score {
				best, score = i, s
			}
		}
		if best < 0 {
			return nil
		}
		result = append(result, strings.TrimSpace(string(runes[:best])))
		runes = []rune(strings.TrimSpace(string(runes[best:])))
	}
	return append(result, strings.TrimSpace(string(runes)))
}

// isSafeCutIndex reports whether cutting text between runes[i-1] and runes[i]
// keeps grapheme clusters intact. It conservatively rejects any position that
// could split a ZWJ sequence, a combining mark, a variation selector, an emoji
// modifier, a keycap or a regional-indicator flag pair. When in doubt it refuses
// the cut so callers keep the complete original text.
func isSafeCutIndex(runes []rune, i int) bool {
	if i <= 0 || i >= len(runes) {
		return false
	}
	left, right := runes[i-1], runes[i]
	if isGraphemeContinuation(right) || isGraphemeContinuation(left) {
		return false
	}
	// ZWJ sequences: never break immediately before or after U+200D.
	if left == '\u200d' || right == '\u200d' {
		return false
	}
	// Regional indicator pairs form one flag: do not cut between them.
	if isRegionalIndicator(left) && isRegionalIndicator(right) {
		return false
	}
	return true
}

// isGraphemeContinuation reports runes that visually attach to the preceding
// rune and therefore cannot start a new line on their own.
func isGraphemeContinuation(r rune) bool {
	switch {
	case unicode.IsMark(r):
		return true
	case r == '\ufe0e', r == '\ufe0f': // text/emoji variation selectors
		return true
	case r >= 0x1F3FB && r <= 0x1F3FF: // skin tone modifiers
		return true
	case r >= 0xE0020 && r <= 0xE007F: // emoji tag sequence components
		return true
	case r == 0x20E3: // combining enclosing keycap
		return true
	}
	return false
}

func isRegionalIndicator(r rune) bool { return r >= 0x1F1E6 && r <= 0x1F1FF }

func proportionalCuts(parts []string, start, end int) []int {
	total := 0
	for _, part := range parts {
		total += utf8.RuneCountInString(part)
	}
	var result []int
	prefix := 0
	for _, part := range parts[:len(parts)-1] {
		prefix += utf8.RuneCountInString(part)
		result = append(result, start+int((int64(end-start)*int64(prefix)+int64(total/2))/int64(total)))
	}
	return result
}

func compactText(text string, speech bool) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || (speech && !unicode.IsLetter(r) && !unicode.IsNumber(r)) {
			return -1
		}
		if speech {
			return unicode.ToLower(r)
		}
		return r
	}, text)
}

func wordCuts(text string, parts []string, words []WordTiming) []int {
	if len(words) < 2 {
		return nil
	}
	var joined strings.Builder
	for i, word := range words {
		if word.StartMS < 0 || word.EndMS < word.StartMS || (i > 0 && word.StartMS < words[i-1].EndMS) {
			return nil
		}
		joined.WriteString(compactText(word.Text, true))
	}
	if joined.String() != compactText(text, true) {
		return nil
	}
	var cuts []int
	target, reached, cursor := 0, 0, 0
	for _, part := range parts[:len(parts)-1] {
		target += utf8.RuneCountInString(compactText(part, true))
		for cursor < len(words) && reached < target {
			reached += utf8.RuneCountInString(compactText(words[cursor].Text, true))
			cursor++
		}
		if reached != target || cursor == 0 || cursor == len(words) {
			return nil
		}
		left, right := words[cursor-1], words[cursor]
		if left.StartMS == left.EndMS || right.StartMS == right.EndMS {
			return nil
		}
		cuts = append(cuts, left.EndMS+(right.StartMS-left.EndMS)/2)
	}
	return cuts
}

func readablePieces(parts []textPiece, windows [][2]int) bool {
	return readablePiecesWithFloor(parts, windows, minFallbackFragmentMS)
}

func readablePiecesWithFloor(parts []textPiece, windows [][2]int, floorMS int) bool {
	for _, part := range parts {
		if roundCentiseconds(part.end)-roundCentiseconds(part.start) < floorMS {
			return false
		}
		var visible [][2]int
		for _, window := range windows {
			start, end := max(part.start, window[0]), min(part.end, window[1])
			if start < end {
				visible = append(visible, [2]int{start, end})
			}
		}
		sort.Slice(visible, func(i, j int) bool { return visible[i][0] < visible[j][0] })
		longest, start, end := 0, 0, -1
		for _, interval := range visible {
			if interval[0] > end {
				start, end = interval[0], interval[1]
			} else {
				end = max(end, interval[1])
			}
			longest = max(longest, roundCentiseconds(end)-roundCentiseconds(start))
		}
		if longest < floorMS {
			return false
		}
	}
	return true
}

// holdToleranceMS is how far a Chinese cue may run into an English gap and
// still count as finished at the gap. Shorter overruns are timing noise
// between the tracks, not an intentional hold over the gap.
const holdToleranceMS = 300

// adjustCutsForSourceGaps rolls back candidate cut points that land just after an
// English source gap when the earlier Chinese cue already finished at or before the
// gap (cues[i].End <= gapStart + holdToleranceMS) and already had sufficient reading
// time in the prior window, preventing unreadable flashback tails from projecting
// across the gap.
func adjustCutsForSourceGaps(cuts []int, cues []Cue, windows [][2]int) []int {
	if len(cuts) == 0 || len(windows) < 2 {
		return cuts
	}
	adjusted := append([]int(nil), cuts...)
	for i := 0; i < len(adjusted) && i+1 < len(cues); i++ {
		cut := adjusted[i]
		for w := 0; w+1 < len(windows); w++ {
			gapStart := windows[w][1]
			gapEnd := windows[w+1][0]
			if gapStart >= gapEnd {
				continue
			}
			// If the cut lands inside the window after the gap, but the earlier Chinese
			// cue already finished at or before the gap, had sufficient display in the
			// earlier window (evaluated in ASS centiseconds), and lands in the next window
			// as an unreadably short tail, roll back the cut to the gap boundary so the old
			// cue does not flash back.
			if cut > gapEnd && cut < windows[w+1][1] {
				cueVisibleStart := windows[w][0]
				if i > 0 && adjusted[i-1] > cueVisibleStart {
					cueVisibleStart = adjusted[i-1]
				}
				priorDuration := roundCentiseconds(gapStart) - roundCentiseconds(cueVisibleStart)
				tailDuration := roundCentiseconds(cut) - roundCentiseconds(gapEnd)
				if cues[i].End <= gapStart+holdToleranceMS && priorDuration >= minFallbackFragmentMS && tailDuration < minFallbackFragmentMS {
					newCut := gapEnd
					if (i == 0 || newCut > adjusted[i-1]) && (i+1 == len(adjusted) || newCut < adjusted[i+1]) {
						adjusted[i] = newCut
					}
					break
				}
			}
		}
	}
	return adjusted
}

// snapToleranceMS is how far a Chinese cut may move to switch together with
// the nearest English cue start. Up to snapCheckedToleranceMS it moves only
// when the order check confirms that both tracks break at the same place.
const (
	snapToleranceMS        = 500
	snapCheckedToleranceMS = 1000
)

// splitChineseSources restores source fragments inside the English outer
// bounds. Invalid or unreadably short source cuts use proportional timing.
// If any source would become invisible in an English gap, retain the hold.
func splitChineseSources(slices []block, cues []Cue, en map[int]Cue, checks *splitChecks) []block {
	start, end := slices[0].start, slices[0].end
	var windows [][2]int
	for _, b := range slices {
		start, end = min(start, b.start), max(end, b.end)
		windows = append(windows, [2]int{b.start, b.end})
	}
	texts, cuts := make([]string, len(cues)), make([]int, 0, len(cues)-1)
	for i, cue := range cues {
		texts[i] = cue.Display
		if i > 0 {
			cuts = append(cuts, cue.Start)
		}
	}
	snapped := snapCuts(cuts, slices, cues, en, checks)
	cuts = adjustCutsForSourceGaps(cuts, cues, windows)
	proportional := adjustCutsForSourceGaps(proportionalCuts(texts, start, end), cues, windows)
	for _, candidate := range [][]int{snapped, cuts, proportional} {
		if len(candidate) != len(cues)-1 {
			continue
		}
		bounds := append([]int{start}, candidate...)
		bounds = append(bounds, end)
		pieces := make([]textPiece, len(cues))
		for i, cue := range cues {
			pieces[i] = textPiece{start: bounds[i], end: bounds[i+1], text: cue.Display}
		}
		if !readablePieces(pieces, windows) {
			continue
		}
		var result []block
		for _, b := range slices {
			for i, part := range pieces {
				lo, hi := max(b.start, part.start), min(b.end, part.end)
				if lo < hi {
					child := b
					child.start, child.end, child.zhIDs = lo, hi, []int{cues[i].ID}
					child.strategy = strategyMNSourceCuts
					result = append(result, child)
				}
			}
		}
		return result
	}
	return slices
}

// snapCuts moves each Chinese cut to the nearest English cue start inside the
// group, so both languages switch together. It returns nil when no cut moves.
func snapCuts(cuts []int, slices []block, cues []Cue, en map[int]Cue, checks *splitChecks) []int {
	snapped := append([]int(nil), cuts...)
	moved := false
	for i, cut := range cuts {
		best := -1
		for s := 1; s < len(slices); s++ {
			if best < 0 || absInt(slices[s].start-cut) < absInt(slices[best].start-cut) {
				best = s
			}
		}
		if best < 0 || slices[best].start == cut {
			continue
		}
		target, distance := slices[best].start, absInt(slices[best].start-cut)
		if distance > snapCheckedToleranceMS || (i > 0 && target <= snapped[i-1]) || (i+1 < len(cuts) && target >= cuts[i+1]) {
			continue
		}
		if distance > snapToleranceMS {
			before, after := en[slices[best-1].enIDs[0]], en[slices[best].enIDs[0]]
			if !checks.confirmed([]string{cues[i].Display, cues[i+1].Display}, []string{before.Display, after.Display}) {
				continue
			}
		}
		snapped[i], moved = target, true
	}
	if !moved {
		return nil
	}
	return snapped
}
