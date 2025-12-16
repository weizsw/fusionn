package version

import (
	"bytes"
	"strings"
	"testing"
)

func TestBanner(t *testing.T) {
	banner := Banner()
	if banner == "" {
		t.Error("Banner should not be empty")
	}
	// The banner is ASCII art, check for key characters
	if len(banner) < 10 {
		t.Error("Banner should be substantial in size")
	}
}

func TestPrintBanner(t *testing.T) {
	var buf bytes.Buffer
	PrintBanner(&buf)

	output := buf.String()
	if output == "" {
		t.Error("PrintBanner output should not be empty")
	}
	if !strings.Contains(output, "fusionn") {
		t.Error("Output should contain 'fusionn'")
	}
	if !strings.Contains(output, Version) {
		t.Errorf("Output should contain version %s", Version)
	}
}
