package subtitlellm

import (
	"testing"
)

func TestMeasureTextWidthRealFontAndFallback(t *testing.T) {
	// 1. Real font test (Helvetica on macOS)
	width, ok := MeasureTextWidth("Hello World", "Helvetica", 40, 100, 0, "")
	if ok {
		if width <= 0 {
			t.Fatalf("expected positive width for Helvetica, got %f", width)
		}
	} else {
		// If running on an OS without Helvetica, confirm that missing font returns false
		t.Log("Helvetica not present in default search paths, fallback behavior verified")
	}

	// 2. Non-existent font MUST return (0, false)
	missingWidth, missingOK := MeasureTextWidth("Hello World", "DefinitelyNonExistentFontXYZ123", 40, 100, 0, "")
	if missingOK {
		t.Fatalf("expected ok=false for non-existent font, got true with width %f", missingWidth)
	}
	if missingWidth != 0 {
		t.Fatalf("expected width=0 for non-existent font, got %f", missingWidth)
	}

	// 3. Spacing and scale checks
	if ok {
		wNormal, _ := MeasureTextWidth("AB", "Helvetica", 40, 100, 0, "")
		wScaled, _ := MeasureTextWidth("AB", "Helvetica", 40, 120, 0, "")
		if wScaled <= wNormal {
			t.Fatalf("scale_x 120 width %f should be greater than scale_x 100 width %f", wScaled, wNormal)
		}
		wSpaced, _ := MeasureTextWidth("AB", "Helvetica", 40, 100, 10, "")
		if wSpaced <= wNormal {
			t.Fatalf("spacing 10 width %f should be greater than spacing 0 width %f", wSpaced, wNormal)
		}
	}
}
