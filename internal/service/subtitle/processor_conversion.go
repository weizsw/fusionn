package subtitle

import (
	"context"
	"fmt"

	"github.com/fusionn/internal/executor"
	"github.com/fusionn/pkg/logger"
)

// ConversionProcessor converts Traditional Chinese to Simplified Chinese.
type ConversionProcessor struct {
	openccEnabled bool
	openccConfig  string
}

// NewConversionProcessor creates a new conversion processor.
func NewConversionProcessor(enabled bool, config string) *ConversionProcessor {
	return &ConversionProcessor{
		openccEnabled: enabled,
		openccConfig:  config,
	}
}

// Name returns the processor name.
func (p *ConversionProcessor) Name() string {
	return "ConversionProcessor"
}

// ShouldRun runs if Chinese subtitle needs conversion (Traditional → Simplified).
func (p *ConversionProcessor) ShouldRun(pctx *ProcessingContext) bool {
	return p.openccEnabled && pctx.NeedsConversion && pctx.ChineseSubPath != ""
}

// Process converts Traditional Chinese subtitle to Simplified Chinese.
func (p *ConversionProcessor) Process(ctx context.Context, pctx *ProcessingContext) error {
	logger.Infof("Converting Traditional Chinese → Simplified: %s", pctx.ChineseSubPath)

	convertedPath, err := executor.ConvertChineseSubtitle(
		ctx,
		pctx.ChineseSubPath,
		p.openccConfig,
	)
	if err != nil {
		return fmt.Errorf("failed to convert Chinese subtitle: %w", err)
	}

	pctx.ChineseSubPath = convertedPath
	pctx.NeedsConversion = false // Conversion complete

	logger.Infof("✅ Chinese subtitle converted: %s", convertedPath)
	return nil
}
