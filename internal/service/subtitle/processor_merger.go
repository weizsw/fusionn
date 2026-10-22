package subtitle

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/fusionn/internal/config"
	"github.com/fusionn/internal/executor"
	"github.com/fusionn/internal/subtitlellm"
	"github.com/fusionn/pkg/logger"
)

const (
	// llmMergedFileName is the renderer output inside the isolated merge directory.
	llmMergedFileName = "bilingual.ass"
	// llmArtifactDirName groups persisted alignment diagnostics under the
	// persistent cache dir or the isolated merge dir.
	llmArtifactDirName = "llm-alignment"
)

// Metadata keys published by the LLM merger. They complement
// subtitlellm.MetadataLLMStyled (the style processor's skip flag) and let
// diagnostics locate the persisted alignment artifacts.
const (
	MetadataLLMMappingPath = "llm_mapping_path"
	MetadataLLMAuditPath   = "llm_audit_path"
	MetadataLLMSourcesPath = "llm_sources_path"

	MetadataLLMDegraded       = "llm_degraded"
	MetadataLLMFailedWindows  = "llm_failed_windows"
	MetadataLLMWindowFailures = "llm_window_failures"
)

// MergerProcessor merges English and Chinese subtitles with the configured
// engine: the legacy DuoSubs aligner or the native ID-mapping LLM renderer.
type MergerProcessor struct {
	engine          string
	duosubsExecutor *executor.DuoSubsConfig
	llmEngine       *subtitlellm.Engine
	llmConfig       config.LLMConfig
	styleConfig     config.ASSStyleConfig
	openccConfig    config.OpenCCConfig
	convertOpenCC   func(ctx context.Context, inputPath, outputPath, configName string) error
	probe           func(context.Context, string) (*executor.FFProbeOutput, error)
}

// NewMergerProcessor creates a legacy DuoSubs merger processor.
func NewMergerProcessor(cfg config.DuoSubsConfig) (*MergerProcessor, error) {
	return NewConfiguredMergerProcessor(config.SubtitleConfig{
		Engine:  config.EngineDuoSubs,
		DuoSubs: cfg,
	})
}

// NewConfiguredMergerProcessor creates the merger selected by
// subtitle.engine. Only the selected engine's dependency is initialized: a
// selected LLM engine never constructs the DuoSubs executor, and a selected
// DuoSubs engine never constructs the LLM engine.
func NewConfiguredMergerProcessor(cfg config.SubtitleConfig) (*MergerProcessor, error) {
	engine := strings.ToLower(strings.TrimSpace(cfg.Engine))
	if engine == "" {
		// Directly constructed configs without a resolved engine keep the
		// legacy DuoSubs behavior; config.Load resolves new files to llm.
		engine = config.EngineDuoSubs
	}

	switch engine {
	case config.EngineDuoSubs:
		execCfg := executor.DuoSubsConfig{
			Mode:                executor.DuoSubsMode(cfg.DuoSubs.Mode),
			Model:               cfg.DuoSubs.Model,
			Device:              cfg.DuoSubs.Device,
			Timeout:             time.Duration(cfg.DuoSubs.TimeoutMinutes) * time.Minute,
			HTTPURL:             cfg.DuoSubs.HTTPURL,
			HTTPContainerPrefix: cfg.DuoSubs.HTTPContainerPrefix,
			HTTPHostPrefix:      cfg.DuoSubs.HTTPHostPrefix,
		}

		duosubsExec, err := executor.NewDuoSubsExecutor(execCfg)
		if err != nil {
			return nil, fmt.Errorf("failed to initialize duosubs executor: %w", err)
		}
		return &MergerProcessor{
			engine:          config.EngineDuoSubs,
			duosubsExecutor: duosubsExec,
		}, nil

	case config.EngineLLM:
		return &MergerProcessor{
			engine:        config.EngineLLM,
			llmEngine:     subtitlellm.NewEngine(),
			llmConfig:     cfg.LLM,
			styleConfig:   cfg.ASSStyle,
			openccConfig:  cfg.OpenCC,
			convertOpenCC: executor.ConvertOpenCC,
			probe:         executor.FFProbe,
		}, nil

	default:
		return nil, fmt.Errorf("unsupported subtitle merge engine %q", cfg.Engine)
	}
}

// Name returns the processor name.
func (p *MergerProcessor) Name() string {
	return ProcessorNameMerger
}

// ShouldRun determines if merging should run.
func (p *MergerProcessor) ShouldRun(pctx *ProcessingContext) bool {
	return pctx.MergedSubPath == "" && pctx.EnglishSubPath != "" && pctx.ChineseSubPath != ""
}

// Process merges English and Chinese subtitles with the selected engine.
func (p *MergerProcessor) Process(ctx context.Context, pctx *ProcessingContext) error {
	if p.engine == config.EngineLLM {
		return p.processLLM(ctx, pctx)
	}
	return p.processDuoSubs(ctx, pctx)
}

// processDuoSubs keeps the legacy DuoSubs pipeline unchanged.
func (p *MergerProcessor) processDuoSubs(ctx context.Context, pctx *ProcessingContext) error {
	log := logger.Indent()
	log.Info("Merging English + Chinese subtitles with DuoSubs")

	outputDir, err := createMergeDir(pctx.VideoPath)
	if err != nil {
		return err
	}
	pctx.TempMergeDir = outputDir

	// Run DuoSubs - it will extract and return the path to the combined ASS file
	mergedPath, err := executor.MergeDuoSubs(ctx, pctx.ChineseSubPath, pctx.EnglishSubPath, outputDir, *p.duosubsExecutor)
	if err != nil {
		return fmt.Errorf("duosubs merge failed: %w", err)
	}

	pctx.MergedSubPath = mergedPath

	log.Infof("✅ Merge completed: %s", mergedPath)
	return nil
}

// processLLM loads the immutable source bundle, aligns it by ID, renders the
// bilingual ASS locally, and persists the audit trail. Subtitle text never
// comes from model output.
func (p *MergerProcessor) processLLM(ctx context.Context, pctx *ProcessingContext) error {
	log := logger.Indent()
	log.Info("Merging English + Chinese subtitles with the native LLM engine")

	if err := ctx.Err(); err != nil {
		return err
	}

	outputDir, err := createMergeDir(pctx.VideoPath)
	if err != nil {
		return err
	}
	pctx.TempMergeDir = outputDir

	// Load the source bundle before alignment. A Traditional Chinese track is
	// converted on a private copy first so the immutable bundle, the mapping
	// fingerprint and the cache key all describe the exact text being rendered.
	enPath, zhPath := pctx.EnglishSubPath, pctx.ChineseSubPath
	if pctx.NeedsConversion {
		convertedZh, err := p.convertChineseFile(ctx, enPath, zhPath, outputDir)
		if err != nil {
			return err
		}
		if convertedZh != "" {
			zhPath = convertedZh
		}
	}
	bundle, err := subtitlellm.LoadSources(enPath, zhPath)
	if err != nil {
		return fmt.Errorf("load subtitle sources: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	// Alignment, cache keys and rendering all describe the same bundle. A
	// precomputed MappingPath is verified against this text, so an imported
	// mapping for a different (for example unconverted) track is rejected
	// rather than silently mixed with converted Chinese text.
	mapping, audit, alignErr := p.llmEngine.Align(ctx, bundle, p.llmConfig)
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := p.persistLLMArtifacts(pctx, outputDir, bundle, mapping, audit); err != nil {
		return fmt.Errorf("persist alignment artifacts: %w", err)
	}
	p.logLLMAudit(audit)

	if alignErr != nil {
		if errors.Is(alignErr, context.Canceled) || errors.Is(alignErr, context.DeadlineExceeded) {
			return alignErr
		}
		return fmt.Errorf("llm alignment failed: %w", alignErr)
	}
	if len(audit.FailedWindows) > 0 {
		log.Warnf("LLM alignment partially failed (%d windows); fell back to native timing and cue order for failed windows", len(audit.FailedWindows))
		if pctx.Metadata != nil {
			pctx.Metadata[MetadataLLMDegraded] = true
			pctx.Metadata[MetadataLLMFailedWindows] = audit.FailedWindows
			if len(audit.WindowFailures) > 0 {
				pctx.Metadata[MetadataLLMWindowFailures] = audit.WindowFailures
			}
		}
	}
	// Context cancellation can race a fully cached alignment; never render
	// after the caller has cancelled the job.
	if err := ctx.Err(); err != nil {
		return err
	}

	width, height := p.videoDimensions(ctx, pctx.VideoPath)
	if err := ctx.Err(); err != nil {
		return err
	}

	words, wordErr := subtitlellm.AlignWords(ctx, pctx.VideoPath, bundle, p.llmConfig)
	if err := ctx.Err(); err != nil {
		return err
	}
	if wordErr != nil {
		log.Warnf("Word alignment stopped; remaining cues use proportional timing: %v", wordErr)
	}
	if len(words) > 0 {
		log.Infof("Received word timestamps for %d long English cues", len(words))
	}
	rendered, err := subtitlellm.RenderWithWordTimings(bundle, mapping, p.llmConfig, p.styleConfig, width, height, words)
	if err != nil {
		return fmt.Errorf("render bilingual subtitles: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	mergedPath := filepath.Join(outputDir, llmMergedFileName)
	if err := os.WriteFile(mergedPath, rendered, 0644); err != nil {
		return fmt.Errorf("write merged subtitles: %w", err)
	}

	if pctx.Metadata == nil {
		pctx.Metadata = make(map[string]interface{})
	}
	// The renderer already applied the final styles, PlayRes and explicit
	// positions; prevent the style processor from rewriting them.
	pctx.Metadata[subtitlellm.MetadataLLMStyled] = true
	pctx.MergedSubPath = mergedPath

	log.Infof("✅ Merge completed: %s", mergedPath)
	return nil
}

// createMergeDir creates the isolated temp directory used by both engines.
func createMergeDir(videoPath string) (string, error) {
	videoDir := filepath.Dir(videoPath)
	outputDir := filepath.Join(videoDir, fmt.Sprintf(".fusionn-merge-%s", uuid.New().String()))
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create output directory: %w", err)
	}
	return outputDir, nil
}

// convertChineseFile converts a Traditional Chinese subtitle on a private copy
// inside the merge directory and returns its path. OpenCC substitutes text
// line by line, so native ASS alignment/position metadata is preserved; the
// source file is untouched. An empty path means conversion was disabled or
// failed and the caller should keep the original track.
func (p *MergerProcessor) convertChineseFile(ctx context.Context, enPath, zhPath, outputDir string) (string, error) {
	log := logger.Indent()
	if !p.openccConfig.Enabled {
		log.Warn("Traditional Chinese conversion requested but OpenCC is disabled; using original Chinese text")
		return "", nil
	}
	if p.convertOpenCC == nil {
		return "", errors.New("opencc converter is not configured")
	}

	configName := p.openccConfig.Config
	if configName == "" {
		configName = "t2s.json"
	}
	outputPath := filepath.Join(outputDir, "zh.converted"+filepath.Ext(zhPath))
	if err := p.convertOpenCC(ctx, zhPath, outputPath, configName); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		log.Warnf("OpenCC conversion failed, using original Chinese text: %v", err)
		return "", nil
	}
	// The converted file must still parse as a matching source track; otherwise
	// discard it rather than changing the mapping.
	if _, err := subtitlellm.LoadSources(enPath, outputPath); err != nil {
		log.Warnf("Failed to parse converted Chinese subtitles, using original text: %v", err)
		return "", nil
	}
	log.Infof("Applied OpenCC Traditional → Simplified conversion: %s", outputPath)
	return outputPath, nil
}

// videoDimensions probes the video for renderer geometry. Failures fall back
// to unknown dimensions, which makes the renderer use the configured style
// unchanged. Cancellation is never treated as a fallback.
func (p *MergerProcessor) videoDimensions(ctx context.Context, videoPath string) (int, int) {
	if videoPath == "" || p.probe == nil {
		return 0, 0
	}
	probeOutput, err := p.probe(ctx, videoPath)
	if err != nil {
		if ctx.Err() == nil {
			logger.Warnf("Failed to detect video resolution, using configured ASS style: %v", err)
		}
		return 0, 0
	}
	if width, height, ok := videoDimensions(probeOutput); ok {
		return width, height
	}
	logger.Warn("Failed to detect video resolution, using configured ASS style")
	return 0, 0
}

// persistLLMArtifacts writes sources, mapping and audit JSON without any
// credential fields. Artifacts are only persisted when the alignment cache is
// enabled; with the cache disabled the merger works request-only and reuses
// the existing subtitlellm.MetadataLLMStyled flag in the context.
//
// The layout mirrors the prototype (<root>/sources.json +
// <root>/alignment/mapping.json) so a persisted mapping can be re-imported
// directly through subtitle.llm.mapping_path. A configured cache_dir is the
// persistent, merge-isolation-safe location; without one the diagnostics go to
// the isolated merge directory for this run.
func (p *MergerProcessor) persistLLMArtifacts(pctx *ProcessingContext, outputDir string, bundle subtitlellm.Bundle, mapping subtitlellm.Mapping, audit subtitlellm.Audit) error {
	if !p.llmConfig.CacheEnabled || mapping.Fingerprint == "" {
		return nil
	}
	log := logger.Indent()

	artifactRoot := filepath.Join(outputDir, llmArtifactDirName, artifactID(bundle, mapping))
	if strings.TrimSpace(p.llmConfig.CacheDir) != "" {
		artifactRoot = filepath.Join(p.llmConfig.CacheDir, llmArtifactDirName, artifactID(bundle, mapping))
	}
	alignmentDir := filepath.Join(artifactRoot, "alignment")

	paths := []struct {
		name  string
		path  string
		value any
		key   string
	}{
		{"sources", filepath.Join(artifactRoot, "sources.json"), bundle, MetadataLLMSourcesPath},
		{"mapping", filepath.Join(alignmentDir, "mapping.json"), mapping, MetadataLLMMappingPath},
		{"audit", filepath.Join(alignmentDir, "audit.json"), audit, MetadataLLMAuditPath},
	}
	for _, artifact := range paths {
		if err := writeJSONFile(artifact.path, artifact.value); err != nil {
			return fmt.Errorf("save LLM %s artifact %s: %w", artifact.name, artifact.path, err)
		}
		if pctx.Metadata == nil {
			pctx.Metadata = make(map[string]interface{})
		}
		pctx.Metadata[artifact.key] = artifact.path
	}
	log.Infof("Saved LLM alignment artifacts to %s", artifactRoot)
	return nil
}

// artifactID names one artifact directory. The suffix is deterministic from
// the mapping fingerprint so reruns replace diagnostics in place instead of
// accumulating directories; the bundle prefix keeps it recognizable.
func artifactID(bundle subtitlellm.Bundle, mapping subtitlellm.Mapping) string {
	suffix := mapping.Fingerprint
	if len(suffix) > 16 {
		suffix = suffix[:16]
	}
	prefix := bundle.Fingerprint
	if len(prefix) > 12 {
		prefix = prefix[:12]
	}
	return prefix + "-" + suffix
}

// writeJSONFile atomically writes indented JSON.
func writeJSONFile(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	temp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if _, err := temp.Write(append(data, '\n')); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempName, path)
}

// logLLMAudit reports deterministic counts and provenance only. Rejected model
// rows are never logged, and no credentials exist in the audit.
func (p *MergerProcessor) logLLMAudit(audit subtitlellm.Audit) {
	log := logger.Indent()
	log.Infof("LLM alignment audit: rejected=%d conflicts=%d restored_en=%d restored_zh=%d context_duplicates=%d failed_windows=%d cache_hits=%d",
		len(audit.Rejected), len(audit.Conflicts), len(audit.Restored["en"]), len(audit.Restored["zh"]),
		audit.ContextDuplicates, len(audit.FailedWindows), audit.CacheHits)
	if len(audit.FailedWindows) > 0 {
		log.Warnf("LLM alignment degraded: native timing fallback used for %d windows", len(audit.FailedWindows))
		for _, f := range audit.WindowFailures {
			log.Warnf("LLM window %d failure reason: %s", f.Window, f.Reason)
		}
	}
	if audit.Provenance != nil {
		log.Infof("LLM mapping provenance: source=%s mapping_path=%s mapping_model=%s configured_model=%s model_mismatch=%v",
			audit.Provenance.Source, audit.Provenance.MappingPath, audit.Provenance.MappingModel,
			audit.Provenance.ConfiguredModel, audit.Provenance.ModelMismatch)
	}
}
