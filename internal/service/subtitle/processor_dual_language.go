package subtitle

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/fusionn/internal/config"
	"github.com/fusionn/internal/executor"
	"github.com/fusionn/pkg/logger"
)

type DualLanguageProcessor struct {
	openccConfig config.OpenCCConfig
}

func NewDualLanguageProcessor(openccCfg config.OpenCCConfig) *DualLanguageProcessor {
	return &DualLanguageProcessor{
		openccConfig: openccCfg,
	}
}

func (p *DualLanguageProcessor) Name() string {
	return ProcessorNameDualLanguage
}

// ShouldRun checks Bazarr files and extracted embedded tracks. Both can
// already be bilingual (for example a "简英双语" track); merging English into
// them would show the English text twice.
func (p *DualLanguageProcessor) ShouldRun(pctx *ProcessingContext) bool {
	return (pctx.ChineseSubSource == ChineseSourceBazarr || pctx.ChineseSubSource == ChineseSourceExtracted) &&
		pctx.ChineseSubPath != "" &&
		pctx.MergedSubPath == ""
}

func (p *DualLanguageProcessor) Process(ctx context.Context, pctx *ProcessingContext) error {
	log := logger.Component("dual_language")

	content, err := os.ReadFile(pctx.ChineseSubPath)
	if err != nil {
		return fmt.Errorf("read subtitle file: %w", err)
	}

	text := string(content)
	ext := strings.ToLower(filepath.Ext(pctx.ChineseSubPath))

	var isDual bool
	switch ext {
	case ".srt":
		isDual = detectDualLanguageSRT(text)
	case ".ass", ".ssa":
		isDual = detectDualLanguageASS(text)
	default:
		log.Debug(ctx, "dual_language_skipped", "Unsupported subtitle format for dual-language detection",
			zap.String("format", ext))
		return nil
	}

	if !isDual {
		log.Debug(ctx, "dual_language_skipped", "Subtitle is not dual-language, proceeding with normal merge")
		return nil
	}

	log.Info(ctx, "dual_language_detected", "Dual-language subtitle detected, converting to styled ASS")

	var assContent string
	switch ext {
	case ".srt":
		assContent, err = convertDualLanguageSRTToASS(text)
	case ".ass", ".ssa":
		assContent, err = remapDualLanguageASS(text)
	}
	if err != nil {
		return fmt.Errorf("convert dual-language subtitle: %w", err)
	}

	if p.openccConfig.Enabled {
		assContent, err = p.convertChineseInASS(ctx, assContent, log)
		if err != nil {
			log.Warn(ctx, "dual_language_conversion_failed", "OpenCC conversion failed, using original text", zap.Error(err))
		}
	}

	videoDir := filepath.Dir(pctx.VideoPath)
	outputDir := filepath.Join(videoDir, fmt.Sprintf(".fusionn-dual-%s", uuid.New().String()))
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	pctx.TempMergeDir = outputDir

	outputPath := filepath.Join(outputDir, "dual_combined.ass")
	if err := os.WriteFile(outputPath, []byte(assContent), 0644); err != nil {
		return fmt.Errorf("write ASS file: %w", err)
	}

	pctx.MergedSubPath = outputPath
	log.Info(ctx, "dual_language_written", "Dual-language ASS written", zap.String("path", outputPath))
	return nil
}

func (p *DualLanguageProcessor) convertChineseInASS(ctx context.Context, assContent string, log logger.Logger) (string, error) {
	log.Debug(ctx, "conversion_started", "Running OpenCC on Chinese text in dual-language ASS")

	lines := strings.Split(assContent, "\n")
	var chineseTexts []string
	var chineseIndices []int

	for i, line := range lines {
		if strings.HasPrefix(line, "Dialogue:") && strings.Contains(line, ",Default,") && !strings.Contains(line, ",Default_1,") {
			parts := strings.SplitN(line, ",", 10)
			if len(parts) >= 10 {
				chineseTexts = append(chineseTexts, parts[9])
				chineseIndices = append(chineseIndices, i)
			}
		}
	}

	if len(chineseTexts) == 0 {
		return assContent, nil
	}

	tmpDir := os.TempDir()
	inputPath := filepath.Join(tmpDir, fmt.Sprintf("fusionn-opencc-input-%s.txt", uuid.New().String()))
	outputPath := filepath.Join(tmpDir, fmt.Sprintf("fusionn-opencc-output-%s.txt", uuid.New().String()))
	defer os.Remove(inputPath)
	defer os.Remove(outputPath)

	if err := os.WriteFile(inputPath, []byte(strings.Join(chineseTexts, "\n")), 0644); err != nil {
		return "", fmt.Errorf("write opencc input: %w", err)
	}

	configName := p.openccConfig.Config
	if configName == "" {
		configName = "t2s.json"
	}

	if err := executor.ConvertOpenCC(ctx, inputPath, outputPath, configName); err != nil {
		return "", fmt.Errorf("opencc conversion: %w", err)
	}

	convertedBytes, err := os.ReadFile(outputPath)
	if err != nil {
		return "", fmt.Errorf("read opencc output: %w", err)
	}

	convertedTexts := strings.Split(string(convertedBytes), "\n")
	if len(convertedTexts) != len(chineseTexts) {
		log.Warn(ctx, "dual_language_conversion_mismatch", "OpenCC output line count mismatch, skipping replacement",
			zap.Int("got", len(convertedTexts)), zap.Int("expected", len(chineseTexts)))
		return assContent, nil
	}

	for j, idx := range chineseIndices {
		parts := strings.SplitN(lines[idx], ",", 10)
		if len(parts) >= 10 {
			parts[9] = convertedTexts[j]
			lines[idx] = strings.Join(parts, ",")
		}
	}

	return strings.Join(lines, "\n"), nil
}
