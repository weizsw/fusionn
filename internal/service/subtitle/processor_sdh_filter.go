package subtitle

import (
	"context"
	"os"

	"github.com/fusionn/internal/executor"
	"github.com/fusionn/pkg/logger"
)

// SDHFilterProcessor removes SDH annotations from English SRT subtitles using cleanit.
type SDHFilterProcessor struct{}

// NewSDHFilterProcessor creates a new SDH filter processor.
func NewSDHFilterProcessor() *SDHFilterProcessor {
	return &SDHFilterProcessor{}
}

// Name returns the processor name.
func (p *SDHFilterProcessor) Name() string {
	return "SDHFilter"
}

// ShouldRun returns true when the selected English track is SDH and has been extracted.
func (p *SDHFilterProcessor) ShouldRun(pctx *ProcessingContext) bool {
	if pctx.Analysis == nil || pctx.Analysis.EnglishTrack == nil {
		return false
	}
	return pctx.Analysis.EnglishTrack.IsSDH && pctx.EnglishSubPath != ""
}

// Process runs cleanit on the English SRT to strip SDH content.
// On failure, logs a warning and returns nil so the pipeline continues with the unfiltered SRT.
func (p *SDHFilterProcessor) Process(ctx context.Context, pctx *ProcessingContext) error {
	log := logger.Indent()

	if !executor.IsCleanitAvailable() {
		log.Warn("cleanit not found in PATH - skipping SDH filter")
		return nil
	}

	log.Infof("Filtering SDH content from: %s", pctx.EnglishSubPath)

	// Copy to temp file so original is untouched on failure
	tempPath := pctx.EnglishSubPath + ".sdh_tmp"
	data, err := os.ReadFile(pctx.EnglishSubPath)
	if err != nil {
		log.Warnf("Failed to read SRT for SDH filtering: %v", err)
		return nil
	}
	if err := os.WriteFile(tempPath, data, 0644); err != nil {
		log.Warnf("Failed to create temp file for SDH filtering: %v", err)
		return nil
	}
	defer os.Remove(tempPath)

	// Run cleanit on the temp copy
	if err := executor.RunCleanit(ctx, tempPath); err != nil {
		log.Warnf("cleanit failed: %v - continuing with unfiltered SRT", err)
		return nil
	}

	// Atomic replace: rename temp over original
	if err := os.Rename(tempPath, pctx.EnglishSubPath); err != nil {
		log.Warnf("Failed to replace SRT with filtered version: %v", err)
		return nil
	}

	log.Info("✅ SDH content filtered successfully")
	return nil
}

// IsCleanitAvailable is a package-level check for startup logging.
func IsCleanitAvailable() bool {
	return executor.IsCleanitAvailable()
}
