package subtitle

import (
	"context"
	"os"

	"go.uber.org/zap"

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
	log := logger.Component("cleanup")
	log.Info(ctx, "cleanup_started", "Cleaning up temporary files")

	// Clean up extracted subtitles. LLM-mode extraction hands out unique hidden
	// temp paths; removing exactly those paths never touches existing sidecars.
	if pctx.EnglishSubPath != "" {
		if err := os.Remove(pctx.EnglishSubPath); err != nil && !os.IsNotExist(err) {
			log.Warn(ctx, "cleanup_failed", "Failed to remove temporary file",
				zap.String("path", pctx.EnglishSubPath), zap.Error(err))
		}
	}

	if pctx.ChineseSubPath != "" {
		if err := os.Remove(pctx.ChineseSubPath); err != nil && !os.IsNotExist(err) {
			log.Warn(ctx, "cleanup_failed", "Failed to remove temporary file",
				zap.String("path", pctx.ChineseSubPath), zap.Error(err))
		}
	}

	if pctx.TempMergeDir != "" {
		if err := os.RemoveAll(pctx.TempMergeDir); err != nil {
			log.Warn(ctx, "cleanup_failed", "Failed to remove temporary merge directory",
				zap.String("path", pctx.TempMergeDir), zap.Error(err))
		}
	}

	log.Info(ctx, "cleanup_completed", "Cleanup completed")
	return nil
}
