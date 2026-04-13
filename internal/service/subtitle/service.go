package subtitle

import (
	"context"

	"github.com/google/uuid"

	"github.com/fusionn/internal/client/bazarr"
	"github.com/fusionn/internal/config"
	"github.com/fusionn/internal/notification"
	"github.com/fusionn/internal/queue"
	"github.com/fusionn/pkg/logger"
)

// Service orchestrates subtitle processing using the pipeline architecture.
type Service struct {
	analyzePipeline *Pipeline // Analyze + Extract
	mergePipeline   *Pipeline // Convert + Merge + Style + Output + Notify + Cleanup
	config          *config.Config
	mergeQueue      *queue.MergeQueue
	redisClient     QueueClient
	appriseClient   *notification.AppriseClient
}

// NewService creates a new subtitle service with all processors configured.
func NewService(cfg *config.Config, redisClient QueueClient, mergeQueue *queue.MergeQueue, bazarrClient *bazarr.Client) (*Service, error) {
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
	analyzePipeline.AddProcessor(NewSDHFilterProcessor())
	if bazarrClient != nil {
		analyzePipeline.AddProcessor(NewBazarrSearchProcessor(
			bazarrClient, cfg.Bazarr.LanguageCode,
			cfg.Bazarr.PollIntervalSeconds, cfg.Bazarr.PollTimeoutSeconds,
		))
	}
	analyzePipeline.AddProcessor(NewTranslationQueueProcessor(redisClient, ""))

	// Build merge pipeline (slow, runs asynchronously in queue)
	mergePipeline := NewPipeline()
	mergePipeline.AddProcessor(NewDualLanguageProcessor(cfg.Subtitle.OpenCC))
	mergePipeline.AddProcessor(NewConversionProcessor(cfg.Subtitle.OpenCC))

	// Create merger processor (may fail if HTTP service is unreachable)
	mergerProc, err := NewMergerProcessor(cfg.Subtitle.DuoSubs)
	if err != nil {
		return nil, err
	}
	mergePipeline.AddProcessor(mergerProc)

	mergePipeline.AddProcessor(NewStyleProcessor(cfg.Subtitle.ASSStyle))
	mergePipeline.AddProcessor(NewFontEmbeddingProcessor(cfg.Subtitle.FontEmbedding))
	mergePipeline.AddProcessor(NewOutputProcessor(cfg.Subtitle.OutputSameDir))
	mergePipeline.AddProcessor(NewNotificationProcessor(appriseClient, cfg.Apprise.Enabled))
	mergePipeline.AddProcessor(NewCleanupProcessor())

	// Check if font embedding is enabled and binary is available
	if cfg.Subtitle.FontEmbedding.Enabled {
		if !isFusionnFontAvailable() {
			logger.Warn("⚠️ fusionn-font binary not found - font embedding disabled")
		}
	}

	if !IsCleanitAvailable() {
		logger.Warn("⚠️ cleanit not found in PATH - SDH subtitle filtering disabled")
	}

	return &Service{
		analyzePipeline: analyzePipeline,
		mergePipeline:   mergePipeline,
		config:          cfg,
		mergeQueue:      mergeQueue,
		redisClient:     redisClient,
		appriseClient:   appriseClient,
	}, nil
}

// ProcessMedia processes a media file through the subtitle pipeline.
// Phase 1: Analyze + Extract (fast, synchronous)
// Phase 2: Merge (slow, enqueued to worker)
func (s *Service) ProcessMedia(ctx context.Context, params MediaParams) error {
	jobID := uuid.New().String()

	pctx := &ProcessingContext{
		VideoPath:       params.Path,
		MediaType:       params.MediaType,
		MediaTitle:      params.Title,
		JobID:           jobID,
		SonarrSeriesID:  params.SonarrSeriesID,
		SonarrEpisodeID: params.SonarrEpisodeID,
		RadarrID:        params.RadarrID,
		Metadata:        make(map[string]interface{}),
	}

	logger.Infof("📝 Analyzing subtitles for %s (job: %s)", params.Title, jobID)

	if err := s.analyzePipeline.Execute(ctx, pctx); err != nil {
		logger.Errorf("Analyze pipeline failed (job: %s): %v", jobID, err)
		return err
	}

	if pctx.EnglishSubPath != "" && pctx.ChineseSubPath != "" {
		mergeJob := &queue.MergeJob{
			JobID:            jobID,
			VideoPath:        params.Path,
			EnglishPath:      pctx.EnglishSubPath,
			ChinesePath:      pctx.ChineseSubPath,
			MediaTitle:       params.Title,
			MediaType:        params.MediaType,
			ChineseSubSource: pctx.ChineseSubSource,
		}

		if err := s.mergeQueue.Enqueue(mergeJob); err != nil {
			return err
		}

		logger.Infof("✅ Analysis complete, merge job enqueued (job: %s)", jobID)
	} else {
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
		JobID:            jobID,
		VideoPath:        videoPath,
		EnglishPath:      engSubPath,
		ChinesePath:      chsSubPath,
		MediaTitle:       videoPath,
		MediaType:        "callback",
		ChineseSubSource: ChineseSourceTranslated,
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
		ChineseSubSource: job.ChineseSubSource,
		NeedsConversion:  false,
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
