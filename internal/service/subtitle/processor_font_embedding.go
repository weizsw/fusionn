package subtitle

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/fusionn/internal/config"
	"github.com/fusionn/pkg/logger"
)

// FontEmbeddingProcessor embeds fonts into ASS subtitle files.
type FontEmbeddingProcessor struct {
	config config.FontEmbeddingConfig
}

// NewFontEmbeddingProcessor creates a new font embedding processor.
func NewFontEmbeddingProcessor(cfg config.FontEmbeddingConfig) *FontEmbeddingProcessor {
	return &FontEmbeddingProcessor{
		config: cfg,
	}
}

// Name returns the processor name.
func (p *FontEmbeddingProcessor) Name() string {
	return "FontEmbedding"
}

// ShouldRun determines if font embedding should run.
func (p *FontEmbeddingProcessor) ShouldRun(pctx *ProcessingContext) bool {
	// Check if feature is enabled
	if !p.config.Enabled {
		return false
	}

	// Check if we have a merged subtitle to process
	if pctx.MergedSubPath == "" {
		return false
	}

	// Check if fusionn-font binary is available
	if !isFusionnFontAvailable() {
		logger.Warn("fusionn-font binary not found - skipping font embedding")
		return false
	}

	return true
}

// Process embeds fonts into the ASS subtitle file.
func (p *FontEmbeddingProcessor) Process(ctx context.Context, pctx *ProcessingContext) error {
	logger.Info("🔤 Embedding fonts into subtitle...")

	// Get original file size for logging
	originalInfo, err := os.Stat(pctx.MergedSubPath)
	if err != nil {
		logger.Warnf("⚠️ Failed to stat subtitle file: %v - skipping font embedding", err)
		return nil
	}
	originalSize := originalInfo.Size()

	// Build output path for embedded file
	dir := filepath.Dir(pctx.MergedSubPath)
	base := filepath.Base(pctx.MergedSubPath)
	ext := filepath.Ext(base)
	nameWithoutExt := strings.TrimSuffix(base, ext)
	embeddedPath := filepath.Join(dir, nameWithoutExt+".embedded.ass")

	// Execute fusionn-font
	output, err := p.executeFusionnFont(ctx, pctx.MergedSubPath, embeddedPath)
	if err != nil {
		logger.Warnf("⚠️ Font embedding failed: %v - using non-embedded subtitle", err)
		if output != "" {
			logger.Warnf("fusionn-font output: %s", output)
		}
		return nil
	}

	// Verify embedded file was created and is valid
	if err := p.verifyEmbeddedFile(embeddedPath); err != nil {
		logger.Warnf("⚠️ Embedded file validation failed: %v - using non-embedded subtitle", err)
		os.Remove(embeddedPath)
		return nil
	}

	// Replace original with embedded version
	if err := os.Rename(embeddedPath, pctx.MergedSubPath); err != nil {
		logger.Warnf("⚠️ Failed to replace subtitle with embedded version: %v", err)
		os.Remove(embeddedPath)
		return nil
	}

	// Log success with size comparison
	embeddedInfo, _ := os.Stat(pctx.MergedSubPath)
	if embeddedInfo != nil {
		embeddedSize := embeddedInfo.Size()
		logger.Infof("✅ Fonts embedded successfully (%d → %d bytes)", originalSize, embeddedSize)
	} else {
		logger.Info("✅ Fonts embedded successfully")
	}

	return nil
}

// isFusionnFontAvailable checks if fusionn-font binary exists in PATH.
func isFusionnFontAvailable() bool {
	_, err := exec.LookPath("fusionn-font")
	return err == nil
}

// buildFusionnFontCommand builds the fusionn-font command with proper arguments.
func (p *FontEmbeddingProcessor) buildFusionnFontCommand(inputPath, outputPath string) *exec.Cmd {
	args := []string{
		"subset",
		inputPath,
		"-d", p.config.FontsDir,
		"--embed",
		"--output-ass", outputPath,
	}
	return exec.Command("fusionn-font", args...)
}

// executeFusionnFont executes fusionn-font with timeout and captures output.
func (p *FontEmbeddingProcessor) executeFusionnFont(ctx context.Context, inputPath, outputPath string) (string, error) {
	// Create context with timeout
	timeout := time.Duration(p.config.TimeoutSeconds) * time.Second
	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Build command
	cmd := p.buildFusionnFontCommand(inputPath, outputPath)
	cmd = exec.CommandContext(cmdCtx, cmd.Path, cmd.Args[1:]...)

	// Capture combined output
	output, err := cmd.CombinedOutput()
	if err != nil {
		if cmdCtx.Err() == context.DeadlineExceeded {
			return string(output), fmt.Errorf("command timed out after %v", timeout)
		}
		return string(output), fmt.Errorf("command failed: %w", err)
	}

	return string(output), nil
}

// verifyEmbeddedFile verifies that the embedded file exists and has non-zero size.
func (p *FontEmbeddingProcessor) verifyEmbeddedFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("embedded file was not created")
		}
		return fmt.Errorf("failed to stat embedded file: %w", err)
	}

	if info.Size() == 0 {
		return fmt.Errorf("embedded file has zero size")
	}

	return nil
}
