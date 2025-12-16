package subtitle

import (
	"context"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/fusionn/internal/config"
	"github.com/fusionn/internal/notification"
	"github.com/fusionn/internal/queue"
	"github.com/fusionn/pkg/logger"
)

// Service orchestrates subtitle processing using the pipeline architecture.
type Service struct {
	analyzePipeline *Pipeline // Analyze + Extract
	mergePipeline   *Pipeline // Merge + Output + Notify + Cleanup
	config          *config.Config
	mergeQueue      *queue.MergeQueue
	redisClient     *redis.Client
	appriseClient   *notification.AppriseClient
}

// NewService creates a new subtitle service with all processors configured.
func NewService(cfg *config.Config, redisClient *redis.Client, mergeQueue *queue.MergeQueue) *Service {
	// Create analyzer
	analyzer := NewAnalyzer(
		cfg.Subtitle.EnglishVariants,
		cfg.Subtitle.ChineseVariants,
		cfg.Subtitle.SimplifiedKeywords,
		cfg.Subtitle.TraditionalKeywords,
	)

	// Create Apprise client
	var appriseClient *notification.AppriseClient
	if cfg.Apprise.Enabled {
		appriseClient = notification.NewAppriseClient(
			cfg.Apprise.BaseURL,
			cfg.Apprise.Key,
			cfg.Apprise.Tag,
		)
	}

	// Build analyze pipeline (fast, runs synchronously in webhook)
	analyzePipeline := NewPipeline()
	analyzePipeline.AddProcessor(NewAnalyzerProcessor(analyzer))
	analyzePipeline.AddProcessor(NewExtractorProcessor(analyzer))
	analyzePipeline.AddProcessor(NewTranslationQueueProcessor(redisClient, cfg.Redis.QueueKey))

	// Build merge pipeline (slow, runs asynchronously in queue)
	mergePipeline := NewPipeline()
	mergePipeline.AddProcessor(NewConversionProcessor(
		cfg.Subtitle.OpenCC.Enabled,
		cfg.Subtitle.OpenCC.Config,
	))
	mergePipeline.AddProcessor(NewMergerProcessor(
		cfg.Subtitle.DuoSubs.Model,
		cfg.Subtitle.DuoSubs.Device,
		cfg.Subtitle.DuoSubs.TimeoutMinutes,
	))
	mergePipeline.AddProcessor(NewStyleProcessor(cfg.Subtitle.ASSStyle))
	mergePipeline.AddProcessor(NewOutputProcessor(cfg.Subtitle.OutputSameDir))
	mergePipeline.AddProcessor(NewNotificationProcessor(appriseClient, cfg.Apprise.Enabled))
	mergePipeline.AddProcessor(NewCleanupProcessor())

	return &Service{
		analyzePipeline: analyzePipeline,
		mergePipeline:   mergePipeline,
		config:          cfg,
		mergeQueue:      mergeQueue,
		redisClient:     redisClient,
		appriseClient:   appriseClient,
	}
}

// ProcessMedia processes a media file through the subtitle pipeline.
// Phase 1: Analyze + Extract (fast, synchronous)
// Phase 2: Merge (slow, enqueued to worker)
func (s *Service) ProcessMedia(ctx context.Context, videoPath, mediaType, mediaTitle string) error {
	// Generate job ID
	jobID := uuid.New().String()

	// Create processing context
	pctx := &ProcessingContext{
		VideoPath:  videoPath,
		MediaType:  mediaType,
		MediaTitle: mediaTitle,
		JobID:      jobID,
		Metadata:   make(map[string]interface{}),
	}

	logger.Infof("📝 Analyzing subtitles for %s (job: %s)", mediaTitle, jobID)

	// Phase 1: Execute analyze pipeline (fast, synchronous)
	if err := s.analyzePipeline.Execute(ctx, pctx); err != nil {
		logger.Errorf("Analyze pipeline failed (job: %s): %v", jobID, err)
		return err
	}

	// Check if we have both subtitles ready for merge
	if pctx.EnglishSubPath != "" && pctx.ChineseSubPath != "" {
		// Enqueue merge job
		mergeJob := &queue.MergeJob{
			JobID:       jobID,
			VideoPath:   videoPath,
			EnglishPath: pctx.EnglishSubPath,
			ChinesePath: pctx.ChineseSubPath,
			MediaTitle:  mediaTitle,
			MediaType:   mediaType,
		}

		if err := s.mergeQueue.Enqueue(mergeJob); err != nil {
			return err
		}

		logger.Infof("✅ Analysis complete, merge job enqueued (job: %s)", jobID)
	} else {
		// No merge needed (either missing subs or queued for translation)
		logger.Infof("✅ Analysis complete (job: %s)", jobID)
	}

	return nil
}

// ProcessWithSubtitles processes pre-extracted subtitles (from translation callback).
// This enqueues a merge job with the provided subtitle paths.
func (s *Service) ProcessWithSubtitles(ctx context.Context, videoPath, engSubPath, chsSubPath string) error {
	// Generate job ID
	jobID := uuid.New().String()

	// Enqueue merge job
	mergeJob := &queue.MergeJob{
		JobID:       jobID,
		VideoPath:   videoPath,
		EnglishPath: engSubPath,
		ChinesePath: chsSubPath,
		MediaTitle:  videoPath,
		MediaType:   "callback",
	}

	if err := s.mergeQueue.Enqueue(mergeJob); err != nil {
		return err
	}

	logger.Infof("✅ Translation callback merge job enqueued (job: %s)", jobID)
	return nil
}

// ProcessMergeJob processes a merge job from the queue (called by worker).
func (s *Service) ProcessMergeJob(ctx context.Context, job *queue.MergeJob) error {
	// Create processing context
	pctx := &ProcessingContext{
		VideoPath:        job.VideoPath,
		MediaType:        job.MediaType,
		MediaTitle:       job.MediaTitle,
		JobID:            job.JobID,
		EnglishSubPath:   job.EnglishPath,
		ChineseSubPath:   job.ChinesePath,
		NeedsConversion:  false, // Will be determined by conversion processor
		NeedsTranslation: false,
		Metadata:         make(map[string]interface{}),
	}

	logger.Infof("🔧 Processing merge job: %s (%s)", job.JobID, job.MediaTitle)

	// Execute merge pipeline
	if err := s.mergePipeline.Execute(ctx, pctx); err != nil {
		logger.Errorf("Merge pipeline failed (job: %s): %v", job.JobID, err)
		return err
	}

	logger.Infof("✅ Merge job completed: %s", job.JobID)
	return nil
}

// GetAnalyzePipeline returns the analyze pipeline (for testing).
func (s *Service) GetAnalyzePipeline() *Pipeline {
	return s.analyzePipeline
}

// GetMergePipeline returns the merge pipeline (for testing).
func (s *Service) GetMergePipeline() *Pipeline {
	return s.mergePipeline
}
