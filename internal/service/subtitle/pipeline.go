package subtitle

import (
	"context"
	"fmt"

	"github.com/fusionn/pkg/logger"
)

// ProcessingContext holds the state throughout the subtitle processing pipeline.
type ProcessingContext struct {
	// Input
	VideoPath  string
	MediaType  string // "movie" or "episode"
	MediaTitle string
	JobID      string

	// Analysis results
	Analysis *AnalysisResult

	// Processing results
	EnglishSubPath string // Path to extracted/processed English subtitle
	ChineseSubPath string // Path to extracted/processed Chinese subtitle
	MergedSubPath  string // Path to final merged subtitle

	// Flags
	NeedsConversion  bool // Traditional → Simplified Chinese conversion needed
	NeedsTranslation bool // Chinese subtitle missing, needs translation

	// Metadata for notifications/logging
	Metadata map[string]interface{}
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
	logger.Infof("Starting pipeline execution (job: %s)", pctx.JobID)

	for _, proc := range p.processors {
		// Check if processor should run
		if !proc.ShouldRun(pctx) {
			logger.Debugf("⏭️  Skipping processor: %s (condition not met)", proc.Name())
			continue
		}

		logger.Infof("▶️  Running processor: %s", proc.Name())

		// Execute processor
		if err := proc.Process(ctx, pctx); err != nil {
			return fmt.Errorf("processor %s failed: %w", proc.Name(), err)
		}

		logger.Infof("✅ Processor completed: %s", proc.Name())
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
