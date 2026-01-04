package subtitle

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

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
	return "Output"
}

// ShouldRun determines if output should run.
func (p *OutputProcessor) ShouldRun(pctx *ProcessingContext) bool {
	// Run if we have a merged subtitle
	return pctx.MergedSubPath != ""
}

// Process copies merged subtitle to final destination.
func (p *OutputProcessor) Process(ctx context.Context, pctx *ProcessingContext) error {
	if pctx.MergedSubPath == "" {
		return fmt.Errorf("merged subtitle path not set")
	}

	// Determine output path
	videoDir := filepath.Dir(pctx.VideoPath)
	videoBase := filepath.Base(pctx.VideoPath)
	videoExt := filepath.Ext(videoBase)
	videoName := videoBase[:len(videoBase)-len(videoExt)]

	finalPath := filepath.Join(videoDir, videoName+".ass")

	// If not outputting to same directory, use a configured output directory
	// For now, we always output to the same directory
	if !p.outputSameDir {
		logger.Warn("Output to separate directory not yet implemented, using video directory")
	}

	logger.Infof("Copying merged subtitle: %s → %s", pctx.MergedSubPath, finalPath)

	// Copy file
	if err := copyFile(pctx.MergedSubPath, finalPath); err != nil {
		return fmt.Errorf("failed to copy merged subtitle: %w", err)
	}

	// Update context with final path
	pctx.MergedSubPath = finalPath

	logger.Infof("✅ Merged subtitle saved: %s", finalPath)
	return nil
}

// copyFile is a helper function to copy files
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("failed to read source: %w", err)
	}

	if err := os.WriteFile(dst, data, 0644); err != nil {
		return fmt.Errorf("failed to write destination: %w", err)
	}

	return nil
}

