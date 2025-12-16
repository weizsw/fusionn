package subtitle

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fusionn/pkg/logger"
)

// OutputProcessor copies the merged subtitle to the final destination.
type OutputProcessor struct {
	outputSameDir bool
}

// NewOutputProcessor creates a new output processor.
func NewOutputProcessor(outputSameDir bool) *OutputProcessor {
	return &OutputProcessor{
		outputSameDir: outputSameDir,
	}
}

// Name returns the processor name.
func (p *OutputProcessor) Name() string {
	return "OutputProcessor"
}

// ShouldRun runs if merge was successful.
func (p *OutputProcessor) ShouldRun(pctx *ProcessingContext) bool {
	return pctx.MergedSubPath != ""
}

// Process copies the merged subtitle to the video directory.
func (p *OutputProcessor) Process(ctx context.Context, pctx *ProcessingContext) error {
	logger.Infof("Copying merged subtitle to video directory")

	// Determine output path: same directory as video, with _bilingual.ass suffix
	videoDir := filepath.Dir(pctx.VideoPath)
	videoBase := strings.TrimSuffix(filepath.Base(pctx.VideoPath), filepath.Ext(pctx.VideoPath))
	outputPath := filepath.Join(videoDir, videoBase+"_bilingual.ass")

	// Copy merged subtitle to output path
	if err := copyFile(pctx.MergedSubPath, outputPath); err != nil {
		return fmt.Errorf("failed to copy merged subtitle: %w", err)
	}

	// Update context with final output path
	pctx.MergedSubPath = outputPath

	logger.Infof("✅ Merged subtitle saved: %s", outputPath)
	return nil
}

// copyFile copies a file from src to dst.
func copyFile(src, dst string) error {
	input, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("failed to read source file: %w", err)
	}

	if err := os.WriteFile(dst, input, 0644); err != nil {
		return fmt.Errorf("failed to write destination file: %w", err)
	}

	return nil
}
