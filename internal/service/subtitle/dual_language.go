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

type assDialogue struct {
	layer   string
	start   string
	end     string
	style   string
	name    string
	marginL string
	marginR string
	marginV string
	effect  string
	text    string
}

func parseASSDialogues(content string) []assDialogue {
	var dialogues []assDialogue
	lines := strings.Split(content, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "Dialogue:") {
			continue
		}

		data := strings.TrimPrefix(line, "Dialogue:")
		data = strings.TrimSpace(data)

		parts := strings.SplitN(data, ",", 10)
		if len(parts) < 10 {
			continue
		}

		dialogues = append(dialogues, assDialogue{
			layer:   strings.TrimSpace(parts[0]),
			start:   strings.TrimSpace(parts[1]),
			end:     strings.TrimSpace(parts[2]),
			style:   strings.TrimSpace(parts[3]),
			name:    strings.TrimSpace(parts[4]),
			marginL: strings.TrimSpace(parts[5]),
			marginR: strings.TrimSpace(parts[6]),
			marginV: strings.TrimSpace(parts[7]),
			effect:  strings.TrimSpace(parts[8]),
			text:    parts[9],
		})
	}
	return dialogues
}

func detectDualLanguageASS(content string) bool {
	dialogues := parseASSDialogues(content)
	if len(dialogues) == 0 {
		return false
	}

	sampleSize := len(dialogues)
	if sampleSize > 60 {
		sampleSize = 60
	}

	// Pattern 1: multiple styles serving different languages
	styleCJKCount := make(map[string]int)
	styleLatinCount := make(map[string]int)

	for _, d := range dialogues[:sampleSize] {
		if containsCJK(d.text) {
			styleCJKCount[d.style]++
		}
		if containsLatin(d.text) && !containsCJK(d.text) {
			styleLatinCount[d.style]++
		}
	}

	if len(styleCJKCount)+len(styleLatinCount) >= 2 {
		hasCJKStyle := false
		hasLatinStyle := false
		allStyles := make(map[string]bool)
		for s := range styleCJKCount {
			allStyles[s] = true
		}
		for s := range styleLatinCount {
			allStyles[s] = true
		}
		for style := range allStyles {
			cjk := styleCJKCount[style]
			latin := styleLatinCount[style]
			if cjk > latin {
				hasCJKStyle = true
			} else if latin > cjk {
				hasLatinStyle = true
			}
		}
		if hasCJKStyle && hasLatinStyle {
			return true
		}
	}

	// Pattern 2: single style with \N-separated bilingual text
	bilingualCount := 0
	for _, d := range dialogues[:sampleSize] {
		if strings.Contains(d.text, "\\N") {
			parts := strings.Split(d.text, "\\N")
			hasCJK := false
			hasLatin := false
			for _, p := range parts {
				if containsCJK(p) {
					hasCJK = true
				}
				if containsLatin(p) {
					hasLatin = true
				}
			}
			if hasCJK && hasLatin {
				bilingualCount++
			}
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

func srtTimeToASS(srtTime string) string {
	t := strings.Replace(srtTime, ",", ".", 1)
	if len(t) > 0 && t[0] == '0' {
		t = t[1:]
	}
	if idx := strings.LastIndex(t, "."); idx >= 0 && len(t)-idx == 4 {
		t = t[:len(t)-1]
	}
	return t
}

func splitMixedLine(line string) (chinese, english string) {
	runes := []rune(line)
	lastCJKIdx := -1
	for i, r := range runes {
		if isCJK(r) {
			lastCJKIdx = i
		}
	}

	if lastCJKIdx < 0 || lastCJKIdx >= len(runes)-1 {
		if containsCJK(line) {
			return line, ""
		}
		return "", line
	}

	chinese = strings.TrimSpace(string(runes[:lastCJKIdx+1]))
	english = strings.TrimSpace(string(runes[lastCJKIdx+1:]))
	return chinese, english
}

func separateCueLanguages(lines []string) (chineseLines, englishLines []string) {
	normalized := normalizeCueLines(lines)

	for _, line := range normalized {
		lang := classifyLine(line)
		switch lang {
		case langChinese:
			chineseLines = append(chineseLines, line)
		case langEnglish:
			englishLines = append(englishLines, line)
		case langMixed:
			ch, en := splitMixedLine(line)
			if ch != "" {
				chineseLines = append(chineseLines, ch)
			}
			if en != "" {
				englishLines = append(englishLines, en)
			}
		}
	}
	return chineseLines, englishLines
}

func convertDualLanguageSRTToASS(content string) (string, error) {
	cues, err := parseSRTCues(content)
	if err != nil {
		return "", fmt.Errorf("parse SRT: %w", err)
	}

	var sb strings.Builder

	sb.WriteString("[Script Info]\n")
	sb.WriteString("ScriptType: v4.00+\n")
	sb.WriteString("WrapStyle: 0\n")
	sb.WriteString("ScaledBorderAndShadow: yes\n")
	sb.WriteString("PlayResX: 384\n")
	sb.WriteString("PlayResY: 288\n\n")

	sb.WriteString("[V4+ Styles]\n")
	sb.WriteString("Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding\n")
	sb.WriteString("Style: Default,Arial,20,&H00FFFFFF,&H0000ffff,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,1.0,0.0,2,10,10,10,1\n")
	sb.WriteString("Style: Default_1,Arial,14,&H00FFFFFF,&H0000ffff,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,1.0,0.0,2,10,10,10,1\n\n")

	sb.WriteString("[Events]\n")
	sb.WriteString("Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n")

	for _, cue := range cues {
		startASS := srtTimeToASS(cue.startTime)
		endASS := srtTimeToASS(cue.endTime)

		chineseLines, englishLines := separateCueLanguages(cue.lines)

		if len(chineseLines) > 0 {
			text := strings.Join(chineseLines, "\\N")
			sb.WriteString(fmt.Sprintf("Dialogue: 0,%s,%s,Default,,0,0,0,,%s\n", startASS, endASS, text))
		}
		if len(englishLines) > 0 {
			text := strings.Join(englishLines, "\\N")
			sb.WriteString(fmt.Sprintf("Dialogue: 0,%s,%s,Default_1,,0,0,0,,%s\n", startASS, endASS, text))
		}
	}

	return sb.String(), nil
}

func remapDualLanguageASS(content string) (string, error) {
	dialogues := parseASSDialogues(content)
	if len(dialogues) == 0 {
		return "", fmt.Errorf("no dialogues found in ASS")
	}

	// Determine which style is CJK-dominant vs Latin-dominant
	styleCJKCount := make(map[string]int)
	styleLatinCount := make(map[string]int)

	for _, d := range dialogues {
		if containsCJK(d.text) {
			styleCJKCount[d.style]++
		}
		if containsLatin(d.text) && !containsCJK(d.text) {
			styleLatinCount[d.style]++
		}
	}

	styleLanguage := make(map[string]lineLanguage)
	allStyles := make(map[string]bool)
	for s := range styleCJKCount {
		allStyles[s] = true
	}
	for s := range styleLatinCount {
		allStyles[s] = true
	}
	for style := range allStyles {
		if styleCJKCount[style] > styleLatinCount[style] {
			styleLanguage[style] = langChinese
		} else if styleLatinCount[style] > styleCJKCount[style] {
			styleLanguage[style] = langEnglish
		}
	}

	var sb strings.Builder

	sb.WriteString("[Script Info]\n")
	sb.WriteString("ScriptType: v4.00+\n")
	sb.WriteString("WrapStyle: 0\n")
	sb.WriteString("ScaledBorderAndShadow: yes\n")
	sb.WriteString("PlayResX: 384\n")
	sb.WriteString("PlayResY: 288\n\n")

	sb.WriteString("[V4+ Styles]\n")
	sb.WriteString("Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding\n")
	sb.WriteString("Style: Default,Arial,20,&H00FFFFFF,&H0000ffff,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,1.0,0.0,2,10,10,10,1\n")
	sb.WriteString("Style: Default_1,Arial,14,&H00FFFFFF,&H0000ffff,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,1.0,0.0,2,10,10,10,1\n\n")

	sb.WriteString("[Events]\n")
	sb.WriteString("Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text\n")

	for _, d := range dialogues {
		// Check for \N-separated bilingual text within a single dialogue
		if strings.Contains(d.text, "\\N") {
			parts := strings.Split(d.text, "\\N")
			var chParts, enParts []string
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if containsCJK(p) {
					chParts = append(chParts, p)
				} else if containsLatin(p) {
					enParts = append(enParts, p)
				}
			}
			if len(chParts) > 0 {
				sb.WriteString(fmt.Sprintf("Dialogue: %s,%s,%s,Default,%s,%s,%s,%s,%s,%s\n",
					d.layer, d.start, d.end, d.name, d.marginL, d.marginR, d.marginV, d.effect,
					strings.Join(chParts, "\\N")))
			}
			if len(enParts) > 0 {
				sb.WriteString(fmt.Sprintf("Dialogue: %s,%s,%s,Default_1,%s,%s,%s,%s,%s,%s\n",
					d.layer, d.start, d.end, d.name, d.marginL, d.marginR, d.marginV, d.effect,
					strings.Join(enParts, "\\N")))
			}
			continue
		}

		// Map style based on language analysis
		newStyle := "Default"
		if lang, ok := styleLanguage[d.style]; ok && lang == langEnglish {
			newStyle = "Default_1"
		} else if !ok {
			if containsLatin(d.text) && !containsCJK(d.text) {
				newStyle = "Default_1"
			}
		}

		sb.WriteString(fmt.Sprintf("Dialogue: %s,%s,%s,%s,%s,%s,%s,%s,%s,%s\n",
			d.layer, d.start, d.end, newStyle, d.name, d.marginL, d.marginR, d.marginV, d.effect, d.text))
	}

	return sb.String(), nil
}
