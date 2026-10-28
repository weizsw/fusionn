package subtitle

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"go.uber.org/zap"

	"github.com/fusionn/internal/executor"
	"github.com/fusionn/pkg/logger"
)

var (
	isCleanitAvailableFunc = executor.IsCleanitAvailable
	runCleanitFunc         = executor.RunCleanit
)

// SDHFilterProcessor removes SDH annotations from English SRT subtitles using cleanit.
type SDHFilterProcessor struct{}

// NewSDHFilterProcessor creates a new SDH filter processor.
func NewSDHFilterProcessor() *SDHFilterProcessor {
	return &SDHFilterProcessor{}
}

// Name returns the processor name.
func (p *SDHFilterProcessor) Name() string {
	return ProcessorNameSDHFilter
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
	log := logger.Component("sdh_filter")
	log.Debug(ctx, "sdh_filter_detail", "SDH filter detail", logger.Preview(pctx.EnglishSubPath))
	return filterSDHFile(ctx, pctx.EnglishSubPath, log)
}

// BazarrSDHFilterProcessor removes SDH annotations from Bazarr dual-language SRT subtitles.
type BazarrSDHFilterProcessor struct{}

func NewBazarrSDHFilterProcessor() *BazarrSDHFilterProcessor {
	return &BazarrSDHFilterProcessor{}
}

func (p *BazarrSDHFilterProcessor) Name() string {
	return ProcessorNameBazarrSDHFilter
}

func (p *BazarrSDHFilterProcessor) ShouldRun(pctx *ProcessingContext) bool {
	if pctx.ChineseSubSource != ChineseSourceBazarr || pctx.ChineseSubPath == "" {
		return false
	}
	return strings.EqualFold(filepath.Ext(pctx.ChineseSubPath), ".srt")
}

func (p *BazarrSDHFilterProcessor) Process(ctx context.Context, pctx *ProcessingContext) error {
	log := logger.Component("sdh_filter")
	data, err := os.ReadFile(pctx.ChineseSubPath)
	if err != nil {
		log.Warn(ctx, "sdh_filter_read_failed", "Failed to read Bazarr subtitle for SDH filtering", zap.Error(err))
		return nil
	}

	if !detectDualLanguageSRT(string(data)) {
		log.Debug(ctx, "sdh_filter_skipped", "Bazarr subtitle is not dual-language SRT, skipping SDH filter")
		return nil
	}

	log.Debug(ctx, "sdh_filter_detail", "SDH filter detail", logger.Preview(pctx.ChineseSubPath))
	return filterSDHFile(ctx, pctx.ChineseSubPath, log)
}

func filterSDHFile(ctx context.Context, path string, log logger.Logger) error {
	if !isCleanitAvailableFunc() {
		log.Warn(ctx, "sdh_filter_unavailable", "cleanit not found in PATH, skipping SDH filter")
		return nil
	}

	// Copy to temp file so original is untouched on failure
	tempPath := path + ".sdh_tmp"
	data, err := os.ReadFile(path)
	if err != nil {
		log.Warn(ctx, "sdh_filter_read_failed", "Failed to read SRT for SDH filtering", zap.Error(err))
		return nil
	}
	if err := os.WriteFile(tempPath, data, 0644); err != nil {
		log.Warn(ctx, "sdh_filter_temp_failed", "Failed to create temp file for SDH filtering", zap.Error(err))
		return nil
	}
	defer os.Remove(tempPath)

	// Run cleanit on the temp copy
	if err := runCleanitFunc(ctx, tempPath); err != nil {
		log.Warn(ctx, "sdh_filter_failed", "cleanit failed, continuing with unfiltered SRT", zap.Error(err))
		return nil
	}

	// Atomic replace: rename temp over original
	if err := os.Rename(tempPath, path); err != nil {
		log.Warn(ctx, "sdh_filter_replace_failed", "Failed to replace SRT with filtered version", zap.Error(err))
		return nil
	}

	log.Info(ctx, "sdh_filter_completed", "SDH content filtered")
	return nil
}

// IsCleanitAvailable is a package-level check for startup logging.
func IsCleanitAvailable() bool {
	return isCleanitAvailableFunc()
}
