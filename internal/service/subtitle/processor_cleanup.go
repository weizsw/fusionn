package subtitle

import (
	"context"
	"os"

	"github.com/fusionn/pkg/logger"
)

// CleanupProcessor removes temporary files.
type CleanupProcessor struct{}

// NewCleanupProcessor creates a new cleanup processor.
func NewCleanupProcessor() *CleanupProcessor {
	return &CleanupProcessor{}
}

// Name returns the processor name.
func (p *CleanupProcessor) Name() string {
	return ProcessorNameCleanup
}

// ShouldRun determines if cleanup should run.
func (p *CleanupProcessor) ShouldRun(pctx *ProcessingContext) bool {
	// Always run cleanup
	return true
}

// Process removes temporary files.
func (p *CleanupProcessor) Process(ctx context.Context, pctx *ProcessingContext) error {
	log := logger.Indent()
	log.Info("Cleaning up temporary files")

	// Clean up extracted subtitles
	if pctx.EnglishSubPath != "" {
		if err := os.Remove(pctx.EnglishSubPath); err != nil && !os.IsNotExist(err) {
			log.Warnf("Failed to remove %s: %v", pctx.EnglishSubPath, err)
		}
	}

	if pctx.ChineseSubPath != "" {
		if err := os.Remove(pctx.ChineseSubPath); err != nil && !os.IsNotExist(err) {
			log.Warnf("Failed to remove %s: %v", pctx.ChineseSubPath, err)
		}
	}

	if pctx.TempMergeDir != "" {
		if err := os.RemoveAll(pctx.TempMergeDir); err != nil {
			log.Warnf("Failed to remove temp merge dir %s: %v", pctx.TempMergeDir, err)
		}
	}

	log.Info("✅ Cleanup completed")
	return nil
}
