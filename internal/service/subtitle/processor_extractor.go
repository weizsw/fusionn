package subtitle

import (
	"context"
	"fmt"
)

// ExtractorProcessor extracts subtitle tracks to temporary SRT files.
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
	return "Extractor"
}

// ShouldRun determines if extraction should run.
func (p *ExtractorProcessor) ShouldRun(pctx *ProcessingContext) bool {
	// Extract if we have detected tracks
	return pctx.Analysis != nil && (pctx.Analysis.EnglishTrack != nil || pctx.Analysis.ChineseTrack != nil)
}

// Process extracts subtitle tracks.
func (p *ExtractorProcessor) Process(ctx context.Context, pctx *ProcessingContext) error {
	if err := p.analyzer.ExtractSubtitles(ctx, pctx.Analysis); err != nil {
		return fmt.Errorf("failed to extract subtitles: %w", err)
	}

	// Update context with extracted paths
	if pctx.Analysis.EnglishTrack != nil {
		pctx.EnglishSubPath = pctx.Analysis.EnglishTrack.ExtractedPath
	}
	if pctx.Analysis.ChineseTrack != nil {
		pctx.ChineseSubPath = pctx.Analysis.ChineseTrack.ExtractedPath
		pctx.ChineseSubSource = ChineseSourceExtracted
		pctx.NeedsConversion = pctx.Analysis.ChineseTrack.NeedsConversion
	}

	return nil
}
