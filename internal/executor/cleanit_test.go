package executor

import (
	"context"
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
	err := RunCleanit(context.Background(), "/nonexistent/file.srt")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}
