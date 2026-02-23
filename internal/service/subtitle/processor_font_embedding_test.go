package subtitle

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/fusionn/internal/config"
)

func TestFontEmbeddingProcessor_Name(t *testing.T) {
	proc := NewFontEmbeddingProcessor(config.FontEmbeddingConfig{})
	if proc.Name() != "FontEmbedding" {
		t.Errorf("Expected name 'FontEmbedding', got %s", proc.Name())
	}
}

func TestFontEmbeddingProcessor_ShouldRun_Disabled(t *testing.T) {
	proc := NewFontEmbeddingProcessor(config.FontEmbeddingConfig{
		Enabled: false,
	})

	pctx := &ProcessingContext{
		MergedSubPath: "/path/to/subtitle.ass",
	}

	if proc.ShouldRun(pctx) {
		t.Error("Expected ShouldRun to return false when disabled")
	}
}

func TestFontEmbeddingProcessor_ShouldRun_NoMergedSubPath(t *testing.T) {
	tmpDir := t.TempDir()

	proc := NewFontEmbeddingProcessor(config.FontEmbeddingConfig{
		Enabled:  true,
		FontsDir: tmpDir,
	})

	pctx := &ProcessingContext{
		MergedSubPath: "",
	}

	if proc.ShouldRun(pctx) {
		t.Error("Expected ShouldRun to return false when MergedSubPath is empty")
	}
}

func TestFontEmbeddingProcessor_ShouldRun_BinaryNotFound(t *testing.T) {
	tmpDir := t.TempDir()

	proc := NewFontEmbeddingProcessor(config.FontEmbeddingConfig{
		Enabled:  true,
		FontsDir: tmpDir,
	})

	pctx := &ProcessingContext{
		MergedSubPath: "/path/to/subtitle.ass",
	}

	// This test assumes fusionn-font is not in PATH
	// If it is, the test will fail - that's expected behavior
	result := proc.ShouldRun(pctx)

	// Check if binary is actually available
	if isFusionnFontAvailable() {
		if !result {
			t.Error("Expected ShouldRun to return true when binary is available")
		}
	} else {
		if result {
			t.Error("Expected ShouldRun to return false when binary not found")
		}
	}
}

func TestFontEmbeddingProcessor_ShouldRun_AllConditionsMet(t *testing.T) {
	// Skip if fusionn-font not available
	if !isFusionnFontAvailable() {
		t.Skip("fusionn-font binary not available")
	}

	tmpDir := t.TempDir()

	proc := NewFontEmbeddingProcessor(config.FontEmbeddingConfig{
		Enabled:  true,
		FontsDir: tmpDir,
	})

	pctx := &ProcessingContext{
		MergedSubPath: "/path/to/subtitle.ass",
	}

	if !proc.ShouldRun(pctx) {
		t.Error("Expected ShouldRun to return true when all conditions met")
	}
}

func TestFontEmbeddingProcessor_Process_GracefulFallback_MissingFile(t *testing.T) {
	tmpDir := t.TempDir()
	fontsDir := filepath.Join(tmpDir, "fonts")
	os.MkdirAll(fontsDir, 0755)

	proc := NewFontEmbeddingProcessor(config.FontEmbeddingConfig{
		Enabled:        true,
		FontsDir:       fontsDir,
		TimeoutSeconds: 30,
	})

	pctx := &ProcessingContext{
		MergedSubPath: filepath.Join(tmpDir, "nonexistent.ass"),
	}

	// Should not fail - graceful fallback
	err := proc.Process(context.Background(), pctx)
	if err != nil {
		t.Errorf("Expected graceful fallback, got error: %v", err)
	}
}

func TestFontEmbeddingProcessor_Process_GracefulFallback_MissingFontsDir(t *testing.T) {
	// Skip if fusionn-font not available
	if !isFusionnFontAvailable() {
		t.Skip("fusionn-font binary not available")
	}

	tmpDir := t.TempDir()

	// Create a test ASS file
	assPath := filepath.Join(tmpDir, "test.ass")
	assContent := `[Script Info]
Title: Test

[V4+ Styles]
Format: Name, Fontname, Fontsize
Style: Default,WenQuanYi Micro Hei,20

[Events]
Format: Layer, Start, End, Style, Text
Dialogue: 0,0:00:00.00,0:00:05.00,Default,Test subtitle` //nolint:misspell // Dialogue is the correct ASS format keyword
	if err := os.WriteFile(assPath, []byte(assContent), 0644); err != nil {
		t.Fatalf("Failed to create test ASS file: %v", err)
	}

	proc := NewFontEmbeddingProcessor(config.FontEmbeddingConfig{
		Enabled:        true,
		FontsDir:       "/nonexistent/fonts",
		TimeoutSeconds: 30,
	})

	pctx := &ProcessingContext{
		MergedSubPath: assPath,
	}

	// Should not fail - graceful fallback
	err := proc.Process(context.Background(), pctx)
	if err != nil {
		t.Errorf("Expected graceful fallback, got error: %v", err)
	}

	// Original file should still exist
	if _, err := os.Stat(assPath); os.IsNotExist(err) {
		t.Error("Original file was removed")
	}
}

func TestFontEmbeddingProcessor_VerifyEmbeddedFile_NotExists(t *testing.T) {
	proc := NewFontEmbeddingProcessor(config.FontEmbeddingConfig{})

	err := proc.verifyEmbeddedFile("/nonexistent/file.ass")
	if err == nil {
		t.Error("Expected error for non-existent file")
	}
}

func TestFontEmbeddingProcessor_VerifyEmbeddedFile_ZeroSize(t *testing.T) {
	tmpDir := t.TempDir()
	emptyFile := filepath.Join(tmpDir, "empty.ass")

	if err := os.WriteFile(emptyFile, []byte{}, 0644); err != nil {
		t.Fatalf("Failed to create empty file: %v", err)
	}

	proc := NewFontEmbeddingProcessor(config.FontEmbeddingConfig{})

	err := proc.verifyEmbeddedFile(emptyFile)
	if err == nil {
		t.Error("Expected error for zero-size file")
	}
}

func TestFontEmbeddingProcessor_VerifyEmbeddedFile_Valid(t *testing.T) {
	tmpDir := t.TempDir()
	validFile := filepath.Join(tmpDir, "valid.ass")

	if err := os.WriteFile(validFile, []byte("test content"), 0644); err != nil {
		t.Fatalf("Failed to create valid file: %v", err)
	}

	proc := NewFontEmbeddingProcessor(config.FontEmbeddingConfig{})

	err := proc.verifyEmbeddedFile(validFile)
	if err != nil {
		t.Errorf("Expected no error for valid file, got: %v", err)
	}
}

func TestFontEmbeddingProcessor_BuildCommand(t *testing.T) {
	proc := NewFontEmbeddingProcessor(config.FontEmbeddingConfig{
		FontsDir: "/app/fonts",
	})

	cmd := proc.buildFusionnFontCommand("/input.ass", "/output.ass")

	expectedArgs := []string{
		"fusionn-font",
		"subset",
		"/input.ass",
		"-d", "/app/fonts",
		"--embed",
		"--output-ass", "/output.ass",
	}

	if len(cmd.Args) != len(expectedArgs) {
		t.Errorf("Expected %d args, got %d", len(expectedArgs), len(cmd.Args))
	}

	for i, expected := range expectedArgs {
		if i < len(cmd.Args) && cmd.Args[i] != expected {
			t.Errorf("Arg %d: expected %s, got %s", i, expected, cmd.Args[i])
		}
	}
}

func TestFontEmbeddingProcessor_Process_Timeout(t *testing.T) {
	// Skip if fusionn-font not available
	if !isFusionnFontAvailable() {
		t.Skip("fusionn-font binary not available")
	}

	tmpDir := t.TempDir()
	fontsDir := filepath.Join(tmpDir, "fonts")
	os.MkdirAll(fontsDir, 0755)

	// Create a test ASS file
	assPath := filepath.Join(tmpDir, "test.ass")
	assContent := `[Script Info]
Title: Test

[V4+ Styles]
Format: Name, Fontname, Fontsize
Style: Default,Arial,20

[Events]
Format: Layer, Start, End, Style, Text
Dialogue: 0,0:00:00.00,0:00:05.00,Default,Test` //nolint:misspell // Dialogue is the correct ASS format keyword
	if err := os.WriteFile(assPath, []byte(assContent), 0644); err != nil {
		t.Fatalf("Failed to create test ASS file: %v", err)
	}

	// Use very short timeout to simulate timeout scenario
	proc := NewFontEmbeddingProcessor(config.FontEmbeddingConfig{
		Enabled:        true,
		FontsDir:       fontsDir,
		TimeoutSeconds: 0, // This will cause immediate timeout
	})

	pctx := &ProcessingContext{
		MergedSubPath: assPath,
	}

	// Should not fail - graceful fallback
	err := proc.Process(context.Background(), pctx)
	if err != nil {
		t.Errorf("Expected graceful fallback on timeout, got error: %v", err)
	}
}

func TestIsFusionnFontAvailable(t *testing.T) {
	// Just check the function doesn't panic
	result := isFusionnFontAvailable()
	t.Logf("fusionn-font available: %v", result)
}
