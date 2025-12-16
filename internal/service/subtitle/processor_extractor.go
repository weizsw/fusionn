package subtitle

import (
	"context"
	"fmt"

	"github.com/fusionn/pkg/logger"
)

// ExtractorProcessor extracts subtitle tracks to SRT files.
type ExtractorProcessor struct {
	analyzer *Analyzer
}

// NewExtractorProcessor creates a new extractor processor.
func NewExtractorProcessor(analyzer *Analyzer) *ExtractorProcessor {
	return &ExtractorProcessor{
		analyzer: analyzer,
	}
}

// Name returns the processor name.
func (p *ExtractorProcessor) Name() string {
	return "ExtractorProcessor"
}

// ShouldRun runs if subtitles were detected.
func (p *ExtractorProcessor) ShouldRun(pctx *ProcessingContext) bool {
	return pctx.Analysis != nil &&
		(pctx.Analysis.EnglishTrack != nil || pctx.Analysis.ChineseTrack != nil)
}

// Process extracts subtitle tracks to temporary SRT files.
func (p *ExtractorProcessor) Process(ctx context.Context, pctx *ProcessingContext) error {
	logger.Infof("Extracting subtitle tracks from: %s", pctx.VideoPath)

	// Extract subtitles using analyzer
	if err := p.analyzer.ExtractSubtitles(ctx, pctx.Analysis); err != nil {
		return fmt.Errorf("failed to extract subtitles: %w", err)
	}

	// Update processing context with extracted paths
	if pctx.Analysis.EnglishTrack != nil {
		pctx.EnglishSubPath = pctx.Analysis.EnglishTrack.ExtractedPath
		logger.Infof("✅ Extracted English subtitle: %s", pctx.EnglishSubPath)
	}

	if pctx.Analysis.ChineseTrack != nil {
		pctx.ChineseSubPath = pctx.Analysis.ChineseTrack.ExtractedPath
		pctx.NeedsConversion = pctx.Analysis.ChineseTrack.NeedsConversion
		logger.Infof("✅ Extracted Chinese subtitle: %s (NeedsConversion: %v)",
			pctx.ChineseSubPath, pctx.NeedsConversion)
	}

	// Mark translation needed if Chinese missing but English found
	if pctx.Analysis.EnglishTrack != nil && pctx.Analysis.ChineseTrack == nil {
		pctx.NeedsTranslation = true
		logger.Infof("⚠️  Chinese subtitle missing, translation will be queued")
	}

	return nil
}
