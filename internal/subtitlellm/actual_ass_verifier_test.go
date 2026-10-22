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

// TestValidateTerminalListGPT61EnhancedArtifacts loads the generated artifacts from
// the live GPT-6.1 enhanced run, validates bilingual.ass against an independently
// rendered expected ASS, and outputs validation.json for the review-ledger tool.
func TestValidateTerminalListGPT61EnhancedArtifacts(t *testing.T) {
	sampleDir := "/Users/maverick/Desktop/fusionn-subtitle-split-test/terminal-list-s02e01-gpt61-enhanced"
	if _, err := os.Stat(filepath.Join(sampleDir, "bilingual.ass")); err != nil {
		t.Skip("sample directory not present, skipping")
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

	rawWords, err := os.ReadFile(filepath.Join(sampleDir, "word-timings.json"))
	if err != nil {
		t.Fatalf("read word-timings.json: %v", err)
	}
	var words WordTimings
	if err := json.Unmarshal(rawWords, &words); err != nil {
		t.Fatalf("unmarshal word-timings.json: %v", err)
	}

	loadedCfg, err := config.Load("/tmp/fusionn-phase-b-gpt61.yaml")
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	cfg := loadedCfg.Subtitle.LLM
	style := loadedCfg.Subtitle.ASSStyle

	expectedData, events, err := renderWithAudits(bundle, mapping, cfg, style, 3840, 1632, words)
	if err != nil {
		t.Fatalf("renderWithAudits: %v", err)
	}

	// 1. Strict delivery verification using Phase A verifier
	assPath := filepath.Join(sampleDir, "bilingual.ass")
	report, err := VerifyDeliveredASSAgainstExpected(assPath, expectedData)
	if err != nil {
		t.Fatalf("VerifyDeliveredASSAgainstExpected: %v", err)
	}
	if !report.Passed {
		t.Fatalf("bilingual.ass failed verification: %v", report.Failures)
	}
	t.Logf("bilingual.ass verified successfully: %d dialogues match expected reference", report.TotalActual)

	// 2. Export validation.json for review-ledger tool
	type eventAuditRecord struct {
		GroupIndex int    `json:"group"`
		Strategy   string `json:"strategy"`
		ENIDs      []int  `json:"en_ids"`
		ZHIDs      []int  `json:"zh_ids"`
		StartMS    int    `json:"start_ms"`
		EndMS      int    `json:"end_ms"`
		Alignment  int    `json:"alignment"`
		Top        bool   `json:"top"`
		ENText     string `json:"en_text"`
		ZHText     string `json:"zh_text"`
		ENLines    int    `json:"en_lines"`
		ZHLines    int    `json:"zh_lines"`
		ZHMerged   bool   `json:"zh_merged"`
		ENMerged   bool   `json:"en_merged"`
	}
	var eventAudits []eventAuditRecord
	for _, ev := range events {
		eventAudits = append(eventAudits, eventAuditRecord{
			GroupIndex: ev.group,
			Strategy:   ev.strategy,
			ENIDs:      ev.enIDs,
			ZHIDs:      ev.zhIDs,
			StartMS:    ev.assStart,
			EndMS:      ev.assEnd,
			Alignment:  ev.alignment,
			Top:        ev.alignment >= 7,
			ENText:     ev.enText,
			ZHText:     ev.zhText,
			ENLines:    countVisibleLines(ev.enText),
			ZHLines:    countVisibleLines(ev.zhText),
			ZHMerged:   ev.zhMerged,
			ENMerged:   ev.enMerged,
		})
	}
	valObj := map[string]interface{}{
		"event_audit": eventAudits,
	}
	valBytes, errVal := json.MarshalIndent(valObj, "", "  ")
	if errVal != nil {
		t.Fatalf("marshal validation.json: %v", errVal)
	}
	if err := os.WriteFile(filepath.Join(sampleDir, "validation.json"), valBytes, 0644); err != nil {
		t.Fatalf("write validation.json: %v", err)
	}
	t.Logf("validation.json written with %d rendered events", len(eventAudits))
}
