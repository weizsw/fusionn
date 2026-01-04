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

	// Build DuoSubs config
	duosubsCfg := executor.DuoSubsConfig{
		Model:   p.duosubsConfig.Model,
		Device:  p.duosubsConfig.Device,
		Timeout: time.Duration(p.duosubsConfig.TimeoutMinutes) * time.Minute,
	}

	// Run DuoSubs - it will extract and return the path to the combined ASS file
	mergedPath, err := executor.MergeDuoSubs(ctx, pctx.ChineseSubPath, pctx.EnglishSubPath, outputDir, duosubsCfg)
	if err != nil {
		return fmt.Errorf("duosubs merge failed: %w", err)
	}

	// Update context with merged subtitle path
	pctx.MergedSubPath = mergedPath

	logger.Infof("✅ Merge completed: %s", mergedPath)
	return nil
}
