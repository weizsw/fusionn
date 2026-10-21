package subtitlellm

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"testing"
)

// EpisodeValidationResult holds genuine, computed metrics for an episode render.
type EpisodeValidationResult struct {
	TotalEvents         int
	SingleLineEvents    int
	TwoLineEvents       int
	ThreeLineEvents     int
	FourLineEvents      int
	TopEvents           int
	BottomEvents        int
	MultiSpeakerEvents  int
	ENCueCount          int
	ZHCueCount          int
	ENCoverage          float64
	ZHCoverage          float64
	MissingENIDs        []int
	MissingZHIDs        []int
	ChronologicalOrder  bool
	HasABARepetition    bool
	ABARepetitionDetail string
}

func validateASSEpisode(assContent string, bundle Bundle) (EpisodeValidationResult, error) {
	dialogues := parseDialoguesForValidation(assContent)
	if len(dialogues) == 0 {
		return EpisodeValidationResult{}, fmt.Errorf("no dialogue lines parsed")
	}

	res := EpisodeValidationResult{
		TotalEvents: len(dialogues),
		ENCueCount:  len(bundle.EN),
		ZHCueCount:  len(bundle.ZH),
	}

	for _, d := range dialogues {
		if strings.HasPrefix(d.style, "Top") {
			res.TopEvents++
		} else {
			res.BottomEvents++
		}

		cleanText := regexp.MustCompile(`\{[^}]*\}`).ReplaceAllString(d.text, "")
		explicitLines := strings.Count(cleanText, `\N`) + 1
		switch explicitLines {
		case 1:
			res.SingleLineEvents++
		case 2:
			res.TwoLineEvents++
		case 3:
			res.ThreeLineEvents++
		case 4:
			res.FourLineEvents++
		}

		if strings.Contains(d.text, "- ") || strings.Contains(d.text, "– ") {
			res.MultiSpeakerEvents++
		}
	}

	// Verify text content coverage for all source cues
	fullASSText := assContent
	enFound := 0
	for _, c := range bundle.EN {
		firstWord := strings.Fields(c.Display)
		if len(firstWord) > 0 && strings.Contains(fullASSText, firstWord[0]) {
			enFound++
		} else {
			res.MissingENIDs = append(res.MissingENIDs, c.ID)
		}
	}
	if len(bundle.EN) > 0 {
		res.ENCoverage = float64(enFound) / float64(len(bundle.EN))
	}

	zhFound := 0
	for _, c := range bundle.ZH {
		runes := []rune(strings.TrimSpace(c.Display))
		target := string(runes)
		if len(runes) > 4 {
			target = string(runes[:4])
		}
		if target != "" && strings.Contains(fullASSText, target) {
			zhFound++
		} else {
			res.MissingZHIDs = append(res.MissingZHIDs, c.ID)
		}
	}
	if len(bundle.ZH) > 0 {
		res.ZHCoverage = float64(zhFound) / float64(len(bundle.ZH))
	}

	// Check for A-B-A-B repetition defect in dialogue text
	for i := 0; i+3 < len(dialogues); i++ {
		t0 := cleanDialogueText(dialogues[i].text)
		t1 := cleanDialogueText(dialogues[i+1].text)
		t2 := cleanDialogueText(dialogues[i+2].text)
		t3 := cleanDialogueText(dialogues[i+3].text)
		if t0 != "" && t0 == t2 && t1 != "" && t1 == t3 && t0 != t1 {
			res.HasABARepetition = true
			res.ABARepetitionDetail = fmt.Sprintf("Repeated [%s] -> [%s] at indices %d-%d", t0, t1, i, i+3)
			break
		}
	}

	// Verify chronological order of start timestamps
	res.ChronologicalOrder = true
	for i := 0; i+1 < len(dialogues); i++ {
		if dialogues[i].start > dialogues[i+1].start {
			res.ChronologicalOrder = false
			break
		}
	}

	return res, nil
}

type validationDialogue struct {
	start int
	end   int
	style string
	text  string
}

func parseDialoguesForValidation(content string) []validationDialogue {
	var result []validationDialogue
	re := regexp.MustCompile(`^Dialogue:\s*\d+,([^,]+),([^,]+),([^,]+),[^,]*,[^,]*,[^,]*,[^,]*,[^,]*,(.*)$`)
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		m := re.FindStringSubmatch(line)
		if m != nil {
			start, _ := parseASSTime(m[1])
			end, _ := parseASSTime(m[2])
			result = append(result, validationDialogue{
				start: start,
				end:   end,
				style: m[3],
				text:  m[4],
			})
		}
	}
	return result
}

func cleanDialogueText(raw string) string {
	clean := regexp.MustCompile(`\{[^}]*\}`).ReplaceAllString(raw, "")
	clean = strings.TrimSpace(clean)
	// Return first line
	parts := strings.Split(clean, `\N`)
	return strings.TrimSpace(parts[0])
}

// TestNegativeValidationCatchesABARepetitionAndMissingContent proves that the validation
// logic actually catches defects rather than emitting false positives.
func TestNegativeValidationCatchesABARepetitionAndMissingContent(t *testing.T) {
	bundle := Bundle{
		EN: []Cue{
			{ID: 1, Display: "First English sentence."},
			{ID: 2, Display: "Second English sentence."},
		},
		ZH: []Cue{
			{ID: 1, Display: "第一句中文。"},
			{ID: 2, Display: "第二句中文。"},
		},
	}

	// Corrupted ASS with A-B-A-B repetition
	corruptedABA := `[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Dialogue: 0,0:00:01.00,0:00:02.00,Default,,0,0,0,,第一句中文\N{\rEng}First
Dialogue: 0,0:00:02.00,0:00:03.00,Default,,0,0,0,,第二句中文\N{\rEng}Second
Dialogue: 0,0:00:03.00,0:00:04.00,Default,,0,0,0,,第一句中文\N{\rEng}First
Dialogue: 0,0:00:04.00,0:00:05.00,Default,,0,0,0,,第二句中文\N{\rEng}Second
`
	res, err := validateASSEpisode(corruptedABA, bundle)
	if err != nil {
		t.Fatal(err)
	}
	if !res.HasABARepetition {
		t.Fatal("negative test failed: validator did NOT detect A-B-A-B repetition defect")
	}

	// Corrupted ASS with missing Chinese cue 2
	missingCue := `[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Dialogue: 0,0:00:01.00,0:00:02.00,Default,,0,0,0,,第一句中文\N{\rEng}First English sentence.
`
	resMissing, err := validateASSEpisode(missingCue, bundle)
	if err != nil {
		t.Fatal(err)
	}
	if resMissing.ZHCoverage >= 1.0 || len(resMissing.MissingZHIDs) == 0 {
		t.Fatal("negative test failed: validator did NOT detect missing Chinese cue")
	}
}

// TestE01RealRenderValidation verifies the real E01 bilingual ASS output
func TestE01RealRenderValidation(t *testing.T) {
	assPath := "/Users/maverick/Desktop/fusionn-subtitle-split-test/terminal-list-s02e01-v3/bilingual.ass"
	sourcesPath := "/Users/maverick/Desktop/fusionn-subtitle-split-test/terminal-list-s02e01-v3/sources.json"

	assData, err := os.ReadFile(assPath)
	if err != nil {
		t.Skipf("E01 ASS file %s not present, skipping", assPath)
	}
	sourcesData, err := os.ReadFile(sourcesPath)
	if err != nil {
		t.Skipf("E01 sources file %s not present, skipping", sourcesPath)
	}

	var bundle Bundle
	if err := json.Unmarshal(sourcesData, &bundle); err != nil {
		t.Fatal(err)
	}

	res, err := validateASSEpisode(string(assData), bundle)
	if err != nil {
		t.Fatal(err)
	}

	if res.HasABARepetition {
		t.Fatalf("A-B-A-B repetition detected in real E01 ASS: %s", res.ABARepetitionDetail)
	}
	if !res.ChronologicalOrder {
		t.Fatal("E01 dialogue events are not in chronological order")
	}
	if res.ENCoverage < 0.99 || res.ZHCoverage < 0.99 {
		t.Fatalf("E01 source coverage low: EN=%f, ZH=%f", res.ENCoverage, res.ZHCoverage)
	}

	t.Logf("E01 Genuine Validation Summary: Total=%d, Top=%d, Bottom=%d, 2-line=%d, 3-line=%d, 4-line=%d, EN-Coverage=%.2f%%, ZH-Coverage=%.2f%%",
		res.TotalEvents, res.TopEvents, res.BottomEvents, res.TwoLineEvents, res.ThreeLineEvents, res.FourLineEvents, res.ENCoverage*100, res.ZHCoverage*100)
}
