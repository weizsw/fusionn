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
	duosubsExecutor *executor.DuoSubsConfig
}

// NewMergerProcessor creates a new merger processor.
func NewMergerProcessor(cfg config.DuoSubsConfig) (*MergerProcessor, error) {
	// Convert config to executor config
	execCfg := executor.DuoSubsConfig{
		Mode:                executor.DuoSubsMode(cfg.Mode),
		Model:               cfg.Model,
		Device:              cfg.Device,
		Timeout:             time.Duration(cfg.TimeoutMinutes) * time.Minute,
		HTTPURL:             cfg.HTTPURL,
		HTTPContainerPrefix: cfg.HTTPContainerPrefix,
		HTTPHostPrefix:      cfg.HTTPHostPrefix,
	}

	// Initialize executor (validates config, creates HTTP client if needed)
	duosubsExec, err := executor.NewDuoSubsExecutor(execCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize duosubs executor: %w", err)
	}

	return &MergerProcessor{
		duosubsExecutor: duosubsExec,
	}, nil
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
	log := logger.Indent()
	log.Info("Merging English + Chinese subtitles with DuoSubs")

	// Use the video's directory for temp output (shared volume, accessible from host)
	// This ensures HTTP mode can create files that the container can access
	videoDir := filepath.Dir(pctx.VideoPath)
	outputDir := filepath.Join(videoDir, fmt.Sprintf(".fusionn-merge-%s", uuid.New().String()))
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Run DuoSubs - it will extract and return the path to the combined ASS file
	mergedPath, err := executor.MergeDuoSubs(ctx, pctx.ChineseSubPath, pctx.EnglishSubPath, outputDir, *p.duosubsExecutor)
	if err != nil {
		return fmt.Errorf("duosubs merge failed: %w", err)
	}

	// Update context with merged subtitle path
	pctx.MergedSubPath = mergedPath

	log.Infof("✅ Merge completed: %s", mergedPath)
	return nil
}
