package subtitle

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"

	"github.com/fusionn/pkg/logger"
)

const (
	ChineseSourceExtracted  = "extracted"
	ChineseSourceBazarr     = "bazarr"
	ChineseSourceTranslated = "translated"
)

const (
	ProcessorNameAnalyzer         = "AnalyzerProcessor"
	ProcessorNameExtractor        = "Extractor"
	ProcessorNameConversion       = "Conversion"
	ProcessorNameMerger           = "Merger"
	ProcessorNameStyle            = "Style"
	ProcessorNameFontEmbedding    = "FontEmbedding"
	ProcessorNameOutput           = "Output"
	ProcessorNameNotification     = "Notification"
	ProcessorNameCleanup          = "Cleanup"
	ProcessorNameSDHFilter        = "SDHFilter"
	ProcessorNameBazarrSDHFilter  = "BazarrSDHFilter"
	ProcessorNameTranslationQueue = "TranslationQueue"
	ProcessorNameBazarrSearch     = "BazarrSearch"
	ProcessorNameDualLanguage     = "DualLanguage"
)

// ProcessingContext holds the state throughout the subtitle processing pipeline.
type ProcessingContext struct {
	// Input
	VideoPath  string
	MediaType  string
	MediaTitle string
	JobID      string

	// Stable media identity for downstream services such as glossary reuse.
	SourceSystem string
	MediaID      string
	ExternalIDs  map[string]string
	Season       int
	Episode      int

	// Sonarr/Radarr IDs for Bazarr API
	SonarrSeriesID  int
	SonarrEpisodeID int
	RadarrID        int

	// Analysis results
	Analysis *AnalysisResult

	// Processing results
	EnglishSubPath string // Path to extracted/processed English subtitle
	ChineseSubPath string // Path to extracted/processed Chinese subtitle
	MergedSubPath  string // Path to final merged subtitle
	TempMergeDir   string // Temp directory created by merger, cleaned up after pipeline

	// Flags
	NeedsConversion  bool   // Traditional → Simplified Chinese conversion needed
	NeedsTranslation bool   // Chinese subtitle missing, needs translation
	ChineseSubSource string // Where the Chinese subtitle came from: "extracted", "bazarr", "translated"

	// Metadata for notifications/logging
	Metadata map[string]interface{}
}

// MediaParams holds the parameters for processing a media file.
type MediaParams struct {
	Path            string
	MediaType       string
	Title           string
	SonarrSeriesID  int
	SonarrEpisodeID int
	RadarrID        int
	SourceSystem    string
	MediaID         string
	ExternalIDs     map[string]string
	Season          int
	Episode         int
}

// Processor represents a single step in the subtitle processing pipeline.
type Processor interface {
	// Name returns the processor name for logging
	Name() string

	// Process executes the processor logic
	Process(ctx context.Context, pctx *ProcessingContext) error

	// ShouldRun determines if this processor should run based on context
	ShouldRun(pctx *ProcessingContext) bool
}

// Pipeline executes a sequence of processors.
type Pipeline struct {
	processors []Processor
}

// NewPipeline creates a new processing pipeline.
func NewPipeline() *Pipeline {
	return &Pipeline{
		processors: make([]Processor, 0),
	}
}

// AddProcessor adds a processor to the pipeline.
func (p *Pipeline) AddProcessor(proc Processor) *Pipeline {
	p.processors = append(p.processors, proc)
	return p
}

// Execute runs all processors in sequence.
func (p *Pipeline) Execute(ctx context.Context, pctx *ProcessingContext) error {
	log := logger.Component("pipeline")
	log.Info(ctx, "pipeline_started", "Pipeline execution started")

	for _, proc := range p.processors {
		// Check if processor should run
		if !proc.ShouldRun(pctx) {
			log.Debug(ctx, "stage_skipped", "Stage skipped", zap.String("stage", proc.Name()))
			continue
		}

		start := time.Now()
		log.Info(ctx, "stage_started", "Stage started", zap.String("stage", proc.Name()))

		// Execute processor; the failing operation is reported once by its
		// boundary (worker/caller), so this layer only adds stage context.
		if err := proc.Process(ctx, pctx); err != nil {
			log.Debug(ctx, "stage_failed", "Stage failed", zap.String("stage", proc.Name()), zap.Error(err))
			return fmt.Errorf("processor %s failed: %w", proc.Name(), err)
		}

		log.Info(ctx, "stage_completed", "Stage completed", zap.String("stage", proc.Name()), logger.DurMs(time.Since(start)))
	}

	log.Info(ctx, "pipeline_completed", "Pipeline execution completed")
	return nil
}

// GetProcessors returns the list of registered processors (for testing/debugging).
func (p *Pipeline) GetProcessors() []Processor {
	return p.processors
}

// Example processors (to be implemented):
//
// 1. AnalyzerProcessor - Detect subtitle tracks
// 2. ExtractorProcessor - Extract subtitles to SRT files
// 3. ConversionProcessor - Traditional → Simplified Chinese (OpenCC)
// 4. MergerProcessor - Merge English + Chinese (DuoSubs)
// 5. StyleProcessor - Modify ASS styles (FUTURE)
// 6. TimingProcessor - Adjust subtitle timing (FUTURE)
// 7. FilterProcessor - Remove ads/credits (FUTURE)
// 8. OutputProcessor - Copy to final destination
// 9. NotificationProcessor - Send Apprise notification
// 10. CleanupProcessor - Remove temporary files
// 11. TranslationQueueProcessor - Queue for translation if Chinese missing
//
// Each processor can be enabled/disabled via configuration and can have its own settings.
