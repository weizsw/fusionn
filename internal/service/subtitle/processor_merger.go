package subtitle

import (
	"context"
	"fmt"

	"github.com/fusionn/internal/executor"
	"github.com/fusionn/pkg/logger"
)

// MergerProcessor merges English and Chinese subtitles using DuoSubs.
type MergerProcessor struct {
	model          string
	device         string
	timeoutMinutes int
}

// NewMergerProcessor creates a new merger processor.
func NewMergerProcessor(model, device string, timeoutMinutes int) *MergerProcessor {
	return &MergerProcessor{
		model:          model,
		device:         device,
		timeoutMinutes: timeoutMinutes,
	}
}

// Name returns the processor name.
func (p *MergerProcessor) Name() string {
	return "MergerProcessor"
}

// ShouldRun runs if both English and Chinese subtitles are available.
func (p *MergerProcessor) ShouldRun(pctx *ProcessingContext) bool {
	return pctx.EnglishSubPath != "" && pctx.ChineseSubPath != ""
}

// Process merges English and Chinese subtitles using DuoSubs.
func (p *MergerProcessor) Process(ctx context.Context, pctx *ProcessingContext) error {
	logger.Infof("Merging subtitles: Chinese (primary) + English (secondary)")
	logger.Debugf("English: %s", pctx.EnglishSubPath)
	logger.Debugf("Chinese: %s", pctx.ChineseSubPath)

	mergedPath, err := executor.MergeSubtitles(
		ctx,
		pctx.ChineseSubPath, // Primary
		pctx.EnglishSubPath, // Secondary
		p.model,
		p.device,
		p.timeoutMinutes,
	)
	if err != nil {
		return fmt.Errorf("failed to merge subtitles: %w", err)
	}

	pctx.MergedSubPath = mergedPath

	logger.Infof("✅ Subtitles merged successfully: %s", mergedPath)
	return nil
}
