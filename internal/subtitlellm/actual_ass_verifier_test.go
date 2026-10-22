package subtitlellm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Gap 1: Test verification of ASS headers and Style definitions, including CRLF tolerance.
func TestVerifyDeliveredASSHeader_Gap1(t *testing.T) {
	validASS := `[Script Info]
Title: Bilingual Subtitle
ScriptType: v4.00+
WrapStyle: 2
ScaledBorderAndShadow: yes
Collisions: Normal
PlayResX: 1920
PlayResY: 1080
YCbCr Matrix: TV.709

[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding
Style: Default,Source Han Sans SC,46,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,1,0,0,0,100,100,0.0,0,1,2.0,1.0,2,30,30,30,1
Style: Eng,Source Han Sans SC,32,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0.0,0,1,2.0,1.0,2,30,30,30,1

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Dialogue: 0,0:01:00.00,0:01:02.00,Default,,0,0,0,,Line 1
`

	t.Run("Valid LF header passes", func(t *testing.T) {
		failures, err := VerifyDeliveredASSHeader(validASS, 1920, 1080, 2)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(failures) != 0 {
			t.Fatalf("expected 0 failures, got: %v", failures)
		}
	})

	t.Run("Valid CRLF header passes identically", func(t *testing.T) {
		crlfASS := strings.ReplaceAll(validASS, "\n", "\r\n")
		failures, err := VerifyDeliveredASSHeader(crlfASS, 1920, 1080, 2)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(failures) != 0 {
			t.Fatalf("expected 0 failures for CRLF, got: %v", failures)
		}
	})

	t.Run("Missing Script Info section detected", func(t *testing.T) {
		tampered := strings.ReplaceAll(validASS, "[Script Info]", "[Info]")
		failures, err := VerifyDeliveredASSHeader(tampered, 1920, 1080, 2)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(failures) == 0 {
			t.Fatal("expected failure for missing [Script Info]")
		}
	})

	t.Run("PlayResX mismatch detected", func(t *testing.T) {
		tampered := strings.ReplaceAll(validASS, "PlayResX: 1920", "PlayResX: 1280")
		failures, err := VerifyDeliveredASSHeader(tampered, 1920, 1080, 2)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(failures) == 0 || !strings.Contains(failures[0], "PlayResX mismatch") {
			t.Fatalf("expected PlayResX mismatch failure, got: %v", failures)
		}
	})

	t.Run("PlayResY mismatch detected", func(t *testing.T) {
		tampered := strings.ReplaceAll(validASS, "PlayResY: 1080", "PlayResY: 720")
		failures, err := VerifyDeliveredASSHeader(tampered, 1920, 1080, 2)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(failures) == 0 || !strings.Contains(failures[0], "PlayResY mismatch") {
			t.Fatalf("expected PlayResY mismatch failure, got: %v", failures)
		}
	})

	t.Run("Missing Style Default detected", func(t *testing.T) {
		tampered := strings.ReplaceAll(validASS, "Style: Default,", "Style: Custom,")
		failures, err := VerifyDeliveredASSHeader(tampered, 1920, 1080, 2)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(failures) == 0 || !strings.Contains(failures[0], "missing 'Default' style") {
			t.Fatalf("expected missing Default style failure, got: %v", failures)
		}
	})

	t.Run("Style Default alignment mismatch detected", func(t *testing.T) {
		// In validASS, Default style alignment is 2. We request expected alignment 8 (top).
		failures, err := VerifyDeliveredASSHeader(validASS, 1920, 1080, 8)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(failures) == 0 || !strings.Contains(failures[0], "alignment mismatch") {
			t.Fatalf("expected alignment mismatch failure, got: %v", failures)
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

// Gap 3: Test direct delivery verification against expected ASS / word timing baseline.
func TestVerifyDeliveredASSAgainstExpected_Gap3(t *testing.T) {
	tmpDir := t.TempDir()

	baseASS := `[Script Info]
Title: Bilingual Subtitle
ScriptType: v4.00+
WrapStyle: 2
PlayResX: 1920
PlayResY: 1080

[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding
Style: Default,Source Han Sans SC,46,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,1,0,0,0,100,100,0.0,0,1,2.0,1.0,2,30,30,30,1

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Dialogue: 0,0:00:01.00,0:00:03.00,Default,,0,0,0,,First line.\N{\rEng}First english.
Dialogue: 0,0:00:04.00,0:00:06.00,Default,,0,0,0,,{\pos(960,800)}Middle line.\N{\rEng}Middle english.
Dialogue: 0,0:00:07.00,0:00:09.00,Default,,0,0,0,,Last line.\N{\rEng}Last english.
`
	assPath := filepath.Join(tmpDir, "delivered.ass")
	if err := os.WriteFile(assPath, []byte(baseASS), 0644); err != nil {
		t.Fatalf("write base ass: %v", err)
	}

	t.Run("Identical delivery passes with full report", func(t *testing.T) {
		report, err := VerifyDeliveredASSAgainstExpected(assPath, []byte(baseASS))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !report.Passed {
			t.Fatalf("expected report.Passed == true, got false. Failures: %v", report.Failures)
		}
		if report.TotalActual != 3 || report.TotalExpected != 3 {
			t.Fatalf("expected 3 events, got %d actual, %d expected", report.TotalActual, report.TotalExpected)
		}
		if !report.HeaderPassed || !report.FirstEventMatch || !report.LastEventMatch || !report.KeyAnchorsMatch {
			t.Fatalf("expected all match flags true, got: %+v", report)
		}
	})

	t.Run("CRLF delivery matches LF expected reference", func(t *testing.T) {
		crlfASS := strings.ReplaceAll(baseASS, "\n", "\r\n")
		crlfPath := filepath.Join(tmpDir, "delivered_crlf.ass")
		if err := os.WriteFile(crlfPath, []byte(crlfASS), 0644); err != nil {
			t.Fatalf("write crlf ass: %v", err)
		}
		report, err := VerifyDeliveredASSAgainstExpected(crlfPath, []byte(baseASS))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !report.Passed {
			t.Fatalf("CRLF delivered file failed against LF reference: %v", report.Failures)
		}
	})

	t.Run("Missing dialogue count detected", func(t *testing.T) {
		truncated := strings.ReplaceAll(baseASS, "Dialogue: 0,0:00:07.00,0:00:09.00,Default,,0,0,0,,Last line.\\N{\\rEng}Last english.\n", "")
		truncPath := filepath.Join(tmpDir, "truncated.ass")
		if err := os.WriteFile(truncPath, []byte(truncated), 0644); err != nil {
			t.Fatalf("write trunc: %v", err)
		}
		report, err := VerifyDeliveredASSAgainstExpected(truncPath, []byte(baseASS))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if report.Passed {
			t.Fatal("expected report.Passed == false for truncated dialogues")
		}
		if report.TotalActual != 2 || report.TotalExpected != 3 {
			t.Fatalf("expected 2 actual vs 3 expected, got: %d vs %d", report.TotalActual, report.TotalExpected)
		}
	})

	t.Run("First event mismatch detected", func(t *testing.T) {
		tampered := strings.Replace(baseASS, "0:00:01.00", "0:00:01.50", 1)
		tPath := filepath.Join(tmpDir, "tampered_first.ass")
		if err := os.WriteFile(tPath, []byte(tampered), 0644); err != nil {
			t.Fatalf("write: %v", err)
		}
		report, err := VerifyDeliveredASSAgainstExpected(tPath, []byte(baseASS))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if report.Passed || report.FirstEventMatch {
			t.Fatalf("expected FirstEventMatch == false, got: %+v", report)
		}
	})

	t.Run("Last event mismatch detected", func(t *testing.T) {
		tampered := strings.Replace(baseASS, "Last line.", "Tampered last line.", 1)
		tPath := filepath.Join(tmpDir, "tampered_last.ass")
		if err := os.WriteFile(tPath, []byte(tampered), 0644); err != nil {
			t.Fatalf("write: %v", err)
		}
		report, err := VerifyDeliveredASSAgainstExpected(tPath, []byte(baseASS))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if report.Passed || report.LastEventMatch {
			t.Fatalf("expected LastEventMatch == false, got: %+v", report)
		}
	})

	t.Run("Middle anchor position tag removal detected", func(t *testing.T) {
		tampered := strings.Replace(baseASS, `{\pos(960,800)}`, "", 1)
		tPath := filepath.Join(tmpDir, "tampered_anchor.ass")
		if err := os.WriteFile(tPath, []byte(tampered), 0644); err != nil {
			t.Fatalf("write: %v", err)
		}
		report, err := VerifyDeliveredASSAgainstExpected(tPath, []byte(baseASS))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if report.Passed || report.KeyAnchorsMatch {
			t.Fatalf("expected KeyAnchorsMatch == false, got: %+v", report)
		}
	})
}
