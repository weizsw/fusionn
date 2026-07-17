package subtitle

import (
	"context"
	"fmt"
	"math"
	"os"
	"strings"

	"github.com/fusionn/internal/config"
	"github.com/fusionn/internal/executor"
	"github.com/fusionn/pkg/logger"
)

// StyleProcessor injects custom ASS styles into merged subtitle files.
type StyleProcessor struct {
	config config.ASSStyleConfig
	probe  func(context.Context, string) (*executor.FFProbeOutput, error)
}

// NewStyleProcessor creates a new style processor.
func NewStyleProcessor(cfg config.ASSStyleConfig) *StyleProcessor {
	return &StyleProcessor{
		config: cfg,
		probe:  executor.FFProbe,
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

	styleConfig := p.config
	if pctx.VideoPath != "" {
		probeOutput, probeErr := p.probe(ctx, pctx.VideoPath)
		if probeErr != nil {
			log.Warnf("Failed to detect video resolution, using configured ASS style: %v", probeErr)
		} else if width, height, ok := videoDimensions(probeOutput); ok {
			scale := referencePlaybackCanvasScale(width, height)
			styleConfig.PrimarySize = int(math.Round(float64(styleConfig.PrimarySize) * scale))
			styleConfig.SecondarySize = int(math.Round(float64(styleConfig.SecondarySize) * scale))
			styleConfig.Outline *= scale
			styleConfig.Shadow *= scale
			styleConfig.MarginV = adjustedMarginV(styleConfig.MarginV, width, height)
		} else {
			log.Warn("Failed to detect video resolution, using configured ASS style")
		}
	}

	// Generate custom styles
	scriptInfo := GetScriptInfo(p.config.WrapStyle)
	styles := GenerateStyles(styleConfig)

	// Reconstruct ASS file
	newContent := scriptInfo + styles + eventsSection

	// Write back
	if err := os.WriteFile(pctx.MergedSubPath, []byte(newContent), 0644); err != nil {
		return fmt.Errorf("failed to write styled ASS: %w", err)
	}

	log.Infof("✅ Custom styles injected: %s", pctx.MergedSubPath)
	return nil
}

func videoDimensions(probeOutput *executor.FFProbeOutput) (int, int, bool) {
	for _, stream := range probeOutput.Streams {
		if stream.CodecType == "video" &&
			stream.Disposition["attached_pic"] != 1 &&
			stream.Width > 0 && stream.Height > 0 {
			return stream.Width, stream.Height, true
		}
	}
	return 0, 0, false
}

func referencePlaybackCanvasScale(width, height int) float64 {
	canvasHeight := float64(width) * 9 / 16
	if float64(height) >= canvasHeight {
		return 1
	}
	return canvasHeight / float64(height)
}

func adjustedMarginV(configuredMarginV, width, height int) int {
	// Keep the style margin at the same position on a centered 16:9 output canvas.
	scale := referencePlaybackCanvasScale(width, height)
	if scale == 1 {
		return configuredMarginV
	}

	canvasHeight := float64(height) * scale
	bottomBar := (canvasHeight - float64(height)) / 2
	verticalScale := float64(height) / assPlayResY
	targetBottomGap := float64(configuredMarginV) * canvasHeight / assPlayResY

	return int(math.Round((targetBottomGap - bottomBar) / verticalScale))
}

// extractEventsSection extracts the [Events] section and everything after it.
func extractEventsSection(content string) (string, error) {
	eventsIndex := strings.Index(content, "[Events]")
	if eventsIndex == -1 {
		return "", fmt.Errorf("invalid ASS file: missing [Events] section")
	}

	return content[eventsIndex:], nil
}
