package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/fusionn/internal/subtitlellm"
)

type SemanticReviewItem struct {
	Fingerprint string `json:"fingerprint"`
	ENIDs       []int  `json:"en_ids"`
	ZHIDs       []int  `json:"zh_ids"`
	ENText      string `json:"en_text"`
	ZHText      string `json:"zh_text"`
	Verdict     string `json:"verdict"`
	Reason      string `json:"reason"`
	Note        string `json:"note"`
}

type SemanticInheritanceStats struct {
	TotalEntries    int `json:"total_entries"`
	InheritedCount  int `json:"inherited_count"`
	UnreviewedCount int `json:"unreviewed_count"`
	FlaggedCount    int `json:"flagged_count"`
}

func ComputeBundleFingerprint(bundle subtitlellm.Bundle) string {
	if bundle.Fingerprint != "" {
		return bundle.Fingerprint
	}
	h := sha256.New()
	for _, c := range bundle.EN {
		fmt.Fprintf(h, "en:%d:%d:%d:%s:%t\n", c.ID, c.Start, c.End, c.Plain, c.Top)
	}
	for _, c := range bundle.ZH {
		fmt.Fprintf(h, "zh:%d:%d:%d:%s:%t\n", c.ID, c.Start, c.End, c.Plain, c.Top)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func parseASSTimestamp(s string) (int, error) {
	// Format: H:MM:SS.CC or HH:MM:SS.CC
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) != 3 {
		return 0, fmt.Errorf("invalid ASS timestamp: %q", s)
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, err
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, err
	}
	secParts := strings.Split(parts[2], ".")
	if len(secParts) != 2 {
		return 0, fmt.Errorf("invalid seconds in ASS timestamp: %q", s)
	}
	sec, err := strconv.Atoi(secParts[0])
	if err != nil {
		return 0, err
	}
	cs, err := strconv.Atoi(secParts[1])
	if err != nil {
		return 0, err
	}
	// Centennial seconds (1/100s = 10ms)
	ms := (h*3600+m*60+sec)*1000 + cs*10
	return ms, nil
}

var (
	assTagRegex   = regexp.MustCompile(`\{[^}]*\}`)
	alignTagRegex = regexp.MustCompile(`\\an(\d)`)
)

type ParsedASSDialogue struct {
	StartMS   int
	EndMS     int
	Style     string
	Alignment int
	Top       bool
	RawText   string
	CleanText string
	ENText    string
	ZHText    string
}

func hasHanCharacters(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

func ParseDialogueEventsFromASS(assContent string, bundle subtitlellm.Bundle, mapping subtitlellm.Mapping) ([]RenderedEvent, error) {
	norm := strings.ReplaceAll(assContent, "\r\n", "\n")
	lines := strings.Split(norm, "\n")

	var dialogues []ParsedASSDialogue
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "Dialogue:") {
			continue
		}
		// Format: Dialogue: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
		fields := strings.SplitN(trimmed, ",", 10)
		if len(fields) < 10 {
			continue
		}

		startMS, err := parseASSTimestamp(fields[1])
		if err != nil {
			return nil, fmt.Errorf("parse start time %q: %w", fields[1], err)
		}
		endMS, err := parseASSTimestamp(fields[2])
		if err != nil {
			return nil, fmt.Errorf("parse end time %q: %w", fields[2], err)
		}
		style := strings.TrimSpace(fields[3])
		rawText := fields[9]

		// Extract alignment
		align := 2
		if strings.HasPrefix(style, "Top") {
			align = 8
		}
		if match := alignTagRegex.FindStringSubmatch(rawText); len(match) > 1 {
			if a, err := strconv.Atoi(match[1]); err == nil {
				align = a
			}
		}

		top := align >= 7 || strings.HasPrefix(style, "Top")

		// Clean tags
		cleanText := assTagRegex.ReplaceAllString(rawText, "")
		textLines := strings.Split(cleanText, `\N`)
		var enLines []string
		var zhLines []string

		for _, tl := range textLines {
			tlTrimmed := strings.TrimSpace(tl)
			if tlTrimmed == "" {
				continue
			}
			if hasHanCharacters(tlTrimmed) {
				zhLines = append(zhLines, tlTrimmed)
			} else {
				enLines = append(enLines, tlTrimmed)
			}
		}

		dialogues = append(dialogues, ParsedASSDialogue{
			StartMS:   startMS,
			EndMS:     endMS,
			Style:     style,
			Alignment: align,
			Top:       top,
			RawText:   rawText,
			CleanText: cleanText,
			ENText:    strings.Join(enLines, "\n"),
			ZHText:    strings.Join(zhLines, "\n"),
		})
	}

	if len(dialogues) == 0 {
		return nil, fmt.Errorf("no Dialogue lines found in ASS content")
	}

	// Prepare cue maps
	enMap := make(map[int]subtitlellm.Cue)
	for _, c := range bundle.EN {
		enMap[c.ID] = c
	}
	zhMap := make(map[int]subtitlellm.Cue)
	for _, c := range bundle.ZH {
		zhMap[c.ID] = c
	}

	type groupMeta struct {
		Index      int
		ENIDs      []int
		ZHIDs      []int
		UnionStart int
		UnionEnd   int
		ENText     string
		ZHText     string
	}

	groups := make([]groupMeta, len(mapping.Groups))
	for i, grp := range mapping.Groups {
		uStart, uEnd := -1, -1
		var enTexts []string
		for _, id := range grp.ENIDs {
			if cue, ok := enMap[id]; ok {
				enTexts = append(enTexts, cue.Plain)
				if uStart == -1 || cue.Start < uStart {
					uStart = cue.Start
				}
				if cue.End > uEnd {
					uEnd = cue.End
				}
			}
		}
		var zhTexts []string
		for _, id := range grp.ZHIDs {
			if cue, ok := zhMap[id]; ok {
				zhTexts = append(zhTexts, cue.Plain)
				if uStart == -1 || cue.Start < uStart {
					uStart = cue.Start
				}
				if cue.End > uEnd {
					uEnd = cue.End
				}
			}
		}
		groups[i] = groupMeta{
			Index:      i,
			ENIDs:      grp.ENIDs,
			ZHIDs:      grp.ZHIDs,
			UnionStart: uStart,
			UnionEnd:   uEnd,
			ENText:     strings.Join(enTexts, " "),
			ZHText:     strings.Join(zhTexts, " "),
		}
	}

	// Map each dialogue to best group
	var events []RenderedEvent
	for _, d := range dialogues {
		bestGroup := -1
		bestScore := -1.0

		for _, g := range groups {
			// Compute time overlap
			overlapStart := math.Max(float64(d.StartMS), float64(g.UnionStart))
			overlapEnd := math.Min(float64(d.EndMS), float64(g.UnionEnd))
			overlap := overlapEnd - overlapStart
			if overlap < -1000 { // Allow 1s tolerance
				continue
			}

			score := overlap
			// Text matching bonus
			if d.ENText != "" && g.ENText != "" {
				if strings.Contains(g.ENText, d.ENText) || strings.Contains(d.ENText, g.ENText) {
					score += 5000.0
				}
			}
			if d.ZHText != "" && g.ZHText != "" {
				if strings.Contains(g.ZHText, d.ZHText) || strings.Contains(d.ZHText, g.ZHText) {
					score += 5000.0
				}
			}

			if score > bestScore {
				bestScore = score
				bestGroup = g.Index
			}
		}

		if bestGroup < 0 {
			// If no timing match, fallback to sequential nearest
			bestGroup = 0
		}

		events = append(events, RenderedEvent{
			GroupIndex: bestGroup,
			Strategy:   "ass_parsed",
			ENIDs:      groups[bestGroup].ENIDs,
			ZHIDs:      groups[bestGroup].ZHIDs,
			StartMS:    d.StartMS,
			EndMS:      d.EndMS,
			Alignment:  d.Alignment,
			Top:        d.Top,
			ENText:     d.ENText,
			ZHText:     d.ZHText,
			ENLines:    countEffectiveLines(d.ENText),
			ZHLines:    countEffectiveLines(d.ZHText),
		})
	}

	return events, nil
}

func equalIntSlices(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	sortedA := append([]int(nil), a...)
	sortedB := append([]int(nil), b...)
	sort.Ints(sortedA)
	sort.Ints(sortedB)
	for i := range sortedA {
		if sortedA[i] != sortedB[i] {
			return false
		}
	}
	return true
}

func InheritSemanticReviews(entries []LedgerEntry, bundle subtitlellm.Bundle, reviewPaths []string) (SemanticInheritanceStats, error) {
	stats := SemanticInheritanceStats{
		TotalEntries: len(entries),
	}

	bundleFP := ComputeBundleFingerprint(bundle)

	var allItems []SemanticReviewItem
	for _, p := range reviewPaths {
		raw, err := os.ReadFile(p)
		if err != nil {
			return stats, fmt.Errorf("read review file %s: %w", p, err)
		}
		var items []SemanticReviewItem
		if err := json.Unmarshal(raw, &items); err != nil {
			return stats, fmt.Errorf("unmarshal review file %s: %w", p, err)
		}
		allItems = append(allItems, items...)
	}

	for i := range entries {
		matched := false
		for _, item := range allItems {
			if item.Fingerprint != "" && item.Fingerprint != bundleFP {
				continue
			}
			if !equalIntSlices(entries[i].ENIDs, item.ENIDs) || !equalIntSlices(entries[i].ZHIDs, item.ZHIDs) {
				continue
			}
			if item.ENText != "" && strings.TrimSpace(entries[i].ENText) != strings.TrimSpace(item.ENText) {
				continue
			}
			if item.ZHText != "" && strings.TrimSpace(entries[i].ZHText) != strings.TrimSpace(item.ZHText) {
				continue
			}

			verdict := item.Verdict
			if verdict == "" {
				verdict = "reviewed"
			}
			note := item.Note
			if note == "" {
				note = item.Reason
			}
			entries[i].SemanticVerdict = verdict
			entries[i].SemanticNote = note
			matched = true
			if verdict == "acceptable" || verdict == "confirmed-compatible" || verdict == "legitimate-one-sided" {
				stats.InheritedCount++
			} else {
				stats.FlaggedCount++
			}
			break
		}

		if !matched {
			entries[i].SemanticVerdict = "unreviewed"
			stats.UnreviewedCount++
		}
	}

	return stats, nil
}

func VerifyActualASSAgainstExpected(actualASS, expectedASS []byte) ([]string, error) {
	if len(actualASS) == 0 {
		return nil, fmt.Errorf("actual ASS content is empty")
	}
	if len(expectedASS) == 0 {
		return nil, fmt.Errorf("expected ASS content is empty")
	}

	normActual := strings.ReplaceAll(string(actualASS), "\r\n", "\n")
	normExp := strings.ReplaceAll(string(expectedASS), "\r\n", "\n")

	var actualDialogues []string
	for _, l := range strings.Split(normActual, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "Dialogue:") {
			actualDialogues = append(actualDialogues, strings.TrimSpace(l))
		}
	}

	var expDialogues []string
	for _, l := range strings.Split(normExp, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "Dialogue:") {
			expDialogues = append(expDialogues, strings.TrimSpace(l))
		}
	}

	var failures []string
	if len(actualDialogues) != len(expDialogues) {
		failures = append(failures, fmt.Sprintf("dialogue count mismatch: actual %d vs expected %d", len(actualDialogues), len(expDialogues)))
		return failures, nil
	}

	for i := range actualDialogues {
		if actualDialogues[i] != expDialogues[i] {
			failures = append(failures, fmt.Sprintf("dialogue %d mismatch:\n  actual:   %s\n  expected: %s", i, actualDialogues[i], expDialogues[i]))
		}
	}

	return failures, nil
}
