package subtitle

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/fusionn/internal/config"
	"github.com/fusionn/internal/executor"
	"github.com/fusionn/pkg/logger"
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
	}

	return nil
}

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
	return "Conversion"
}

// ShouldRun determines if conversion should run.
func (p *ConversionProcessor) ShouldRun(pctx *ProcessingContext) bool {
	// Run if OpenCC is enabled and Chinese subtitle needs conversion
	return p.openccConfig.Enabled &&
		pctx.Analysis != nil &&
		pctx.Analysis.ChineseTrack != nil &&
		pctx.Analysis.ChineseTrack.NeedsConversion
}

// Process converts Traditional to Simplified Chinese.
func (p *ConversionProcessor) Process(ctx context.Context, pctx *ProcessingContext) error {
	if pctx.ChineseSubPath == "" {
		return fmt.Errorf("chinese subtitle path not set")
	}

	logger.Info("Converting Traditional Chinese → Simplified Chinese")

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
		logger.Warnf("Failed to remove original Traditional subtitle: %v", err)
	}

	pctx.ChineseSubPath = convertedPath
	pctx.NeedsConversion = false

	logger.Info("✅ Conversion completed")
	return nil
}

// CleanupProcessor removes temporary files.
type CleanupProcessor struct{}

// NewCleanupProcessor creates a new cleanup processor.
func NewCleanupProcessor() *CleanupProcessor {
	return &CleanupProcessor{}
}

// Name returns the processor name.
func (p *CleanupProcessor) Name() string {
	return "Cleanup"
}

// ShouldRun determines if cleanup should run.
func (p *CleanupProcessor) ShouldRun(pctx *ProcessingContext) bool {
	// Always run cleanup
	return true
}

// Process removes temporary files.
func (p *CleanupProcessor) Process(ctx context.Context, pctx *ProcessingContext) error {
	logger.Info("Cleaning up temporary files")

	// Clean up extracted subtitles
	if pctx.EnglishSubPath != "" {
		if err := os.Remove(pctx.EnglishSubPath); err != nil && !os.IsNotExist(err) {
			logger.Warnf("Failed to remove %s: %v", pctx.EnglishSubPath, err)
		}
	}

	if pctx.ChineseSubPath != "" {
		if err := os.Remove(pctx.ChineseSubPath); err != nil && !os.IsNotExist(err) {
			logger.Warnf("Failed to remove %s: %v", pctx.ChineseSubPath, err)
		}
	}

	logger.Info("✅ Cleanup completed")
	return nil
}

// OutputProcessor copies the merged subtitle to the final destination.
type OutputProcessor struct {
	outputSameDir bool
}

// NewOutputProcessor creates a new output processor.
func NewOutputProcessor(outputSameDir bool) *OutputProcessor {
	return &OutputProcessor{
		outputSameDir: outputSameDir,
	}
}

// Name returns the processor name.
func (p *OutputProcessor) Name() string {
	return "Output"
}

// ShouldRun determines if output should run.
func (p *OutputProcessor) ShouldRun(pctx *ProcessingContext) bool {
	// Run if we have a merged subtitle
	return pctx.MergedSubPath != ""
}

// Process copies merged subtitle to final destination.
func (p *OutputProcessor) Process(ctx context.Context, pctx *ProcessingContext) error {
	if pctx.MergedSubPath == "" {
		return fmt.Errorf("merged subtitle path not set")
	}

	// Determine output path
	videoDir := filepath.Dir(pctx.VideoPath)
	videoBase := filepath.Base(pctx.VideoPath)
	videoExt := filepath.Ext(videoBase)
	videoName := videoBase[:len(videoBase)-len(videoExt)]

	finalPath := filepath.Join(videoDir, videoName+".ass")

	// If not outputting to same directory, use a configured output directory
	// For now, we always output to the same directory
	if !p.outputSameDir {
		logger.Warn("Output to separate directory not yet implemented, using video directory")
	}

	logger.Infof("Copying merged subtitle: %s → %s", pctx.MergedSubPath, finalPath)

	// Copy file
	if err := copyFile(pctx.MergedSubPath, finalPath); err != nil {
		return fmt.Errorf("failed to copy merged subtitle: %w", err)
	}

	// Update context with final path
	pctx.MergedSubPath = finalPath

	logger.Infof("✅ Merged subtitle saved: %s", finalPath)
	return nil
}

// Helper function to copy files
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("failed to read source: %w", err)
	}

	if err := os.WriteFile(dst, data, 0644); err != nil {
		return fmt.Errorf("failed to write destination: %w", err)
	}

	return nil
}

