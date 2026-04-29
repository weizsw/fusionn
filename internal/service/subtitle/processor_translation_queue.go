package subtitle

import (
	"context"
	"fmt"

	"github.com/fusionn/internal/queue"
	"github.com/fusionn/pkg/logger"
)

// TranslationQueueProcessor queues a job for translation when Chinese subtitle is missing.
type TranslationQueueProcessor struct {
	queueClient QueueClient
}

// QueueClient defines the interface for queuing translation jobs.
type QueueClient interface {
	EnqueueTranslation(ctx context.Context, job *queue.TranslationJob) error
}

// NewTranslationQueueProcessor creates a new translation queue processor.
func NewTranslationQueueProcessor(queueClient QueueClient, _ string) *TranslationQueueProcessor {
	return &TranslationQueueProcessor{
		queueClient: queueClient,
	}
}

// Name returns the processor name.
func (p *TranslationQueueProcessor) Name() string {
	return ProcessorNameTranslationQueue
}

// ShouldRun determines if translation queueing should run.
func (p *TranslationQueueProcessor) ShouldRun(pctx *ProcessingContext) bool {
	return pctx.Analysis != nil &&
		pctx.Analysis.EnglishTrack != nil &&
		pctx.Analysis.ChineseTrack == nil &&
		pctx.ChineseSubPath == ""
}

// Process queues a translation job.
func (p *TranslationQueueProcessor) Process(ctx context.Context, pctx *ProcessingContext) error {
	log := logger.Indent()
	log.Info("Chinese subtitle missing - queuing for translation")

	job := &queue.TranslationJob{
		JobID:        pctx.JobID,
		VideoPath:    pctx.VideoPath,
		SubtitlePath: pctx.EnglishSubPath,
		MediaType:    pctx.MediaType,
		MediaTitle:   pctx.MediaTitle,
		SourceSystem: pctx.SourceSystem,
		MediaID:      pctx.MediaID,
		ExternalIDs:  pctx.ExternalIDs,
		Season:       pctx.Season,
		Episode:      pctx.Episode,
	}

	if err := p.queueClient.EnqueueTranslation(ctx, job); err != nil {
		return fmt.Errorf("failed to enqueue translation: %w", err)
	}

	// Mark that translation is needed
	pctx.NeedsTranslation = true

	log.Infof("✅ Translation job queued: %s", pctx.JobID)
	return nil
}
