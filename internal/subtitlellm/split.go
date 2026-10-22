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
		if !z.MultiSpeaker && !z.Top && len(sourceLines(z.Plain)) == 2 {
			zh2Lines[z.ID] = true
		}
	}
	for _, cue := range bundle.EN {
		if cue.MultiSpeaker || cue.Top || cue.End-cue.Start > 30000 {
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
// It requires >= 1.2s per fragment on the real candidate timestamps.
func trySyncSplit(b block, enCue, zhCue Cue, words WordTimings) ([]block, bool) {
	enText, enMulti := sideSpeakerEvidence(b.enText, b.enSeparatedRole, enCue, true)
	enDisplay := enCue.Display
	if b.enSeparatedRole && b.enText != nil {
		enDisplay = *b.enText
	}
	zhText, zhMulti := sideSpeakerEvidence(b.zhText, b.zhSeparatedRole, zhCue, true)
	if enMulti || zhMulti || b.top {
		return nil, false
	}
	enLines := sourceLines(enText)
	zhLines := sourceLines(zhText)
	if len(enLines) != 2 || len(zhLines) != 2 {
		return nil, false
	}
	totalDuration := roundCentiseconds(b.end) - roundCentiseconds(b.start)
	if totalDuration < 2*minSyncFragmentMS {
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

// splitLongBlocks plans held text once across all of its existing slices.
// Projecting both language plans onto each slice preserves source gaps and
// overlaps, and lets a short translation stay unchanged across new cuts.
func splitLongBlocks(blocks []block, en, zh map[int]Cue, cfg config.LLMConfig, words WordTimings) []block {
	var syncSplitResult []block
	var remaining []block

	for _, b := range blocks {
		// Only true 1:1 mapping groups can undergo 2+2 synchronized splitting.
		// Slices of N:1, 1:N, or M:N groups must retain unified group planning.
		if b.strategy == strategyOneToOne && len(b.enIDs) == 1 && len(b.zhIDs) == 1 {
			enCue, zhCue := en[b.enIDs[0]], zh[b.zhIDs[0]]
			if synced, ok := trySyncSplit(b, enCue, zhCue, words); ok {
				syncSplitResult = append(syncSplitResult, synced...)
				continue
			}
		}
		remaining = append(remaining, b)
	}

	plans := func(index map[int]Cue, english bool) map[string]*displayPlan {
		result := map[string]*displayPlan{}
		for _, b := range remaining {
			ids, separator := b.zhIDs, "　"
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
				isProtected := b.top
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
					if !enMulti && !zhMulti && !b.top {
						if len(enLines) == 2 && len(zhLines) == 2 {
							enMerged := strings.Join(enLines, " ")
							zhMerged := strings.Join(zhLines, "　")
							enOver := utf8.RuneCountInString(enMerged) > cfg.EnChars
							zhOver := utf8.RuneCountInString(zhMerged) > cfg.ZhChars
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
	enPlans, zhPlans := plans(en, true), plans(zh, false)
	for _, p := range enPlans {
		var timing []WordTiming
		if len(p.ids) == 1 {
			timing = words[p.ids[0]]
		}
		planFragments(p, cfg.EnChars, "en", timing, nil)
	}
	for _, p := range zhPlans {
		var english []textPiece
		for _, e := range enPlans {
			if e.group == p.group && e.top == p.top {
				english = append(english, e.pieces...)
			}
		}
		sort.Slice(english, func(i, j int) bool { return english[i].start < english[j].start })
		planFragments(p, cfg.ZhChars, "zh", nil, english)
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

func planFragments(p *displayPlan, limit int, lang string, words []WordTiming, english []textPiece) {
	if p.protected {
		return
	}
	for _, texts := range textOptions(p.text, p.plain, limit, lang) {
		var candidates [][]int
		if lang == "en" {
			if cuts := wordCuts(p.text, texts, words); cuts != nil {
				candidates = append(candidates, cuts)
			}
		} else if cuts := sharedCuts(english, len(texts), p.start, p.end); cuts != nil {
			candidates = append(candidates, cuts)
		}
		proportional := proportionalCuts(texts, p.start, p.end)
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
		for _, cuts := range candidates {
			bounds := append([]int{p.start}, cuts...)
			bounds = append(bounds, p.end)
			parts := make([]textPiece, len(texts))
			for i, text := range texts {
				parts[i] = textPiece{start: bounds[i], end: bounds[i+1], text: text}
			}
			if readablePieces(parts, p.windows) {
				p.pieces = parts
				return
			}
		}
	}
}

// sourceSingleLine identifies a source line that can exceed display limits.
// An empty plain field provides no evidence for this exception.
func sourceSingleLine(plain string) bool {
	plain = strings.TrimSpace(plain)
	return plain != "" && !strings.ContainsAny(plain, "\r\n")
}

// textOptions keeps original single lines. It considers source line breaks
// when the flattened multiline text exceeds the configured limit.
func textOptions(text, plain string, limit int, lang string) [][]string {
	if limit <= 0 || utf8.RuneCountInString(text) <= limit || sourceSingleLine(plain) {
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
		if strings.TrimSpace(part) == "" || utf8.RuneCountInString(part) > limit {
			return false
		}
	}
	return true
}

func balancedParts(text string, limit int, lang string) []string {
	count := (utf8.RuneCountInString(text) + limit - 1) / limit
	maxParts := utf8.RuneCountInString(text)
	if lang == "en" {
		fields := strings.Fields(text)
		maxParts = len(fields)
		for _, field := range fields {
			if utf8.RuneCountInString(field) > limit {
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
		low, high := max(1, len(runes)-(remaining-1)*limit), min(limit, len(runes)-1)
		target := len(runes) / remaining
		best, score := -1, int(^uint(0)>>1)
		for i := low; i <= high; i++ {
			if !isSafeCutIndex(runes, i) {
				continue
			}
			if lang == "en" && !unicode.IsSpace(runes[i]) {
				continue
			}
			if strings.ContainsRune("，。！？；：、…）】》”’", runes[i]) || unicode.IsMark(runes[i]) {
				continue
			}
			s := absInt(i-target) * 4
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

func sharedCuts(english []textPiece, count, start, end int) []int {
	if len(english) != count || english[0].start != start || english[len(english)-1].end != end {
		return nil
	}
	var cuts []int
	for i := 1; i < len(english); i++ {
		if english[i-1].end != english[i].start {
			return nil
		}
		cuts = append(cuts, english[i].start)
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

// adjustCutsForSourceGaps rolls back candidate cut points that land just after an
// English source gap when the earlier Chinese cue already finished at or before the
// gap (cues[i].End <= gapStart) and already had sufficient reading time in the
// prior window, preventing unreadable flashback tails from projecting across the gap.
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
				if cues[i].End <= gapStart && priorDuration >= minFallbackFragmentMS && tailDuration < minFallbackFragmentMS {
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

// splitChineseSources restores source fragments inside the English outer
// bounds. Invalid or unreadably short source cuts use proportional timing.
// If any source would become invisible in an English gap, retain the hold.
func splitChineseSources(slices []block, cues []Cue) []block {
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
	cuts = adjustCutsForSourceGaps(cuts, cues, windows)
	proportional := adjustCutsForSourceGaps(proportionalCuts(texts, start, end), cues, windows)
	for _, candidate := range [][]int{cuts, proportional} {
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
