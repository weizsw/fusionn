package subtitle

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"

	"github.com/fusionn/internal/config"
	"github.com/fusionn/internal/executor"
	"github.com/fusionn/pkg/logger"
)

// MergerProcessor merges English and Chinese subtitles using DuoSubs.
type MergerProcessor struct {
	duosubsConfig config.DuoSubsConfig
}

// NewMergerProcessor creates a new merger processor.
func NewMergerProcessor(cfg config.DuoSubsConfig) *MergerProcessor {
	return &MergerProcessor{
		duosubsConfig: cfg,
	}
}

// Name returns the processor name.
func (p *MergerProcessor) Name() string {
	return "Merger"
}

// ShouldRun determines if merging should run.
func (p *MergerProcessor) ShouldRun(pctx *ProcessingContext) bool {
	// Run if we have both English and Chinese subtitles
	return pctx.EnglishSubPath != "" && pctx.ChineseSubPath != ""
}

// Process merges English and Chinese subtitles.
func (p *MergerProcessor) Process(ctx context.Context, pctx *ProcessingContext) error {
	logger.Info("Merging English + Chinese subtitles with DuoSubs")

	// Create temporary output directory
	tmpDir := os.TempDir()
	outputDir := filepath.Join(tmpDir, fmt.Sprintf("fusionn-merge-%s", uuid.New().String()))
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// DuoSubs expects output directory, not file path
	// It will create output.ass inside the directory
	mergedPath := filepath.Join(outputDir, "output.ass")

	// Build DuoSubs config
	duosubsCfg := executor.DuoSubsConfig{
		Model:   p.duosubsConfig.Model,
		Device:  p.duosubsConfig.Device,
		Timeout: time.Duration(p.duosubsConfig.TimeoutMinutes) * time.Minute,
	}

	// Run DuoSubs
	if err := executor.MergeDuoSubs(ctx, pctx.EnglishSubPath, pctx.ChineseSubPath, outputDir, duosubsCfg); err != nil {
		return fmt.Errorf("duosubs merge failed: %w", err)
	}

	// Verify output file exists
	if _, err := os.Stat(mergedPath); os.IsNotExist(err) {
		return fmt.Errorf("duosubs did not create expected output file: %s", mergedPath)
	}

	// Update context with merged subtitle path
	pctx.MergedSubPath = mergedPath

	logger.Infof("✅ Merge completed: %s", mergedPath)
	return nil
}

