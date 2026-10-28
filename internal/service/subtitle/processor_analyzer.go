package subtitle

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/fusionn/pkg/logger"
)

// AnalyzerProcessor detects subtitle tracks in the video file.
type AnalyzerProcessor struct {
	analyzer *Analyzer
}

// NewAnalyzerProcessor creates a new analyzer processor.
func NewAnalyzerProcessor(analyzer *Analyzer) *AnalyzerProcessor {
	return &AnalyzerProcessor{
		analyzer: analyzer,
	}
}

// Name returns the processor name.
func (p *AnalyzerProcessor) Name() string {
	return ProcessorNameAnalyzer
}

// ShouldRun always runs to detect subtitle tracks.
func (p *AnalyzerProcessor) ShouldRun(pctx *ProcessingContext) bool {
	return true
}

// Process analyzes the video file for subtitle tracks.
func (p *AnalyzerProcessor) Process(ctx context.Context, pctx *ProcessingContext) error {
	log := logger.Component("analyzer")
	log.Info(ctx, "analysis_started", "Analyzing video")
	log.Debug(ctx, "analysis_detail", "Analysis detail", logger.Preview(pctx.VideoPath))

	analysis, err := p.analyzer.AnalyzeVideo(ctx, pctx.VideoPath)
	if err != nil {
		return fmt.Errorf("failed to analyze video: %w", err)
	}

	pctx.Analysis = analysis

	log.Info(ctx, "analysis_completed", "Analysis completed",
		zap.Bool("has_english_track", analysis.EnglishTrack != nil),
		zap.Bool("has_chinese_track", analysis.ChineseTrack != nil))

	return nil
}
