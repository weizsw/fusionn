package subtitlellm

import (
	"sort"
	"strings"
)

// Global offset search between the two tracks. Releases often differ by a
// recap or an intro, so the search covers three minutes either way.
const (
	shiftSearchMS    = 180000
	shiftStepMS      = 100
	shiftToleranceMS = 250
	// minShiftMS leaves near-synced tracks untouched: small offsets are
	// absorbed by the model and by overlap pairing.
	minShiftMS = 500
	// minShiftMatches stops a few coincidental starts, for example in a
	// signs-only track, from moving the whole track.
	minShiftMatches = 20
)

// maxTimingGroupCues caps each language in one time-paired group. Larger
// overlap clusters are usually dense cross-talk and are paired 1:1 instead.
const maxTimingGroupCues = 3

// estimateZHShift returns the milliseconds to add to Chinese cue times so that
// their starts line up with English starts. It returns 0 unless one shift
// matches clearly more starts than the current timing: at least twice as many,
// at least minShiftMatches, and at least a fifth of the Chinese cues.
func estimateZHShift(en, zh []Cue) int {
	if len(en) == 0 || len(zh) == 0 {
		return 0
	}
	starts := make([]int, len(en))
	for i, cue := range en {
		starts[i] = cue.Start
	}
	sort.Ints(starts)
	// nearest returns the English start closest to t within the tolerance.
	nearest := func(t int) (int, bool) {
		best, found := 0, false
		for i := sort.SearchInts(starts, t-shiftToleranceMS); i < len(starts) && starts[i] <= t+shiftToleranceMS; i++ {
			if !found || absInt(starts[i]-t) < absInt(best-t) {
				best, found = starts[i], true
			}
		}
		return best, found
	}
	matches := func(shift int) int {
		count := 0
		for _, cue := range zh {
			t := cue.Start + shift
			if i := sort.SearchInts(starts, t-shiftToleranceMS); i < len(starts) && starts[i] <= t+shiftToleranceMS {
				count++
			}
		}
		return count
	}
	zeroCount := matches(0)
	best, bestCount := 0, zeroCount
	for shift := -shiftSearchMS; shift <= shiftSearchMS; shift += shiftStepMS {
		if count := matches(shift); count > bestCount {
			best, bestCount = shift, count
		}
	}
	if bestCount < 2*zeroCount || bestCount < minShiftMatches || bestCount*5 < len(zh) {
		return 0
	}
	// Refine the coarse grid with the median start difference of matched cues.
	diffs := make([]int, 0, bestCount)
	for _, cue := range zh {
		if start, ok := nearest(cue.Start + best); ok {
			diffs = append(diffs, start-cue.Start)
		}
	}
	sort.Ints(diffs)
	shift := diffs[len(diffs)/2]
	if absInt(shift) < minShiftMS {
		return 0
	}
	return shift
}

// shiftCues returns copies of cues moved by shift milliseconds. OriginalStart
// and OriginalEnd keep the source file times. A cue moved before zero starts
// at zero and keeps what remains of its duration, or all of it if nothing
// remains.
func shiftCues(cues []Cue, shift int) []Cue {
	out := make([]Cue, len(cues))
	for i, cue := range cues {
		start, end := cue.Start+shift, cue.End+shift
		if end <= 0 {
			start, end = 0, cue.End-cue.Start
		} else if start < 0 {
			start = 0
		}
		cue.Start, cue.End = start, end
		out[i] = cue
	}
	return out
}

// isTimingDialogue reports whether a cue looks like ordinary dialogue that may
// be paired by timing alone. Top-positioned cues, lyrics and lines wrapped in
// brackets (sound cues, on-screen captions such as "（伦敦）") stay one-sided,
// because a time overlap says little about them.
func isTimingDialogue(c Cue) bool {
	if c.Top {
		return false
	}
	text := strings.TrimSpace(c.Plain)
	if text == "" || isLyrics(text) {
		return false
	}
	runes := []rune(text)
	closing, ok := closingBrackets[runes[0]]
	return !(ok && len(runes) > 1 && runes[len(runes)-1] == closing)
}

var closingBrackets = map[rune]rune{'(': ')', '（': '）', '[': ']', '【': '】'}

// isLyrics reports whether every line is a song lyric. A cue that mixes a lyric
// line with a spoken line ("- ♪ Silent night ♪ / - Yeah, thank you.") still
// counts as dialogue.
func isLyrics(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.ContainsAny(line, "♪♫") {
			return false
		}
	}
	return true
}

// pairRestoredByTiming pairs restored one-sided dialogue cues by time overlap.
// Model groups, including deliberate one-sided rows, are kept as they are.
// Only cues the model did not place (failed windows, rejected rows,
// quarantined conflicts) are paired, so a failed request degrades to
// time-based pairing instead of separate Chinese and English lines.
//
// Two cues overlap enough when the overlap covers at least half of the shorter
// cue. Overlap clusters become one group when each language has at most
// maxTimingGroupCues cues that are adjacent in track order; other clusters
// keep only mutual best 1:1 pairs. It returns the new groups and the paired
// cue IDs per language.
func pairRestoredByTiming(bundle Bundle, groups []Group) ([]Group, map[string][]int) {
	paired := map[string][]int{"en": {}, "zh": {}}
	type member struct {
		cue Cue
		pos int // index in the full track
	}
	collect := func(cues []Cue, restored map[int]bool) []member {
		var out []member
		for pos, cue := range cues {
			if restored[cue.ID] && isTimingDialogue(cue) {
				out = append(out, member{cue: cue, pos: pos})
			}
		}
		return out
	}
	restored := map[string]map[int]bool{"en": {}, "zh": {}}
	for _, g := range groups {
		if g.Origin != OriginRestored || g.Layer != LayerBottom {
			continue
		}
		for _, id := range g.ENIDs {
			restored["en"][id] = true
		}
		for _, id := range g.ZHIDs {
			restored["zh"][id] = true
		}
	}
	en := collect(bundle.EN, restored["en"])
	zh := collect(bundle.ZH, restored["zh"])
	if len(en) == 0 || len(zh) == 0 {
		return groups, paired
	}

	overlap := make(map[[2]int]int)
	parent := make([]int, len(en)+len(zh))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		if parent[i] != i {
			parent[i] = find(parent[i])
		}
		return parent[i]
	}
	for i, e := range en {
		for j, z := range zh {
			shared := min(e.cue.End, z.cue.End) - max(e.cue.Start, z.cue.Start)
			shorter := min(e.cue.End-e.cue.Start, z.cue.End-z.cue.Start)
			if shared > 0 && 2*shared >= shorter {
				overlap[[2]int{i, j}] = shared
				parent[find(i)] = find(len(en) + j)
			}
		}
	}

	type cluster struct{ en, zh []int }
	clusters := map[int]*cluster{}
	var roots []int
	add := func(root int) *cluster {
		c, ok := clusters[root]
		if !ok {
			c = &cluster{}
			clusters[root] = c
			roots = append(roots, root)
		}
		return c
	}
	for i := range en {
		c := add(find(i))
		c.en = append(c.en, i)
	}
	for j := range zh {
		c := add(find(len(en) + j))
		c.zh = append(c.zh, j)
	}

	adjacent := func(members []member, indexes []int) bool {
		for k := 1; k < len(indexes); k++ {
			if members[indexes[k]].pos != members[indexes[k-1]].pos+1 {
				return false
			}
		}
		return true
	}
	var created []Group
	newGroup := func(enIdx, zhIdx []int) {
		g := Group{ENIDs: []int{}, ZHIDs: []int{}, Layer: LayerBottom, Origin: OriginTiming, Kind: KindDialogue}
		for _, i := range enIdx {
			g.ENIDs = append(g.ENIDs, en[i].cue.ID)
			paired["en"] = append(paired["en"], en[i].cue.ID)
		}
		for _, j := range zhIdx {
			g.ZHIDs = append(g.ZHIDs, zh[j].cue.ID)
			paired["zh"] = append(paired["zh"], zh[j].cue.ID)
		}
		created = append(created, g)
	}
	for _, root := range roots {
		c := clusters[root]
		if len(c.en) == 0 || len(c.zh) == 0 {
			continue
		}
		if len(c.en) <= maxTimingGroupCues && len(c.zh) <= maxTimingGroupCues && adjacent(en, c.en) && adjacent(zh, c.zh) {
			newGroup(c.en, c.zh)
			continue
		}
		bestZH := map[int]int{}
		bestEN := map[int]int{}
		for _, i := range c.en {
			for _, j := range c.zh {
				shared, ok := overlap[[2]int{i, j}]
				if !ok {
					continue
				}
				if k, seen := bestZH[i]; !seen || shared > overlap[[2]int{i, k}] {
					bestZH[i] = j
				}
				if k, seen := bestEN[j]; !seen || shared > overlap[[2]int{k, j}] {
					bestEN[j] = i
				}
			}
		}
		for _, i := range c.en {
			if j, ok := bestZH[i]; ok && bestEN[j] == i {
				newGroup([]int{i}, []int{j})
			}
		}
	}
	if len(created) == 0 {
		return groups, paired
	}

	used := map[string]map[int]bool{"en": {}, "zh": {}}
	for _, lang := range []string{"en", "zh"} {
		for _, id := range paired[lang] {
			used[lang][id] = true
		}
		sort.Ints(paired[lang])
	}
	out := make([]Group, 0, len(groups))
	for _, g := range groups {
		if g.Origin == OriginRestored && ((len(g.ENIDs) == 1 && used["en"][g.ENIDs[0]]) || (len(g.ZHIDs) == 1 && used["zh"][g.ZHIDs[0]])) {
			continue
		}
		out = append(out, g)
	}
	out = append(out, created...)
	tracks := map[string]map[int]Cue{"en": {}, "zh": {}}
	for _, cue := range bundle.EN {
		tracks["en"][cue.ID] = cue
	}
	for _, cue := range bundle.ZH {
		tracks["zh"][cue.ID] = cue
	}
	sort.SliceStable(out, func(i, j int) bool {
		return firstStart(tracks, out[i]) < firstStart(tracks, out[j])
	})
	return out, paired
}

// LowPairingRatio is the English pairing share below which a merge is
// reported as suspicious: the Chinese track may belong to another cut, another
// episode or another language.
const LowPairingRatio = 0.6

// Pairing counts bottom dialogue cues that share a group with the other
// language. It is the main quality signal of a bilingual merge: each unpaired
// cue is shown without its translation.
type Pairing struct {
	ENPaired int `json:"en_paired"`
	ENTotal  int `json:"en_total"`
	ZHPaired int `json:"zh_paired"`
	ZHTotal  int `json:"zh_total"`
}

// ENRatio returns the share of English dialogue cues with a Chinese partner.
func (p Pairing) ENRatio() float64 { return ratio(p.ENPaired, p.ENTotal) }

// ZHRatio returns the share of Chinese dialogue cues with an English partner.
func (p Pairing) ZHRatio() float64 { return ratio(p.ZHPaired, p.ZHTotal) }

// Low reports whether English dialogue exists and less than LowPairingRatio of
// it has a Chinese partner.
func (p Pairing) Low() bool { return p.ENTotal > 0 && p.ENRatio() < LowPairingRatio }

func ratio(part, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) / float64(total)
}

// pairingStats counts dialogue cues in bottom groups that are not on-screen
// text. Cues isTimingDialogue rejects (lyrics, bracketed captions) are left
// out, as they often have no counterpart by design.
func pairingStats(bundle Bundle, groups []Group) Pairing {
	tracks := map[string]map[int]Cue{"en": {}, "zh": {}}
	for _, cue := range bundle.EN {
		tracks["en"][cue.ID] = cue
	}
	for _, cue := range bundle.ZH {
		tracks["zh"][cue.ID] = cue
	}
	var p Pairing
	for _, g := range groups {
		if g.Layer != LayerBottom || g.Kind == KindOnScreenText {
			continue
		}
		bilingual := len(g.ENIDs) > 0 && len(g.ZHIDs) > 0
		for _, id := range g.ENIDs {
			if isTimingDialogue(tracks["en"][id]) {
				p.ENTotal++
				if bilingual {
					p.ENPaired++
				}
			}
		}
		for _, id := range g.ZHIDs {
			if isTimingDialogue(tracks["zh"][id]) {
				p.ZHTotal++
				if bilingual {
					p.ZHPaired++
				}
			}
		}
	}
	return p
}
