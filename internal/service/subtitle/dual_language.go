package subtitle

import (
	"fmt"
	"strings"
	"unicode"
)

type lineLanguage int

const (
	langUnknown lineLanguage = iota
	langChinese
	langEnglish
	langMixed
)

func containsCJK(s string) bool {
	for _, r := range s {
		if isCJK(r) {
			return true
		}
	}
	return false
}

func containsLatin(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) && unicode.In(r, unicode.Latin) {
			return true
		}
	}
	return false
}

func isCJK(r rune) bool {
	return unicode.In(r, unicode.Han)
}

type srtCue struct {
	index     int
	startTime string
	endTime   string
	lines     []string
}

const dualLanguageThreshold = 0.30

func parseSRTCues(content string) ([]srtCue, error) {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	blocks := strings.Split(strings.TrimSpace(content), "\n\n")

	var cues []srtCue
	for i, block := range blocks {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		lines := strings.Split(block, "\n")
		if len(lines) < 2 {
			continue
		}

		timingIdx := -1
		for j, line := range lines {
			if strings.Contains(line, " --> ") {
				timingIdx = j
				break
			}
		}
		if timingIdx < 0 || timingIdx >= len(lines)-1 {
			continue
		}

		parts := strings.SplitN(lines[timingIdx], " --> ", 2)
		if len(parts) != 2 {
			continue
		}

		cues = append(cues, srtCue{
			index:     i + 1,
			startTime: strings.TrimSpace(parts[0]),
			endTime:   strings.TrimSpace(parts[1]),
			lines:     lines[timingIdx+1:],
		})
	}

	if len(cues) == 0 {
		return nil, fmt.Errorf("no valid SRT cues found")
	}
	return cues, nil
}

func normalizeCueLines(lines []string) []string {
	var result []string
	for _, line := range lines {
		parts := strings.Split(line, "\\N")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				result = append(result, p)
			}
		}
	}
	return result
}

func isCueBilingual(lines []string) bool {
	normalized := normalizeCueLines(lines)

	hasCJKLine := false
	hasLatinLine := false
	hasMixedLine := false

	for _, line := range normalized {
		switch classifyLine(line) {
		case langChinese:
			hasCJKLine = true
		case langEnglish:
			hasLatinLine = true
		case langMixed:
			hasMixedLine = true
		}
	}

	return (hasCJKLine && hasLatinLine) || hasMixedLine
}

func detectDualLanguageSRT(content string) bool {
	cues, err := parseSRTCues(content)
	if err != nil || len(cues) == 0 {
		return false
	}

	sampleSize := len(cues)
	if sampleSize > 30 {
		sampleSize = 30
	}

	bilingualCount := 0
	for _, cue := range cues[:sampleSize] {
		if isCueBilingual(cue.lines) {
			bilingualCount++
		}
	}

	ratio := float64(bilingualCount) / float64(sampleSize)
	return ratio >= dualLanguageThreshold
}

func classifyLine(line string) lineLanguage {
	hasCJK := containsCJK(line)
	hasLatin := containsLatin(line)

	switch {
	case hasCJK && hasLatin:
		return langMixed
	case hasCJK:
		return langChinese
	case hasLatin:
		return langEnglish
	default:
		return langUnknown
	}
}
