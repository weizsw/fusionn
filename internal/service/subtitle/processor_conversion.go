package subtitle

import (
	"context"
	"fmt"
	"os"

	"go.uber.org/zap"

	"github.com/fusionn/internal/config"
	"github.com/fusionn/internal/executor"
	"github.com/fusionn/pkg/logger"
)

// ConversionProcessor converts Traditional Chinese to Simplified Chinese using OpenCC.
type ConversionProcessor struct {
	openccConfig config.OpenCCConfig
}

// NewConversionProcessor creates a new conversion processor.
func NewConversionProcessor(cfg config.OpenCCConfig) *ConversionProcessor {
	return &ConversionProcessor{
		openccConfig: cfg,
	}
}

// Name returns the processor name.
func (p *ConversionProcessor) Name() string {
	return ProcessorNameConversion
}

// ShouldRun determines if conversion should run.
func (p *ConversionProcessor) ShouldRun(pctx *ProcessingContext) bool {
	// Run if OpenCC is enabled and Chinese subtitle needs conversion
	return p.openccConfig.Enabled &&
		pctx.ChineseSubPath != "" &&
		pctx.NeedsConversion
}

// Process converts Traditional to Simplified Chinese.
func (p *ConversionProcessor) Process(ctx context.Context, pctx *ProcessingContext) error {
	log := logger.Component("conversion")
	if pctx.ChineseSubPath == "" {
		return fmt.Errorf("chinese subtitle path not set")
	}

	log.Info(ctx, "conversion_started", "Converting Traditional Chinese to Simplified Chinese")

	// Create output path
	convertedPath := pctx.ChineseSubPath + ".converted.srt"

	// Get config name, default to t2s.json if empty
	configName := p.openccConfig.Config
	if configName == "" {
		configName = "t2s.json"
	}

	// Run OpenCC conversion
	if err := executor.ConvertOpenCC(ctx, pctx.ChineseSubPath, convertedPath, configName); err != nil {
		return fmt.Errorf("opencc conversion failed: %w", err)
	}

	// Replace Chinese subtitle path with converted version
	// Clean up original
	if err := os.Remove(pctx.ChineseSubPath); err != nil {
		log.Warn(ctx, "cleanup_failed", "Failed to remove original Traditional subtitle",
			zap.String("path", pctx.ChineseSubPath), zap.Error(err))
	}

	pctx.ChineseSubPath = convertedPath
	pctx.NeedsConversion = false

	log.Info(ctx, "conversion_completed", "Conversion completed")
	return nil
}
