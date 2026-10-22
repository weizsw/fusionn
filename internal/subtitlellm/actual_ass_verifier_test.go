package subtitlellm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fusionn/internal/config"
)

// Gap 1: Test verification of ASS headers and Style definitions using public Render.
func TestVerifyDeliveredASSHeader_Gap1(t *testing.T) {
	style := fixtureStyle()
	cfg := fixtureConfig()
	bundle := fixtureBundle(
		[]Cue{{ID: 1, Start: 1000, End: 3000, Display: "Hello world", Plain: "Hello world"}},
		[]Cue{{ID: 1, Start: 1000, End: 3000, Display: "你好世界"}},
	)
	mapping := fixtureMapping(bundle, []Group{{ENIDs: []int{1}, ZHIDs: []int{1}, Layer: LayerBottom, Origin: OriginModel}})

	renderedBytes, err := Render(bundle, mapping, cfg, style, 1920, 1080)
	if err != nil {
		t.Fatalf("render valid reference ASS: %v", err)
	}
	validASS := string(renderedBytes)

	t.Run("Valid LF header passes", func(t *testing.T) {
		failures, err := VerifyDeliveredASSHeader(validASS, validASS)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(failures) != 0 {
			t.Fatalf("expected 0 failures, got: %v", failures)
		}
	})

	t.Run("Valid CRLF header passes against LF reference", func(t *testing.T) {
		crlfASS := strings.ReplaceAll(validASS, "\n", "\r\n")
		failures, err := VerifyDeliveredASSHeader(crlfASS, validASS)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(failures) != 0 {
			t.Fatalf("expected 0 failures for CRLF, got: %v", failures)
		}
	})

	t.Run("LF delivered passes against CRLF reference", func(t *testing.T) {
		crlfRef := strings.ReplaceAll(validASS, "\n", "\r\n")
		failures, err := VerifyDeliveredASSHeader(validASS, crlfRef)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(failures) != 0 {
			t.Fatalf("expected 0 failures, got: %v", failures)
		}
	})

	t.Run("PlayRes mismatch detected", func(t *testing.T) {
		tampered := strings.Replace(validASS, "PlayResX: 1920", "PlayResX: 1280", 1)
		failures, err := VerifyDeliveredASSHeader(tampered, validASS)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(failures) == 0 {
			t.Fatal("expected failure for PlayResX tampering, got 0")
		}
		found := false
		for _, f := range failures {
			if strings.Contains(f, "PlayResX") {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected failure mentioning PlayResX, got: %v", failures)
		}
	})

	t.Run("Default alignment mismatch detected", func(t *testing.T) {
		// Style: Default,...,Alignment=2,... tampered to Alignment=8
		tampered := strings.Replace(validASS, ",2,80,80,32,1", ",8,80,80,32,1", 1)
		failures, err := VerifyDeliveredASSHeader(tampered, validASS)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(failures) == 0 {
			t.Fatal("expected failure for Default style alignment tampering, got 0")
		}
		found := false
		for _, f := range failures {
			if strings.Contains(f, "Default") {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected failure mentioning Default style, got: %v", failures)
		}
	})

	t.Run("Eng font mismatch detected", func(t *testing.T) {
		// Style: Eng,Go,... tampered to Style: Eng,Comic Sans,...
		tampered := strings.Replace(validASS, "Style: Eng,Go,", "Style: Eng,Comic Sans,", 1)
		failures, err := VerifyDeliveredASSHeader(tampered, validASS)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(failures) == 0 {
			t.Fatal("expected failure for Eng font tampering, got 0")
		}
		found := false
		for _, f := range failures {
			if strings.Contains(f, "Comic Sans") || strings.Contains(f, "Eng") {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected failure mentioning Eng font, got: %v", failures)
		}
	})

	t.Run("Events Format line mismatch detected", func(t *testing.T) {
		tampered := strings.Replace(validASS, "Format: Layer, Start, End, Style", "Format: Layer, Start, End, CustomStyle", 1)
		failures, err := VerifyDeliveredASSHeader(tampered, validASS)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(failures) == 0 {
			t.Fatal("expected failure for Events Format line tampering, got 0")
		}
		found := false
		for _, f := range failures {
			if strings.Contains(f, "Format:") {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected failure mentioning Format line, got: %v", failures)
		}
	})
}

// Gap 2: Test rejection of non-finite floating point coordinates in \pos override tags.
func TestParseASSOverrides_RejectNonFinite_Gap2(t *testing.T) {
	t.Run("Valid coordinates pass", func(t *testing.T) {
		hasPos, x, y, _, _, err := parseASSOverrides(1, `{\pos(960, 540)}Hello`)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !hasPos || x != 960 || y != 540 {
			t.Fatalf("expected pos (960, 540), got (%v, %v)", x, y)
		}
	})

	t.Run("NaN coordinates rejected", func(t *testing.T) {
		_, _, _, _, _, err := parseASSOverrides(1, `{\pos(NaN, 540)}Hello`)
		if err == nil {
			t.Fatal("expected error for NaN X coordinate, got nil")
		}
		if !strings.Contains(err.Error(), "non-finite") {
			t.Fatalf("expected error mentioning non-finite, got: %v", err)
		}

		_, _, _, _, _, err = parseASSOverrides(2, `{\pos(960, NaN)}Hello`)
		if err == nil {
			t.Fatal("expected error for NaN Y coordinate, got nil")
		}
	})

	t.Run("Inf coordinates rejected", func(t *testing.T) {
		for _, tag := range []string{
			`{\pos(+Inf, 540)}`,
			`{\pos(-Inf, 540)}`,
			`{\pos(Inf, 540)}`,
			`{\pos(960, +Inf)}`,
			`{\pos(960, -Inf)}`,
			`{\pos(Infinity, 540)}`,
			`{\pos(-Infinity, 540)}`,
			`{\pos(960, Infinity)}`,
		} {
			_, _, _, _, _, err := parseASSOverrides(1, tag+"Hello")
			if err == nil {
				t.Fatalf("expected error for non-finite tag %s, got nil", tag)
			}
			if !strings.Contains(err.Error(), "non-finite") {
				t.Fatalf("expected non-finite error for %s, got: %v", tag, err)
			}
		}
	})

	t.Run("ParseSerializedASSWithPositions fails on non-finite coordinates", func(t *testing.T) {
		invalidASS := `[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Dialogue: 0,0:00:01.00,0:00:03.00,Default,,0,0,0,,{\pos(NaN,100)}Broken line
`
		_, err := ParseSerializedASSWithPositions(invalidASS)
		if err == nil {
			t.Fatal("expected ParseSerializedASSWithPositions to fail on NaN pos coordinate")
		}
		if !strings.Contains(err.Error(), "non-finite") {
			t.Fatalf("expected non-finite error, got: %v", err)
		}
	})
}

// Gap 3: Test delivery verification against expected ASS using actual word timing splits.
func TestVerifyDeliveredASSAgainstExpected_WordTimingEvidence_Gap3(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := renderTestConfig()
	cfg.EnChars = 22 // forces long cue splitting
	style := fixtureStyle()

	bundle := makeBundle(
		[]Cue{{
			ID:      1,
			Start:   1000,
			End:     7000,
			Display: "We should leave now and return tomorrow.",
			Plain:   "We should leave now\nand return tomorrow.",
		}},
		[]Cue{{
			ID:      1,
			Start:   1000,
			End:     7000,
			Display: "我们该走了，明天再来",
		}},
	)
	mapping := makeMapping(bundle, []Group{{
		ENIDs:  []int{1},
		ZHIDs:  []int{1},
		Layer:  LayerBottom,
		Origin: OriginModel,
	}})

	// Word timings that place the break between "now" (2400ms) and "and" (2800ms) -> split at 2600ms
	words := WordTimings{1: {
		{Text: "We", StartMS: 1000, EndMS: 1200},
		{Text: "should", StartMS: 1250, EndMS: 1500},
		{Text: "leave", StartMS: 1550, EndMS: 1900},
		{Text: "now", StartMS: 2000, EndMS: 2400},
		{Text: "and", StartMS: 2800, EndMS: 3000},
		{Text: "return", StartMS: 3100, EndMS: 3500},
		{Text: "tomorrow", StartMS: 3600, EndMS: 4300},
	}}

	// 1. Generate actual ASS using public producer RenderWithWordTimings
	actualBytes, err := RenderWithWordTimings(bundle, mapping, cfg, style, 1920, 1080, words)
	if err != nil {
		t.Fatalf("RenderWithWordTimings actual: %v", err)
	}

	// 2. Generate expected baseline with identical word timings
	expectedWithWordsBytes, err := RenderWithWordTimings(bundle, mapping, cfg, style, 1920, 1080, words)
	if err != nil {
		t.Fatalf("RenderWithWordTimings expectedWithWords: %v", err)
	}

	// 3. Generate expected baseline without word timings (falls back to proportional split)
	expectedWithoutWordsBytes, err := Render(bundle, mapping, cfg, style, 1920, 1080)
	if err != nil {
		t.Fatalf("Render expectedWithoutWords: %v", err)
	}

	// 4. Parse dialogues to verify that word timings genuinely change event slice timestamps
	actualDialogues, err := ParseSerializedASSWithPositions(string(actualBytes))
	if err != nil {
		t.Fatalf("parse actual dialogues: %v", err)
	}
	withoutWordsDialogues, err := ParseSerializedASSWithPositions(string(expectedWithoutWordsBytes))
	if err != nil {
		t.Fatalf("parse without words dialogues: %v", err)
	}

	if len(actualDialogues) < 2 || len(withoutWordsDialogues) < 2 {
		t.Fatalf("expected both to produce at least 2 split events, got %d and %d",
			len(actualDialogues), len(withoutWordsDialogues))
	}

	// Assert that word timing split boundary explicitly equals 2600ms and differs from proportional split (3920ms)
	if actualDialogues[0].End != 2600 {
		t.Fatalf("expected word-timed split end to explicitly equal 2600 ms, got %d ms", actualDialogues[0].End)
	}
	if withoutWordsDialogues[0].End != 3920 {
		t.Fatalf("expected proportional split end to explicitly equal 3920 ms, got %d ms", withoutWordsDialogues[0].End)
	}
	if actualDialogues[0].End == withoutWordsDialogues[0].End {
		t.Fatalf("precondition failed: expected word-timed split end (%d ms) to differ from proportional split end (%d ms)",
			actualDialogues[0].End, withoutWordsDialogues[0].End)
	}

	actualFile := filepath.Join(tmpDir, "actual.ass")
	if err := os.WriteFile(actualFile, actualBytes, 0644); err != nil {
		t.Fatalf("write actual.ass: %v", err)
	}

	t.Run("Delivered matches expected reference with word timings", func(t *testing.T) {
		report, err := VerifyDeliveredASSAgainstExpected(actualFile, expectedWithWordsBytes)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !report.Passed {
			t.Fatalf("expected report.Passed == true, got false. Failures: %v", report.Failures)
		}
		if !report.HeaderPassed {
			t.Fatal("expected report.HeaderPassed == true")
		}
		if len(report.Failures) != 0 {
			t.Fatalf("expected 0 failures, got: %v", report.Failures)
		}
		if report.TotalActual != len(actualDialogues) || report.TotalExpected != len(actualDialogues) {
			t.Fatalf("event count mismatch: actual %d vs expected %d", report.TotalActual, report.TotalExpected)
		}
	})

	t.Run("Delivered fails against reference without word timings", func(t *testing.T) {
		report, err := VerifyDeliveredASSAgainstExpected(actualFile, expectedWithoutWordsBytes)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if report.Passed {
			t.Fatal("expected report.Passed == false when comparing word-timed delivery against non-word reference")
		}
		if len(report.Failures) == 0 {
			t.Fatal("expected failure descriptions for timing discrepancy, got 0")
		}
		// Confirm timing mismatch failure is present
		foundTimingMismatch := false
		for _, f := range report.Failures {
			if strings.Contains(f, "time mismatch") {
				foundTimingMismatch = true
				break
			}
		}
		if !foundTimingMismatch {
			t.Fatalf("expected timing mismatch failure, got: %v", report.Failures)
		}
	})

	t.Run("CRLF delivery matches LF reference with word timings", func(t *testing.T) {
		crlfActual := strings.ReplaceAll(string(actualBytes), "\n", "\r\n")
		crlfFile := filepath.Join(tmpDir, "actual_crlf.ass")
		if err := os.WriteFile(crlfFile, []byte(crlfActual), 0644); err != nil {
			t.Fatalf("write crlf: %v", err)
		}
		report, err := VerifyDeliveredASSAgainstExpected(crlfFile, expectedWithWordsBytes)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !report.Passed {
			t.Fatalf("CRLF delivered file failed against LF reference: %v", report.Failures)
		}
	})

	t.Run("Dialogue count mismatch detected", func(t *testing.T) {
		// Truncate to first event
		lines := strings.Split(string(actualBytes), "\n")
		var truncatedLines []string
		dialogueCount := 0
		for _, line := range lines {
			if strings.HasPrefix(strings.TrimSpace(line), "Dialogue:") {
				dialogueCount++
				if dialogueCount > 1 {
					continue
				}
			}
			truncatedLines = append(truncatedLines, line)
		}
		truncFile := filepath.Join(tmpDir, "truncated.ass")
		if err := os.WriteFile(truncFile, []byte(strings.Join(truncatedLines, "\n")), 0644); err != nil {
			t.Fatalf("write trunc: %v", err)
		}
		report, err := VerifyDeliveredASSAgainstExpected(truncFile, expectedWithWordsBytes)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if report.Passed {
			t.Fatal("expected report.Passed == false for dialogue count mismatch")
		}
		if report.TotalActual == report.TotalExpected {
			t.Fatalf("expected actual count != expected count, got %d", report.TotalActual)
		}
	})

	t.Run("Line-by-line dialogue text tampering detected", func(t *testing.T) {
		tampered := strings.Replace(string(actualBytes), "tomorrow", "tonight", 1)
		tFile := filepath.Join(tmpDir, "tampered.ass")
		if err := os.WriteFile(tFile, []byte(tampered), 0644); err != nil {
			t.Fatalf("write tampered: %v", err)
		}
		report, err := VerifyDeliveredASSAgainstExpected(tFile, expectedWithWordsBytes)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if report.Passed {
			t.Fatal("expected report.Passed == false for text tampering")
		}
		if len(report.Failures) == 0 {
			t.Fatal("expected failure reporting tampered text")
		}
	})
}

// TestCountVisibleLines_Equivalence_TDD verifies that countVisibleLines treats real LF (\n),
// real CRLF (\r\n), and ASS hard newline (\N) as equivalent line separators, preserves literal ASS
// soft wrap (\n) within line, and correctly reports multi-line dialogue counts for known fixtures.
func TestCountVisibleLines_Equivalence_TDD(t *testing.T) {
	t.Run("Equivalence of newline representations", func(t *testing.T) {
		line1 := "First line of dialogue"
		line2 := "Second line of dialogue"

		textRealLF := line1 + "\n" + line2
		textRealCRLF := line1 + "\r\n" + line2
		textASSHard := line1 + `\N` + line2
		textASSSoft := line1 + `\n` + line2

		countLF := countVisibleLines(textRealLF)
		countCRLF := countVisibleLines(textRealCRLF)
		countASSHard := countVisibleLines(textASSHard)
		countASSSoft := countVisibleLines(textASSSoft)

		if countLF != 2 {
			t.Errorf("expected countLF == 2, got %d", countLF)
		}
		if countCRLF != 2 {
			t.Errorf("expected countCRLF == 2, got %d", countCRLF)
		}
		if countASSHard != 2 {
			t.Errorf("expected countASSHard == 2, got %d", countASSHard)
		}
		if countASSSoft != 1 {
			t.Errorf("expected countASSSoft == 1, got %d", countASSSoft)
		}
	})

	t.Run("Known multi-line fixtures EN 396 and ZH 125", func(t *testing.T) {
		// EN 396 two-person dialogue
		en396Plain := "- Why is this time different?\n- The presence of General Alexander"
		en396ASS := `- Why is this time different?\N- The presence of General Alexander`
		if cnt := countVisibleLines(en396Plain); cnt != 2 {
			t.Errorf("en396Plain: expected 2 lines, got %d", cnt)
		}
		if cnt := countVisibleLines(en396ASS); cnt != 2 {
			t.Errorf("en396ASS: expected 2 lines, got %d", cnt)
		}

		// ZH 125 two-person dialogue
		zh125Plain := "- 我们不太了解阿尔-纳瓦兹…\n- 不"
		zh125ASS := `- 我们不太了解阿尔-纳瓦兹…\N- 不`
		if cnt := countVisibleLines(zh125Plain); cnt != 2 {
			t.Errorf("zh125Plain: expected 2 lines, got %d", cnt)
		}
		if cnt := countVisibleLines(zh125ASS); cnt != 2 {
			t.Errorf("zh125ASS: expected 2 lines, got %d", cnt)
		}
	})

	t.Run("Negative and edge cases", func(t *testing.T) {
		if cnt := countVisibleLines(""); cnt != 0 {
			t.Errorf("empty string: expected 0, got %d", cnt)
		}
		if cnt := countVisibleLines("   \t  \n  \r\n  "); cnt != 0 {
			t.Errorf("whitespace only: expected 0, got %d", cnt)
		}
		if cnt := countVisibleLines(`{\an8}{\pos(100,200)}`); cnt != 0 {
			t.Errorf("tags only: expected 0, got %d", cnt)
		}
		if cnt := countVisibleLines("Single line"); cnt != 1 {
			t.Errorf("single line: expected 1, got %d", cnt)
		}
		if cnt := countVisibleLines("Line 1\n\n\nLine 2"); cnt != 2 {
			t.Errorf("consecutive newlines: expected 2, got %d", cnt)
		}
	})
}

// TestValidateSampleArtifacts reproduces and verifies a delivered sample ASS against an
// independently rendered expected ASS using a credential-free render-settings.json snapshot.
// It is an explicit opt-in test triggered by the FUSIONN_VERIFY_SAMPLE_DIR environment variable.
// By default (when FUSIONN_VERIFY_SAMPLE_DIR is unset), this test is skipped to prevent
// coupling make test to external directory structures or temporary credential files.
func TestValidateSampleArtifacts(t *testing.T) {
	sampleDir := os.Getenv("FUSIONN_VERIFY_SAMPLE_DIR")
	if sampleDir == "" {
		t.Skip("skipping sample artifact validation: set FUSIONN_VERIFY_SAMPLE_DIR to opt in")
	}

	settingsPath := filepath.Join(sampleDir, "render-settings.json")
	rawSettings, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("read render-settings.json from %s: %v", sampleDir, err)
	}

	var settings struct {
		Width  int `json:"width"`
		Height int `json:"height"`
		LLM    struct {
			EnChars   *int `json:"en_chars"`
			ZhChars   *int `json:"zh_chars"`
			EnGapMS   *int `json:"en_gap_ms"`
			ZhGapMS   *int `json:"zh_gap_ms"`
			SpanMS    *int `json:"span_ms"`
			FillGapMS *int `json:"fill_gap_ms"`
		} `json:"llm"`
	}
	if err := json.Unmarshal(rawSettings, &settings); err != nil {
		t.Fatalf("unmarshal render-settings.json: %v", err)
	}

	if settings.Width <= 0 || settings.Height <= 0 {
		t.Fatalf("render-settings.json: missing or invalid width/height (%dx%d)", settings.Width, settings.Height)
	}
	if settings.LLM.EnChars == nil || settings.LLM.ZhChars == nil ||
		settings.LLM.EnGapMS == nil || settings.LLM.ZhGapMS == nil ||
		settings.LLM.SpanMS == nil || settings.LLM.FillGapMS == nil {
		t.Fatalf("render-settings.json: missing required LLM layout fields (en_chars, zh_chars, en_gap_ms, zh_gap_ms, span_ms, fill_gap_ms)")
	}

	llmCfg := config.LLMConfig{
		EnChars:   *settings.LLM.EnChars,
		ZhChars:   *settings.LLM.ZhChars,
		EnGapMS:   *settings.LLM.EnGapMS,
		ZhGapMS:   *settings.LLM.ZhGapMS,
		SpanMS:    *settings.LLM.SpanMS,
		FillGapMS: *settings.LLM.FillGapMS,
	}

	stylePath := filepath.Join(sampleDir, "delivered-style.json")
	rawStyle, err := os.ReadFile(stylePath)
	if err != nil {
		t.Fatalf("read delivered-style.json from %s: %v", sampleDir, err)
	}
	var style config.ASSStyleConfig
	if err := json.Unmarshal(rawStyle, &style); err != nil {
		t.Fatalf("unmarshal delivered-style.json: %v", err)
	}
	if style.PlayResX <= 0 || style.PlayResY <= 0 {
		t.Fatalf("delivered-style.json: missing or invalid PlayResX/PlayResY (%dx%d)", style.PlayResX, style.PlayResY)
	}

	rawBundle, err := os.ReadFile(filepath.Join(sampleDir, "sources.json"))
	if err != nil {
		t.Fatalf("read sources.json: %v", err)
	}
	var bundle Bundle
	if err := json.Unmarshal(rawBundle, &bundle); err != nil {
		t.Fatalf("unmarshal sources.json: %v", err)
	}

	rawMapping, err := os.ReadFile(filepath.Join(sampleDir, "alignment", "mapping.json"))
	if err != nil {
		t.Fatalf("read mapping.json: %v", err)
	}
	var mapping Mapping
	if err := json.Unmarshal(rawMapping, &mapping); err != nil {
		t.Fatalf("unmarshal mapping.json: %v", err)
	}

	var words WordTimings
	wordsPath := filepath.Join(sampleDir, "word-timings.json")
	if rawWords, errW := os.ReadFile(wordsPath); errW == nil {
		if err := json.Unmarshal(rawWords, &words); err != nil {
			t.Fatalf("unmarshal word-timings.json: %v", err)
		}
	}

	width := settings.Width
	if width == 0 {
		width = 1920
	}
	height := settings.Height
	if height == 0 {
		height = 1080
	}

	expectedData, _, err := renderWithAudits(bundle, mapping, llmCfg, style, width, height, words)
	if err != nil {
		t.Fatalf("renderWithAudits: %v", err)
	}

	assPath := filepath.Join(sampleDir, "bilingual.ass")
	report, err := VerifyDeliveredASSAgainstExpected(assPath, expectedData)
	if err != nil {
		t.Fatalf("VerifyDeliveredASSAgainstExpected: %v", err)
	}
	if !report.Passed {
		t.Fatalf("bilingual.ass failed verification: %v", report.Failures)
	}
	t.Logf("sample bilingual.ass at %s verified successfully against render-settings.json: %d dialogues match", sampleDir, report.TotalActual)
}
