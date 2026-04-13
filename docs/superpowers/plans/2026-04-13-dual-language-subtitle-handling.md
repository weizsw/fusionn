# Dual-Language Subtitle Handling Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Detect dual-language subtitles downloaded by Bazarr and convert them to styled ASS files, skipping the DuoSubs merge.

**Architecture:** A new `DualLanguageProcessor` is added as the first processor in the merge pipeline. It reads the Bazarr-sourced subtitle, detects if it contains both Chinese and English text, and if so converts it to a properly styled ASS file with `Default` (Chinese) and `Default_1` (English) styles. This sets `MergedSubPath`, causing `MergerProcessor` to skip. The existing `StyleProcessor` then applies custom styling as normal.

**Tech Stack:** Go standard library (`unicode`, `strings`, `os`, `path/filepath`, `bufio`), existing `executor.ConvertOpenCC`, existing `config.OpenCCConfig`.

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/service/subtitle/dual_language.go` (new) | Language detection functions, SRT/ASS parsing, ASS generation — pure logic, no processor interface |
| `internal/service/subtitle/dual_language_test.go` (new) | Unit tests for detection and conversion functions |
| `internal/service/subtitle/processor_dual_language.go` (new) | `DualLanguageProcessor` struct implementing `Processor` interface — orchestrates detection + conversion |
| `internal/service/subtitle/processor_dual_language_test.go` (new) | Tests for processor `ShouldRun`/`Process` behavior |
| `internal/service/subtitle/processor_merger.go` (modify) | Add `MergedSubPath == ""` guard to `ShouldRun` |
| `internal/service/subtitle/processor_merger_test.go` (modify) | Add test case for MergedSubPath skip |
| `internal/service/subtitle/service.go` (modify) | Wire `DualLanguageProcessor` into merge pipeline |
| `internal/service/subtitle/pipeline.go` (modify) | Add `"DualLanguage"` to `processorEmojis` map |

---

### Task 1: Language Detection Utilities

**Files:**
- Create: `internal/service/subtitle/dual_language.go`
- Test: `internal/service/subtitle/dual_language_test.go`

- [ ] **Step 1: Write failing tests for character classification**

Create `internal/service/subtitle/dual_language_test.go`:

```go
package subtitle

import (
	"testing"
)

func TestContainsCJK(t *testing.T) {
	tests := []struct {
		name string
		text string
		want bool
	}{
		{"chinese characters", "你好世界", true},
		{"english only", "Hello World", false},
		{"mixed line", "你好 Hello", true},
		{"empty string", "", false},
		{"numbers only", "12345", false},
		{"cjk extension a", "㐀㐁", true},
		{"punctuation only", "...,,,!!!", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := containsCJK(tt.text); got != tt.want {
				t.Errorf("containsCJK(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}

func TestContainsLatin(t *testing.T) {
	tests := []struct {
		name string
		text string
		want bool
	}{
		{"english sentence", "Hello World", true},
		{"chinese only", "你好世界", false},
		{"mixed line", "你好 Hello", true},
		{"empty string", "", false},
		{"numbers only", "12345", false},
		{"single letter", "a", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := containsLatin(tt.text); got != tt.want {
				t.Errorf("containsLatin(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}

func TestClassifyLine(t *testing.T) {
	tests := []struct {
		name string
		line string
		want lineLanguage
	}{
		{"chinese", "你好世界", langChinese},
		{"english", "Hello World", langEnglish},
		{"mixed", "你好 Hello World", langMixed},
		{"empty", "", langUnknown},
		{"numbers", "12345", langUnknown},
		{"chinese with punctuation", "你好！世界。", langChinese},
		{"english with numbers", "Episode 5", langEnglish},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyLine(tt.line); got != tt.want {
				t.Errorf("classifyLine(%q) = %v, want %v", tt.line, got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/maverick/go/src/Github/fusionn && go test ./internal/service/subtitle/ -run "TestContainsCJK|TestContainsLatin|TestClassifyLine" -v`
Expected: FAIL — functions not defined

- [ ] **Step 3: Implement character classification functions**

Create `internal/service/subtitle/dual_language.go`:

```go
package subtitle

import (
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/maverick/go/src/Github/fusionn && go test ./internal/service/subtitle/ -run "TestContainsCJK|TestContainsLatin|TestClassifyLine" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd /Users/maverick/go/src/Github/fusionn
git add internal/service/subtitle/dual_language.go internal/service/subtitle/dual_language_test.go
git commit -m "feat: add language detection utilities for dual-language subtitles"
```

---

### Task 2: SRT Dual-Language Detection

**Files:**
- Modify: `internal/service/subtitle/dual_language.go`
- Test: `internal/service/subtitle/dual_language_test.go`

- [ ] **Step 1: Write failing tests for SRT cue parsing and detection**

Add to `internal/service/subtitle/dual_language_test.go`:

```go
func TestParseSRTCues(t *testing.T) {
	input := "1\n00:00:01,000 --> 00:00:03,000\n你好世界\nHello World\n\n2\n00:00:04,000 --> 00:00:06,000\n再见\nGoodbye\n\n"

	cues, err := parseSRTCues(input)
	if err != nil {
		t.Fatalf("parseSRTCues() error = %v", err)
	}
	if len(cues) != 2 {
		t.Fatalf("parseSRTCues() got %d cues, want 2", len(cues))
	}
	if cues[0].startTime != "00:00:01,000" || cues[0].endTime != "00:00:03,000" {
		t.Errorf("cue[0] times = %q-%q, want 00:00:01,000-00:00:03,000", cues[0].startTime, cues[0].endTime)
	}
	if len(cues[0].lines) != 2 {
		t.Errorf("cue[0] lines = %d, want 2", len(cues[0].lines))
	}
}

func TestDetectDualLanguageSRT(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{
			name: "dual language alternating lines",
			content: "1\n00:00:01,000 --> 00:00:03,000\n你好世界\nHello World\n\n" +
				"2\n00:00:04,000 --> 00:00:06,000\n再见\nGoodbye\n\n" +
				"3\n00:00:07,000 --> 00:00:09,000\n谢谢\nThank you\n\n",
			want: true,
		},
		{
			name: "dual language with backslash N",
			content: "1\n00:00:01,000 --> 00:00:03,000\n你好世界\\NHello World\n\n" +
				"2\n00:00:04,000 --> 00:00:06,000\n再见\\NGoodbye\n\n",
			want: true,
		},
		{
			name: "chinese only",
			content: "1\n00:00:01,000 --> 00:00:03,000\n你好世界\n\n" +
				"2\n00:00:04,000 --> 00:00:06,000\n再见\n\n",
			want: false,
		},
		{
			name: "english only",
			content: "1\n00:00:01,000 --> 00:00:03,000\nHello World\n\n" +
				"2\n00:00:04,000 --> 00:00:06,000\nGoodbye\n\n",
			want: false,
		},
		{
			name: "mostly dual with some chinese only",
			content: "1\n00:00:01,000 --> 00:00:03,000\n你好世界\nHello World\n\n" +
				"2\n00:00:04,000 --> 00:00:06,000\n再见\nGoodbye\n\n" +
				"3\n00:00:07,000 --> 00:00:09,000\n谢谢\n\n" +
				"4\n00:00:10,000 --> 00:00:12,000\n对不起\nSorry\n\n",
			want: true,
		},
		{
			name: "single line mixed characters",
			content: "1\n00:00:01,000 --> 00:00:03,000\n你好世界 Hello World\n\n" +
				"2\n00:00:04,000 --> 00:00:06,000\n再见 Goodbye\n\n",
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := detectDualLanguageSRT(tt.content); got != tt.want {
				t.Errorf("detectDualLanguageSRT() = %v, want %v", got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/maverick/go/src/Github/fusionn && go test ./internal/service/subtitle/ -run "TestParseSRTCues|TestDetectDualLanguageSRT" -v`
Expected: FAIL — functions not defined

- [ ] **Step 3: Implement SRT parsing and detection**

Add to `internal/service/subtitle/dual_language.go`:

```go
import (
	"fmt"
	"strings"
	"unicode"
)

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

		// Find the timing line (contains " --> ")
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

		textLines := lines[timingIdx+1:]

		cues = append(cues, srtCue{
			index:     i + 1,
			startTime: strings.TrimSpace(parts[0]),
			endTime:   strings.TrimSpace(parts[1]),
			lines:     textLines,
		})
	}

	if len(cues) == 0 {
		return nil, fmt.Errorf("no valid SRT cues found")
	}
	return cues, nil
}

// normalizeCueLines splits cue lines on literal "\N" (ASS-style line break found in some SRTs).
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/maverick/go/src/Github/fusionn && go test ./internal/service/subtitle/ -run "TestParseSRTCues|TestDetectDualLanguageSRT" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd /Users/maverick/go/src/Github/fusionn
git add internal/service/subtitle/dual_language.go internal/service/subtitle/dual_language_test.go
git commit -m "feat: add SRT dual-language detection with cue parsing"
```

---

### Task 3: ASS Dual-Language Detection

**Files:**
- Modify: `internal/service/subtitle/dual_language.go`
- Test: `internal/service/subtitle/dual_language_test.go`

- [ ] **Step 1: Write failing tests for ASS detection**

Add to `internal/service/subtitle/dual_language_test.go`:

```go
func TestDetectDualLanguageASS(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    bool
	}{
		{
			name: "dual language with two styles",
			content: `[Script Info]
ScriptType: v4.00+

[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour
Style: Chinese,Arial,20,&H00FFFFFF
Style: English,Arial,16,&H00FFFF00

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Dialogue: 0,0:00:01.00,0:00:03.00,Chinese,,0,0,0,,你好世界
Dialogue: 0,0:00:01.00,0:00:03.00,English,,0,0,0,,Hello World
Dialogue: 0,0:00:04.00,0:00:06.00,Chinese,,0,0,0,,再见
Dialogue: 0,0:00:04.00,0:00:06.00,English,,0,0,0,,Goodbye
`,
			want: true,
		},
		{
			name: "chinese only ASS",
			content: `[Script Info]
ScriptType: v4.00+

[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour
Style: Default,Arial,20,&H00FFFFFF

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Dialogue: 0,0:00:01.00,0:00:03.00,Default,,0,0,0,,你好世界
Dialogue: 0,0:00:04.00,0:00:06.00,Default,,0,0,0,,再见
`,
			want: false,
		},
		{
			name: "bilingual text in single style",
			content: `[Script Info]
ScriptType: v4.00+

[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour
Style: Default,Arial,20,&H00FFFFFF

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Dialogue: 0,0:00:01.00,0:00:03.00,Default,,0,0,0,,你好世界\NHello World
Dialogue: 0,0:00:04.00,0:00:06.00,Default,,0,0,0,,再见\NGoodbye
`,
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := detectDualLanguageASS(tt.content); got != tt.want {
				t.Errorf("detectDualLanguageASS() = %v, want %v", got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/maverick/go/src/Github/fusionn && go test ./internal/service/subtitle/ -run "TestDetectDualLanguageASS" -v`
Expected: FAIL — function not defined

- [ ] **Step 3: Implement ASS detection**

Add to `internal/service/subtitle/dual_language.go`:

```go
type assDialogue struct {
	layer  string
	start  string
	end    string
	style  string
	name   string
	marginL string
	marginR string
	marginV string
	effect string
	text   string
}

func parseASSDialogues(content string) []assDialogue {
	var dialogues []assDialogue
	lines := strings.Split(content, "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "Dialogue:") {
			continue
		}

		// Dialogue: Layer,Start,End,Style,Name,MarginL,MarginR,MarginV,Effect,Text
		data := strings.TrimPrefix(line, "Dialogue:")
		data = strings.TrimSpace(data)

		// Split into exactly 10 fields (Text may contain commas)
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

	// Check two patterns:
	// 1. Multiple styles where different styles serve different languages
	// 2. Single style with bilingual text (contains \N with mixed languages)

	// Pattern 1: group dialogues by style, check language per style
	styleLanguage := make(map[string]map[lineLanguage]int)
	for _, d := range dialogues[:sampleSize] {
		if _, ok := styleLanguage[d.style]; !ok {
			styleLanguage[d.style] = make(map[lineLanguage]int)
		}
		lang := classifyLine(d.text)
		styleLanguage[d.style][lang]++
	}

	if len(styleLanguage) >= 2 {
		hasCJKStyle := false
		hasLatinStyle := false
		for _, langs := range styleLanguage {
			cjkCount := langs[langChinese] + langs[langMixed]
			latinCount := langs[langEnglish]
			if cjkCount > latinCount {
				hasCJKStyle = true
			} else if latinCount > cjkCount {
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/maverick/go/src/Github/fusionn && go test ./internal/service/subtitle/ -run "TestDetectDualLanguageASS" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd /Users/maverick/go/src/Github/fusionn
git add internal/service/subtitle/dual_language.go internal/service/subtitle/dual_language_test.go
git commit -m "feat: add ASS dual-language detection"
```

---

### Task 4: SRT-to-ASS Conversion for Dual-Language Files

**Files:**
- Modify: `internal/service/subtitle/dual_language.go`
- Test: `internal/service/subtitle/dual_language_test.go`

- [ ] **Step 1: Write failing tests for SRT-to-ASS conversion**

Add to `internal/service/subtitle/dual_language_test.go`:

```go
func TestConvertDualLanguageSRTToASS(t *testing.T) {
	input := "1\n00:00:01,000 --> 00:00:03,000\n你好世界\nHello World\n\n" +
		"2\n00:00:04,500 --> 00:00:06,200\n再见\nGoodbye\n\n"

	result, err := convertDualLanguageSRTToASS(input)
	if err != nil {
		t.Fatalf("convertDualLanguageSRTToASS() error = %v", err)
	}

	if !strings.Contains(result, "[Script Info]") {
		t.Error("missing [Script Info] section")
	}
	if !strings.Contains(result, "[V4+ Styles]") {
		t.Error("missing [V4+ Styles] section")
	}
	if !strings.Contains(result, "Style: Default,") {
		t.Error("missing Default style")
	}
	if !strings.Contains(result, "Style: Default_1,") {
		t.Error("missing Default_1 style")
	}
	if !strings.Contains(result, "[Events]") {
		t.Error("missing [Events] section")
	}

	// Check that Chinese lines use Default style
	if !strings.Contains(result, "Default,,0,0,0,,你好世界") {
		t.Error("Chinese line not using Default style")
	}
	// Check that English lines use Default_1 style
	if !strings.Contains(result, "Default_1,,0,0,0,,Hello World") {
		t.Error("English line not using Default_1 style")
	}

	// Check time format conversion (SRT uses comma, ASS uses period)
	if !strings.Contains(result, "0:00:01.00") {
		t.Error("SRT time not converted to ASS format")
	}
}

func TestConvertDualLanguageSRTToASS_BackslashN(t *testing.T) {
	input := "1\n00:00:01,000 --> 00:00:03,000\n你好世界\\NHello World\n\n"

	result, err := convertDualLanguageSRTToASS(input)
	if err != nil {
		t.Fatalf("convertDualLanguageSRTToASS() error = %v", err)
	}

	if !strings.Contains(result, "Default,,0,0,0,,你好世界") {
		t.Error("Chinese part not extracted from \\N-separated line")
	}
	if !strings.Contains(result, "Default_1,,0,0,0,,Hello World") {
		t.Error("English part not extracted from \\N-separated line")
	}
}

func TestConvertDualLanguageSRTToASS_MixedLine(t *testing.T) {
	input := "1\n00:00:01,000 --> 00:00:03,000\n你好世界 Hello World\n\n"

	result, err := convertDualLanguageSRTToASS(input)
	if err != nil {
		t.Fatalf("convertDualLanguageSRTToASS() error = %v", err)
	}

	// Mixed lines should be split at CJK/Latin boundary
	if !strings.Contains(result, "Default,,0,0,0,,你好世界") {
		t.Error("Chinese part not extracted from mixed line")
	}
	if !strings.Contains(result, "Default_1,,0,0,0,,Hello World") {
		t.Error("English part not extracted from mixed line")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/maverick/go/src/Github/fusionn && go test ./internal/service/subtitle/ -run "TestConvertDualLanguageSRTToASS" -v`
Expected: FAIL — function not defined

- [ ] **Step 3: Implement SRT-to-ASS conversion**

Add to `internal/service/subtitle/dual_language.go`:

```go
func srtTimeToASS(srtTime string) string {
	// SRT: 00:00:01,000  →  ASS: 0:00:01.00
	t := strings.Replace(srtTime, ",", ".", 1)
	// Remove leading zero from hour and truncate milliseconds to centiseconds
	if len(t) > 0 && t[0] == '0' {
		t = t[1:]
	}
	// Truncate last digit of milliseconds (3 digits → 2)
	if idx := strings.LastIndex(t, "."); idx >= 0 && len(t)-idx == 4 {
		t = t[:len(t)-1]
	}
	return t
}

// splitMixedLine splits a line that contains both CJK and Latin text at the boundary.
func splitMixedLine(line string) (chinese, english string) {
	runes := []rune(line)
	// Find the transition point: last CJK character before Latin text starts
	lastCJKIdx := -1
	for i, r := range runes {
		if isCJK(r) {
			lastCJKIdx = i
		}
	}

	if lastCJKIdx < 0 || lastCJKIdx >= len(runes)-1 {
		// No clear boundary; classify whole line by dominant language
		if containsCJK(line) {
			return line, ""
		}
		return "", line
	}

	chinese = strings.TrimSpace(string(runes[:lastCJKIdx+1]))
	english = strings.TrimSpace(string(runes[lastCJKIdx+1:]))
	return chinese, english
}

// separateCueLanguages takes the lines of one cue and returns Chinese text and English text.
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

	// Script Info
	sb.WriteString("[Script Info]\n")
	sb.WriteString("ScriptType: v4.00+\n")
	sb.WriteString("WrapStyle: 0\n")
	sb.WriteString("ScaledBorderAndShadow: yes\n")
	sb.WriteString("PlayResX: 384\n")
	sb.WriteString("PlayResY: 288\n\n")

	// Styles (placeholder — StyleProcessor will overwrite these)
	sb.WriteString("[V4+ Styles]\n")
	sb.WriteString("Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding\n")
	sb.WriteString("Style: Default,Arial,20,&H00FFFFFF,&H0000ffff,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,1.0,0.0,2,10,10,10,1\n")
	sb.WriteString("Style: Default_1,Arial,14,&H00FFFFFF,&H0000ffff,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,1.0,0.0,2,10,10,10,1\n\n")

	// Events
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/maverick/go/src/Github/fusionn && go test ./internal/service/subtitle/ -run "TestConvertDualLanguageSRTToASS" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd /Users/maverick/go/src/Github/fusionn
git add internal/service/subtitle/dual_language.go internal/service/subtitle/dual_language_test.go
git commit -m "feat: add SRT-to-ASS conversion for dual-language subtitles"
```

---

### Task 5: ASS-to-ASS Re-mapping for Dual-Language Files

**Files:**
- Modify: `internal/service/subtitle/dual_language.go`
- Test: `internal/service/subtitle/dual_language_test.go`

- [ ] **Step 1: Write failing tests for ASS re-mapping**

Add to `internal/service/subtitle/dual_language_test.go`:

```go
func TestRemapDualLanguageASS(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantCH  string
		wantEN  string
		wantErr bool
	}{
		{
			name: "two styles remapped to Default and Default_1",
			input: `[Script Info]
ScriptType: v4.00+

[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding
Style: ChiStyle,Arial,20,&H00FFFFFF,&H0000ffff,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,1.0,0.0,2,10,10,10,1
Style: EngStyle,Arial,14,&H00FFFFFF,&H0000ffff,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,1.0,0.0,2,10,10,10,1

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Dialogue: 0,0:00:01.00,0:00:03.00,ChiStyle,,0,0,0,,你好世界
Dialogue: 0,0:00:01.00,0:00:03.00,EngStyle,,0,0,0,,Hello World
`,
			wantCH:  "Default,,0,0,0,,你好世界",
			wantEN:  "Default_1,,0,0,0,,Hello World",
			wantErr: false,
		},
		{
			name: "single style with backslash-N split into two events",
			input: `[Script Info]
ScriptType: v4.00+

[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding
Style: Default,Arial,20,&H00FFFFFF,&H0000ffff,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,1.0,0.0,2,10,10,10,1

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Dialogue: 0,0:00:01.00,0:00:03.00,Default,,0,0,0,,你好世界\NHello World
`,
			wantCH:  "Default,,0,0,0,,你好世界",
			wantEN:  "Default_1,,0,0,0,,Hello World",
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := remapDualLanguageASS(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("remapDualLanguageASS() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !strings.Contains(result, tt.wantCH) {
				t.Errorf("result missing Chinese line %q\ngot:\n%s", tt.wantCH, result)
			}
			if !strings.Contains(result, tt.wantEN) {
				t.Errorf("result missing English line %q\ngot:\n%s", tt.wantEN, result)
			}
			if !strings.Contains(result, "Style: Default,") {
				t.Error("result missing Default style definition")
			}
			if !strings.Contains(result, "Style: Default_1,") {
				t.Error("result missing Default_1 style definition")
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/maverick/go/src/Github/fusionn && go test ./internal/service/subtitle/ -run "TestRemapDualLanguageASS" -v`
Expected: FAIL — function not defined

- [ ] **Step 3: Implement ASS re-mapping**

Add to `internal/service/subtitle/dual_language.go`:

```go
func remapDualLanguageASS(content string) (string, error) {
	dialogues := parseASSDialogues(content)
	if len(dialogues) == 0 {
		return "", fmt.Errorf("no dialogues found in ASS")
	}

	// Determine style-to-language mapping
	styleLanguage := make(map[string]lineLanguage)
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

	for style := range styleCJKCount {
		if styleCJKCount[style] > styleLatinCount[style] {
			styleLanguage[style] = langChinese
		}
	}
	for style := range styleLatinCount {
		if styleLatinCount[style] > styleCJKCount[style] {
			styleLanguage[style] = langEnglish
		}
	}

	var sb strings.Builder

	// Script Info
	sb.WriteString("[Script Info]\n")
	sb.WriteString("ScriptType: v4.00+\n")
	sb.WriteString("WrapStyle: 0\n")
	sb.WriteString("ScaledBorderAndShadow: yes\n")
	sb.WriteString("PlayResX: 384\n")
	sb.WriteString("PlayResY: 288\n\n")

	// Styles (placeholder — StyleProcessor will overwrite)
	sb.WriteString("[V4+ Styles]\n")
	sb.WriteString("Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding\n")
	sb.WriteString("Style: Default,Arial,20,&H00FFFFFF,&H0000ffff,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,1.0,0.0,2,10,10,10,1\n")
	sb.WriteString("Style: Default_1,Arial,14,&H00FFFFFF,&H0000ffff,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,1.0,0.0,2,10,10,10,1\n\n")

	// Events
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
			// Unknown style — classify by content
			if containsLatin(d.text) && !containsCJK(d.text) {
				newStyle = "Default_1"
			}
		}

		sb.WriteString(fmt.Sprintf("Dialogue: %s,%s,%s,%s,%s,%s,%s,%s,%s,%s\n",
			d.layer, d.start, d.end, newStyle, d.name, d.marginL, d.marginR, d.marginV, d.effect, d.text))
	}

	return sb.String(), nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/maverick/go/src/Github/fusionn && go test ./internal/service/subtitle/ -run "TestRemapDualLanguageASS" -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
cd /Users/maverick/go/src/Github/fusionn
git add internal/service/subtitle/dual_language.go internal/service/subtitle/dual_language_test.go
git commit -m "feat: add ASS-to-ASS re-mapping for dual-language subtitles"
```

---

### Task 6: DualLanguageProcessor (Processor Interface)

**Files:**
- Create: `internal/service/subtitle/processor_dual_language.go`
- Test: `internal/service/subtitle/processor_dual_language_test.go`

- [ ] **Step 1: Write failing tests for ShouldRun**

Create `internal/service/subtitle/processor_dual_language_test.go`:

```go
package subtitle

import (
	"testing"

	"github.com/fusionn/internal/config"
)

func TestDualLanguageProcessor_Name(t *testing.T) {
	p := NewDualLanguageProcessor(config.OpenCCConfig{})
	if got := p.Name(); got != "DualLanguage" {
		t.Errorf("Name() = %v, want DualLanguage", got)
	}
}

func TestDualLanguageProcessor_ShouldRun(t *testing.T) {
	tests := []struct {
		name string
		pctx *ProcessingContext
		want bool
	}{
		{
			name: "bazarr source with chinese sub",
			pctx: &ProcessingContext{
				ChineseSubSource: ChineseSourceBazarr,
				ChineseSubPath:   "/path/to/sub.srt",
			},
			want: true,
		},
		{
			name: "non-bazarr source",
			pctx: &ProcessingContext{
				ChineseSubSource: ChineseSourceExtracted,
				ChineseSubPath:   "/path/to/sub.srt",
			},
			want: false,
		},
		{
			name: "translated source",
			pctx: &ProcessingContext{
				ChineseSubSource: ChineseSourceTranslated,
				ChineseSubPath:   "/path/to/sub.srt",
			},
			want: false,
		},
		{
			name: "bazarr but no chinese path",
			pctx: &ProcessingContext{
				ChineseSubSource: ChineseSourceBazarr,
				ChineseSubPath:   "",
			},
			want: false,
		},
		{
			name: "bazarr but merged already set",
			pctx: &ProcessingContext{
				ChineseSubSource: ChineseSourceBazarr,
				ChineseSubPath:   "/path/to/sub.srt",
				MergedSubPath:    "/path/to/merged.ass",
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewDualLanguageProcessor(config.OpenCCConfig{})
			if got := p.ShouldRun(tt.pctx); got != tt.want {
				t.Errorf("ShouldRun() = %v, want %v", got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /Users/maverick/go/src/Github/fusionn && go test ./internal/service/subtitle/ -run "TestDualLanguageProcessor" -v`
Expected: FAIL — type not defined

- [ ] **Step 3: Implement DualLanguageProcessor struct and ShouldRun**

Create `internal/service/subtitle/processor_dual_language.go`:

```go
package subtitle

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"github.com/fusionn/internal/config"
	"github.com/fusionn/internal/executor"
	"github.com/fusionn/pkg/logger"
)

type DualLanguageProcessor struct {
	openccConfig config.OpenCCConfig
}

func NewDualLanguageProcessor(openccCfg config.OpenCCConfig) *DualLanguageProcessor {
	return &DualLanguageProcessor{
		openccConfig: openccCfg,
	}
}

func (p *DualLanguageProcessor) Name() string {
	return "DualLanguage"
}

func (p *DualLanguageProcessor) ShouldRun(pctx *ProcessingContext) bool {
	return pctx.ChineseSubSource == ChineseSourceBazarr &&
		pctx.ChineseSubPath != "" &&
		pctx.MergedSubPath == ""
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/maverick/go/src/Github/fusionn && go test ./internal/service/subtitle/ -run "TestDualLanguageProcessor" -v`
Expected: PASS

- [ ] **Step 5: Implement Process method**

Add the `Process` method to `internal/service/subtitle/processor_dual_language.go`:

```go
func (p *DualLanguageProcessor) Process(ctx context.Context, pctx *ProcessingContext) error {
	log := logger.Indent()

	content, err := os.ReadFile(pctx.ChineseSubPath)
	if err != nil {
		return fmt.Errorf("read subtitle file: %w", err)
	}

	text := string(content)
	ext := strings.ToLower(filepath.Ext(pctx.ChineseSubPath))

	var isDual bool
	switch ext {
	case ".srt":
		isDual = detectDualLanguageSRT(text)
	case ".ass", ".ssa":
		isDual = detectDualLanguageASS(text)
	default:
		log.Debugf("Unsupported subtitle format %q for dual-language detection, skipping", ext)
		return nil
	}

	if !isDual {
		log.Info("Subtitle is not dual-language, proceeding with normal merge")
		return nil
	}

	log.Info("Dual-language subtitle detected — converting to styled ASS")

	// Convert to ASS with Default/Default_1 styles
	var assContent string
	switch ext {
	case ".srt":
		assContent, err = convertDualLanguageSRTToASS(text)
	case ".ass", ".ssa":
		assContent, err = remapDualLanguageASS(text)
	}
	if err != nil {
		return fmt.Errorf("convert dual-language subtitle: %w", err)
	}

	// Run OpenCC if enabled
	if p.openccConfig.Enabled {
		assContent, err = p.convertChineseInASS(ctx, assContent, log)
		if err != nil {
			log.Warnf("OpenCC conversion failed, using original text: %v", err)
		}
	}

	// Write to temp file
	videoDir := filepath.Dir(pctx.VideoPath)
	outputDir := filepath.Join(videoDir, fmt.Sprintf(".fusionn-dual-%s", uuid.New().String()))
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	pctx.TempMergeDir = outputDir

	outputPath := filepath.Join(outputDir, "dual_combined.ass")
	if err := os.WriteFile(outputPath, []byte(assContent), 0644); err != nil {
		return fmt.Errorf("write ASS file: %w", err)
	}

	pctx.MergedSubPath = outputPath
	log.Infof("Dual-language ASS written: %s", outputPath)
	return nil
}

// convertChineseInASS extracts Chinese dialogue text, runs OpenCC, and replaces it back.
func (p *DualLanguageProcessor) convertChineseInASS(ctx context.Context, assContent string, log logger.IndentedLogger) (string, error) {
	log.Info("Running OpenCC on Chinese text in dual-language ASS")

	// Extract Chinese dialogue lines (Default style)
	lines := strings.Split(assContent, "\n")
	var chineseTexts []string
	var chineseIndices []int

	for i, line := range lines {
		if strings.HasPrefix(line, "Dialogue:") && strings.Contains(line, ",Default,") && !strings.Contains(line, ",Default_1,") {
			parts := strings.SplitN(line, ",", 10)
			if len(parts) >= 10 {
				chineseTexts = append(chineseTexts, parts[9])
				chineseIndices = append(chineseIndices, i)
			}
		}
	}

	if len(chineseTexts) == 0 {
		return assContent, nil
	}

	// Write Chinese text to temp file (one line per dialogue)
	tmpDir := os.TempDir()
	inputPath := filepath.Join(tmpDir, fmt.Sprintf("fusionn-opencc-input-%s.txt", uuid.New().String()))
	outputPath := filepath.Join(tmpDir, fmt.Sprintf("fusionn-opencc-output-%s.txt", uuid.New().String()))
	defer os.Remove(inputPath)
	defer os.Remove(outputPath)

	if err := os.WriteFile(inputPath, []byte(strings.Join(chineseTexts, "\n")), 0644); err != nil {
		return "", fmt.Errorf("write opencc input: %w", err)
	}

	configName := p.openccConfig.Config
	if configName == "" {
		configName = "t2s.json"
	}

	if err := executor.ConvertOpenCC(ctx, inputPath, outputPath, configName); err != nil {
		return "", fmt.Errorf("opencc conversion: %w", err)
	}

	convertedBytes, err := os.ReadFile(outputPath)
	if err != nil {
		return "", fmt.Errorf("read opencc output: %w", err)
	}

	convertedTexts := strings.Split(string(convertedBytes), "\n")
	if len(convertedTexts) != len(chineseTexts) {
		log.Warnf("OpenCC output line count mismatch: got %d, expected %d — skipping replacement",
			len(convertedTexts), len(chineseTexts))
		return assContent, nil
	}

	// Replace Chinese text back into the ASS lines
	for j, idx := range chineseIndices {
		parts := strings.SplitN(lines[idx], ",", 10)
		if len(parts) >= 10 {
			parts[9] = convertedTexts[j]
			lines[idx] = strings.Join(parts, ",")
		}
	}

	return strings.Join(lines, "\n"), nil
}
```

- [ ] **Step 6: Run all dual-language tests**

Run: `cd /Users/maverick/go/src/Github/fusionn && go test ./internal/service/subtitle/ -run "TestDualLanguageProcessor|TestConvert|TestDetect|TestClassify|TestContains|TestParseSRT|TestRemapDual" -v`
Expected: PASS

- [ ] **Step 7: Commit**

```bash
cd /Users/maverick/go/src/Github/fusionn
git add internal/service/subtitle/processor_dual_language.go internal/service/subtitle/processor_dual_language_test.go
git commit -m "feat: add DualLanguageProcessor with detection, conversion, and OpenCC support"
```

---

### Task 7: Wire Into Pipeline and Update MergerProcessor Guard

**Files:**
- Modify: `internal/service/subtitle/processor_merger.go`
- Modify: `internal/service/subtitle/processor_merger_test.go`
- Modify: `internal/service/subtitle/service.go`
- Modify: `internal/service/subtitle/pipeline.go`

- [ ] **Step 1: Write failing test for MergerProcessor skip when MergedSubPath is set**

Add to `internal/service/subtitle/processor_merger_test.go`:

```go
{
    name: "skip when merged path already set",
    pctx: &ProcessingContext{
        EnglishSubPath: "/path/to/english.srt",
        ChineseSubPath: "/path/to/chinese.srt",
        MergedSubPath:  "/path/to/already_merged.ass",
    },
    want: false,
},
```

This test case goes inside the existing `TestMergerProcessor_ShouldRun` table.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/maverick/go/src/Github/fusionn && go test ./internal/service/subtitle/ -run "TestMergerProcessor_ShouldRun" -v`
Expected: FAIL — the "skip when merged path already set" case returns true, want false

- [ ] **Step 3: Update MergerProcessor.ShouldRun**

In `internal/service/subtitle/processor_merger.go`, change:

```go
func (p *MergerProcessor) ShouldRun(pctx *ProcessingContext) bool {
	return pctx.EnglishSubPath != "" && pctx.ChineseSubPath != ""
}
```

to:

```go
func (p *MergerProcessor) ShouldRun(pctx *ProcessingContext) bool {
	return pctx.MergedSubPath == "" && pctx.EnglishSubPath != "" && pctx.ChineseSubPath != ""
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/maverick/go/src/Github/fusionn && go test ./internal/service/subtitle/ -run "TestMergerProcessor_ShouldRun" -v`
Expected: PASS

- [ ] **Step 5: Add DualLanguage to processorEmojis in pipeline.go**

In `internal/service/subtitle/pipeline.go`, add to the `processorEmojis` map:

```go
"DualLanguage": "🔀",
```

(Place after the `"BazarrSearch"` entry.)

- [ ] **Step 6: Wire DualLanguageProcessor into merge pipeline in service.go**

In `internal/service/subtitle/service.go`, in `NewService`, add the DualLanguageProcessor as the first processor in the merge pipeline. Change:

```go
mergePipeline := NewPipeline()
mergePipeline.AddProcessor(NewConversionProcessor(cfg.Subtitle.OpenCC))
```

to:

```go
mergePipeline := NewPipeline()
mergePipeline.AddProcessor(NewDualLanguageProcessor(cfg.Subtitle.OpenCC))
mergePipeline.AddProcessor(NewConversionProcessor(cfg.Subtitle.OpenCC))
```

- [ ] **Step 7: Run full test suite**

Run: `cd /Users/maverick/go/src/Github/fusionn && go test ./internal/service/subtitle/ -v`
Expected: All tests PASS

- [ ] **Step 8: Commit**

```bash
cd /Users/maverick/go/src/Github/fusionn
git add internal/service/subtitle/processor_merger.go internal/service/subtitle/processor_merger_test.go internal/service/subtitle/service.go internal/service/subtitle/pipeline.go
git commit -m "feat: wire DualLanguageProcessor into merge pipeline, add MergerProcessor skip guard"
```

---

### Task 8: Integration Test with File I/O

**Files:**
- Modify: `internal/service/subtitle/processor_dual_language_test.go`

- [ ] **Step 1: Write integration test for Process with SRT file on disk**

Add to `internal/service/subtitle/processor_dual_language_test.go`:

```go
import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fusionn/internal/config"
	"github.com/fusionn/pkg/logger"
)

func init() {
	logger.Init(true)
}

func TestDualLanguageProcessor_Process_DualSRT(t *testing.T) {
	tempDir := t.TempDir()

	srtContent := "1\n00:00:01,000 --> 00:00:03,000\n你好世界\nHello World\n\n" +
		"2\n00:00:04,000 --> 00:00:06,000\n再见\nGoodbye\n\n" +
		"3\n00:00:07,000 --> 00:00:09,000\n谢谢\nThank you\n\n"

	subPath := filepath.Join(tempDir, "test.zh.srt")
	if err := os.WriteFile(subPath, []byte(srtContent), 0644); err != nil {
		t.Fatal(err)
	}

	videoPath := filepath.Join(tempDir, "video.mkv")

	pctx := &ProcessingContext{
		VideoPath:        videoPath,
		ChineseSubPath:   subPath,
		ChineseSubSource: ChineseSourceBazarr,
		Metadata:         make(map[string]interface{}),
	}

	p := NewDualLanguageProcessor(config.OpenCCConfig{Enabled: false})
	err := p.Process(context.Background(), pctx)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	if pctx.MergedSubPath == "" {
		t.Fatal("Process() did not set MergedSubPath")
	}

	content, err := os.ReadFile(pctx.MergedSubPath)
	if err != nil {
		t.Fatalf("Failed to read merged file: %v", err)
	}

	result := string(content)
	if !strings.Contains(result, "Style: Default,") {
		t.Error("missing Default style")
	}
	if !strings.Contains(result, "Style: Default_1,") {
		t.Error("missing Default_1 style")
	}
	if !strings.Contains(result, "Default,,0,0,0,,你好世界") {
		t.Error("missing Chinese dialogue")
	}
	if !strings.Contains(result, "Default_1,,0,0,0,,Hello World") {
		t.Error("missing English dialogue")
	}

	// Cleanup
	if pctx.TempMergeDir != "" {
		os.RemoveAll(pctx.TempMergeDir)
	}
}

func TestDualLanguageProcessor_Process_NotDual(t *testing.T) {
	tempDir := t.TempDir()

	srtContent := "1\n00:00:01,000 --> 00:00:03,000\n你好世界\n\n" +
		"2\n00:00:04,000 --> 00:00:06,000\n再见\n\n"

	subPath := filepath.Join(tempDir, "test.zh.srt")
	if err := os.WriteFile(subPath, []byte(srtContent), 0644); err != nil {
		t.Fatal(err)
	}

	videoPath := filepath.Join(tempDir, "video.mkv")

	pctx := &ProcessingContext{
		VideoPath:        videoPath,
		ChineseSubPath:   subPath,
		ChineseSubSource: ChineseSourceBazarr,
		Metadata:         make(map[string]interface{}),
	}

	p := NewDualLanguageProcessor(config.OpenCCConfig{Enabled: false})
	err := p.Process(context.Background(), pctx)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	if pctx.MergedSubPath != "" {
		t.Errorf("Process() should not set MergedSubPath for non-dual subtitle, got %q", pctx.MergedSubPath)
	}
}
```

- [ ] **Step 2: Run integration tests**

Run: `cd /Users/maverick/go/src/Github/fusionn && go test ./internal/service/subtitle/ -run "TestDualLanguageProcessor_Process" -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
cd /Users/maverick/go/src/Github/fusionn
git add internal/service/subtitle/processor_dual_language_test.go
git commit -m "test: add integration tests for DualLanguageProcessor with file I/O"
```

---

### Task 9: Run Full Test Suite and Verify

- [ ] **Step 1: Run all subtitle package tests**

Run: `cd /Users/maverick/go/src/Github/fusionn && go test ./internal/service/subtitle/ -v -count=1`
Expected: All tests PASS

- [ ] **Step 2: Run project-wide tests**

Run: `cd /Users/maverick/go/src/Github/fusionn && go test ./... -count=1`
Expected: All tests PASS (or only pre-existing failures)

- [ ] **Step 3: Run go vet**

Run: `cd /Users/maverick/go/src/Github/fusionn && go vet ./internal/service/subtitle/...`
Expected: No errors

- [ ] **Step 4: Build to verify compilation**

Run: `cd /Users/maverick/go/src/Github/fusionn && go build ./...`
Expected: Success
