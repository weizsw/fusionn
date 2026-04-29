package subtitle

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/fusionn/internal/config"
	"github.com/fusionn/pkg/logger"
)

// StyleProcessor injects custom ASS styles into merged subtitle files.
type StyleProcessor struct {
	config config.ASSStyleConfig
}

// NewStyleProcessor creates a new style processor.
func NewStyleProcessor(cfg config.ASSStyleConfig) *StyleProcessor {
	return &StyleProcessor{
		config: cfg,
	}
}

// Name returns the processor name.
func (p *StyleProcessor) Name() string {
	return ProcessorNameStyle
}

// ShouldRun runs if ASS styling is enabled and we have a merged subtitle.
func (p *StyleProcessor) ShouldRun(pctx *ProcessingContext) bool {
	return p.config.Enabled && pctx.MergedSubPath != ""
}

// Process injects custom styles into the ASS file.
func (p *StyleProcessor) Process(ctx context.Context, pctx *ProcessingContext) error {
	log := logger.Indent()
	log.Infof("Injecting custom ASS styles: %s", pctx.MergedSubPath)

	// Read ASS file
	content, err := os.ReadFile(pctx.MergedSubPath)
	if err != nil {
		return fmt.Errorf("failed to read ASS file: %w", err)
	}

	// Parse ASS into sections
	eventsSection, err := extractEventsSection(string(content))
	if err != nil {
		log.Warnf("Failed to parse ASS file, skipping styling: %v", err)
		return nil // Don't fail the job, just skip styling
	}

	// Generate custom styles
	scriptInfo := GetScriptInfo(p.config.WrapStyle)
	styles := GenerateStyles(p.config)

	// Reconstruct ASS file
	newContent := scriptInfo + styles + eventsSection

	// Write back
	if err := os.WriteFile(pctx.MergedSubPath, []byte(newContent), 0644); err != nil {
		return fmt.Errorf("failed to write styled ASS: %w", err)
	}

	log.Infof("✅ Custom styles injected: %s", pctx.MergedSubPath)
	return nil
}

// extractEventsSection extracts the [Events] section and everything after it.
func extractEventsSection(content string) (string, error) {
	eventsIndex := strings.Index(content, "[Events]")
	if eventsIndex == -1 {
		return "", fmt.Errorf("invalid ASS file: missing [Events] section")
	}

	return content[eventsIndex:], nil
}
