package subtitlellm

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
)

// writeFonts materializes the embedded Go fonts into a temp dir so the real
// parse/match/advance path is exercised without depending on host fonts.
func writeFonts(t *testing.T, dir string, bold, regular bool) {
	t.Helper()
	if regular {
		if err := os.WriteFile(filepath.Join(dir, "Go-Regular.ttf"), goregular.TTF, 0644); err != nil {
			t.Fatal(err)
		}
	}
	if bold {
		if err := os.WriteFile(filepath.Join(dir, "Go-Bold.ttf"), gobold.TTF, 0644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestMeasureTextWidthRealFontFaceSelection(t *testing.T) {
	regularOnly := t.TempDir()
	writeFonts(t, regularOnly, false, true)

	wReg, okReg := MeasureTextWidth("Hello World", "Go", 40, 100, 0, false, regularOnly)
	if !okReg || wReg <= 0 {
		t.Fatalf("regular face: expected usable width, got ok=%v w=%f", okReg, wReg)
	}

	// ASS Bold=true must not silently reuse the Regular face.
	if _, ok := MeasureTextWidth("Hello World", "Go", 40, 100, 0, true, regularOnly); ok {
		t.Fatal("bold request must fall back when only the regular face is present")
	}

	// Italic is not requested by this style, and a reverse lookup must not
	// accept an italic face as the regular one.
	both := t.TempDir()
	writeFonts(t, both, true, true)
	wBold, okBold := MeasureTextWidth("Hello World", "Go", 40, 100, 0, true, both)
	if !okBold || wBold <= 0 {
		t.Fatalf("bold face: expected usable width, got ok=%v w=%f", okBold, wBold)
	}
}

func TestMeasureTextWidthScaleSpacingAndMissingGlyph(t *testing.T) {
	dir := t.TempDir()
	writeFonts(t, dir, false, true)

	base, ok := MeasureTextWidth("Hello World", "Go", 40, 100, 0, false, dir)
	if !ok {
		t.Fatal("baseline width unavailable")
	}
	scaled, _ := MeasureTextWidth("Hello World", "Go", 40, 120, 0, false, dir)
	if scaled <= base {
		t.Fatalf("scale_x 120 (%f) must exceed scale_x 100 (%f)", scaled, base)
	}
	spaced, _ := MeasureTextWidth("Hello World", "Go", 40, 100, 10, false, dir)
	if spaced <= base {
		t.Fatalf("spacing 10 (%f) must exceed spacing 0 (%f)", spaced, base)
	}

	// A glyph the font does not carry must make the measurement unusable
	// rather than silently under-measure.
	if _, ok := MeasureTextWidth("Hello \U0001F600", "Go", 40, 100, 0, false, dir); ok {
		t.Fatal("missing glyph must return ok=false")
	}

	// Unknown family must not be approximated.
	if w, ok := MeasureTextWidth("Hello", "NoSuchFamilyXYZ123", 40, 100, 0, false, dir); ok || w != 0 {
		t.Fatalf("unknown family must return (0,false), got (%f,%v)", w, ok)
	}
}
