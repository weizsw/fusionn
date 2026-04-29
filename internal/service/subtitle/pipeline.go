package subtitle

import (
	"context"
	"fmt"

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

// processorEmojis maps processor names to their visual identifiers
var processorEmojis = map[string]string{
	ProcessorNameAnalyzer:         "🔍",
	ProcessorNameExtractor:        "📤",
	ProcessorNameConversion:       "🔄",
	ProcessorNameMerger:           "🔀",
	ProcessorNameStyle:            "🎨",
	ProcessorNameFontEmbedding:    "🔤",
	ProcessorNameOutput:           "💾",
	ProcessorNameNotification:     "📢",
	ProcessorNameCleanup:          "🧹",
	ProcessorNameSDHFilter:        "🔇",
	ProcessorNameBazarrSDHFilter:  "🔇",
	ProcessorNameTranslationQueue: "🌐",
	ProcessorNameBazarrSearch:     "🔎",
	ProcessorNameDualLanguage:     "🗂️",
}

// getProcessorEmoji returns the emoji for a processor, or empty string if not found
func getProcessorEmoji(name string) string {
	if emoji, ok := processorEmojis[name]; ok {
		return emoji + " "
	}
	return ""
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
	logger.Infof("Starting pipeline execution (job: %s)", pctx.JobID)

	for _, proc := range p.processors {
		// Check if processor should run
		if !proc.ShouldRun(pctx) {
			emoji := getProcessorEmoji(proc.Name())
			logger.Debugf("⏭️ Skipping processor: %s%s (condition not met)", emoji, proc.Name())
			continue
		}

		emoji := getProcessorEmoji(proc.Name())
		logger.Infof("▶️ Running processor: %s%s", emoji, proc.Name())

		// Execute processor
		if err := proc.Process(ctx, pctx); err != nil {
			return fmt.Errorf("processor %s failed: %w", proc.Name(), err)
		}

		logger.Infof("✅ Processor completed: %s%s", emoji, proc.Name())
	}

	logger.Infof("Pipeline execution completed (job: %s)", pctx.JobID)
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
