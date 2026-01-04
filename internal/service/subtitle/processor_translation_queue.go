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
	callbackURL string
}

// QueueClient defines the interface for queuing translation jobs.
type QueueClient interface {
	EnqueueTranslation(ctx context.Context, job *queue.TranslationJob) error
}

// NewTranslationQueueProcessor creates a new translation queue processor.
func NewTranslationQueueProcessor(queueClient QueueClient, callbackURL string) *TranslationQueueProcessor {
	return &TranslationQueueProcessor{
		queueClient: queueClient,
		callbackURL: callbackURL,
	}
}

// Name returns the processor name.
func (p *TranslationQueueProcessor) Name() string {
	return "TranslationQueue"
}

// ShouldRun determines if translation queueing should run.
func (p *TranslationQueueProcessor) ShouldRun(pctx *ProcessingContext) bool {
	// Queue if we have English but missing Chinese subtitle
	return pctx.Analysis != nil &&
		pctx.Analysis.EnglishTrack != nil &&
		pctx.Analysis.ChineseTrack == nil
}

// Process queues a translation job.
func (p *TranslationQueueProcessor) Process(ctx context.Context, pctx *ProcessingContext) error {
	logger.Info("Chinese subtitle missing - queuing for translation")

	job := &queue.TranslationJob{
		JobID:       pctx.JobID,
		VideoPath:   pctx.VideoPath,
		MediaType:   pctx.MediaType,
		MediaTitle:  pctx.MediaTitle,
		CallbackURL: p.callbackURL,
	}

	if err := p.queueClient.EnqueueTranslation(ctx, job); err != nil {
		return fmt.Errorf("failed to enqueue translation: %w", err)
	}

	// Mark that translation is needed
	pctx.NeedsTranslation = true

	logger.Infof("✅ Translation job queued: %s", pctx.JobID)
	return nil
}
