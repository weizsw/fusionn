package subtitle

import (
	"context"
	"os"
	"strings"

	"github.com/fusionn/pkg/logger"
)

// CleanupProcessor removes temporary files created during processing.
type CleanupProcessor struct{}

// NewCleanupProcessor creates a new cleanup processor.
func NewCleanupProcessor() *CleanupProcessor {
	return &CleanupProcessor{}
}

// Name returns the processor name.
func (p *CleanupProcessor) Name() string {
	return "CleanupProcessor"
}

// ShouldRun always runs to clean up temporary files.
func (p *CleanupProcessor) ShouldRun(pctx *ProcessingContext) bool {
	return true
}

// Process removes temporary subtitle files.
func (p *CleanupProcessor) Process(ctx context.Context, pctx *ProcessingContext) error {
	logger.Infof("Cleaning up temporary files")

	// List of temp files to remove
	tempFiles := []string{
		pctx.EnglishSubPath,
		pctx.ChineseSubPath,
	}

	// Only remove the merged temp file if it's different from the final output
	// (i.e., if we copied it to a different location)
	if pctx.MergedSubPath != "" && !strings.HasSuffix(pctx.MergedSubPath, "_bilingual.ass") {
		tempFiles = append(tempFiles, pctx.MergedSubPath)
	}

	for _, file := range tempFiles {
		if file == "" {
			continue
		}

		if err := os.Remove(file); err != nil {
			logger.Warnf("Failed to remove temp file %s: %v", file, err)
			// Don't return error, just log warning
		} else {
			logger.Debugf("Removed temp file: %s", file)
		}
	}

	logger.Infof("✅ Cleanup completed")
	return nil
}
