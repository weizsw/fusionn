package executor

import (
	"testing"
)

func TestIsCleanitAvailable(t *testing.T) {
	result := IsCleanitAvailable()
	t.Logf("cleanit available: %v", result)
}

func TestRunCleanit_FileNotFound(t *testing.T) {
	if !IsCleanitAvailable() {
		t.Skip("cleanit not available")
	}
	err := RunCleanit(t.Context(), "/nonexistent/file.srt")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}
