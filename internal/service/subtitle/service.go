package subtitle

import (
	"context"
	"strings"
	"sync"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/fusionn/internal/client/bazarr"
	"github.com/fusionn/internal/config"
	"github.com/fusionn/internal/notification"
	"github.com/fusionn/internal/queue"
	"github.com/fusionn/pkg/logger"
)

// log is the worker-level logger; correlation IDs come from ctx.
var log = logger.Component("worker")

// Service orchestrates subtitle processing using the pipeline architecture.
type Service struct {
	mu           sync.Mutex
	pipelines    *servicePipelines
	configSource func() *config.Config
	lastConfig   *config.Config
	mergeQueue   *queue.MergeQueue
	redisClient  QueueClient
}

type servicePipelines struct {
	config          *config.Config
	analyzePipeline *Pipeline
	mergePipeline   *Pipeline
	// notifier sends the failure notification of a job; nil sends nothing.
	notifier *NotificationProcessor
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
	engine := effectiveSubtitleEngine(cfg.Subtitle.Engine)
	// The LLM renderer needs native ASS alignment/position metadata, and all
	// uncertain one-sided lines must reach it unmodified.
	analyzer.SetPreserveNativeASS(engine == config.EngineLLM)

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
	analyzePipeline := NewPipeline("analyze")
	analyzePipeline.AddProcessor(NewAnalyzerProcessor(analyzer))
	analyzePipeline.AddProcessor(NewExtractorProcessor(analyzer))
	// SDH filtering discards lines, which would hide uncertain one-sided
	// dialogue from the LLM. Only the DuoSubs engine filters here.
	if engine != config.EngineLLM {
		analyzePipeline.AddProcessor(NewSDHFilterProcessor())
	}
	if bazarrClient != nil {
		analyzePipeline.AddProcessor(NewBazarrSearchProcessor(
			bazarrClient, cfg.Bazarr.LanguageCode,
			cfg.Bazarr.PollIntervalSeconds, cfg.Bazarr.PollTimeoutSeconds,
		))
		if engine != config.EngineLLM {
			analyzePipeline.AddProcessor(NewBazarrSDHFilterProcessor())
		}
	}
	if redisClient != nil {
		analyzePipeline.AddProcessor(NewTranslationQueueProcessor(redisClient, ""))
	}

	// Build merge pipeline (slow, runs asynchronously in queue)
	mergePipeline := NewPipeline("merge")
	// Create the merger selected by subtitle.engine; only that engine's
	// dependency is initialized.
	mergerProc, err := NewConfiguredMergerProcessor(cfg.Subtitle)
	if err != nil {
		return nil, err
	}
	mergePipeline.AddProcessor(NewDualLanguageProcessor(cfg.Subtitle.OpenCC))
	if engine != config.EngineLLM {
		// The LLM merger applies OpenCC to a private parsed copy so native ASS
		// text survives until after parsing; DuoSubs keeps the original order.
		mergePipeline.AddProcessor(NewConversionProcessor(cfg.Subtitle.OpenCC))
	}
	mergePipeline.AddProcessor(mergerProc)
	mergePipeline.AddProcessor(NewStyleProcessor(cfg.Subtitle.ASSStyle))
	mergePipeline.AddProcessor(NewFontEmbeddingProcessor(cfg.Subtitle.FontEmbedding))
	mergePipeline.AddProcessor(NewOutputProcessor(cfg.Subtitle.OutputSameDir))
	notifier := NewNotificationProcessor(appriseClient, cfg.Apprise.Enabled)
	mergePipeline.AddProcessor(notifier)
	mergePipeline.AddProcessor(NewCleanupProcessor())

	// Check if font embedding is enabled and binary is available
	if cfg.Subtitle.FontEmbedding.Enabled {
		if !isFusionnFontAvailable() {
			logger.Component("service").Warn(context.Background(), "font_embedding_unavailable", "fusionn-font binary not found, font embedding disabled")
		}
	}

	if !IsCleanitAvailable() {
		logger.Component("service").Warn(context.Background(), "sdh_filter_unavailable", "cleanit not found in PATH, SDH subtitle filtering disabled")
	}

	return &Service{
		pipelines: &servicePipelines{
			config:          cfg,
			analyzePipeline: analyzePipeline,
			mergePipeline:   mergePipeline,
			notifier:        notifier,
		},
		lastConfig:  cfg,
		mergeQueue:  mergeQueue,
		redisClient: redisClient,
	}, nil
}

// ProcessMedia processes a media file through the subtitle pipeline.
// Phase 1: Analyze + Extract (fast, synchronous)
// Phase 2: Merge (slow, enqueued to worker)
func (s *Service) ProcessMedia(ctx context.Context, params MediaParams) (err error) {
	pipelines := s.currentPipelines()
	jobID := uuid.New().String()
	ctx = logger.WithJobID(ctx, jobID)

	// The webhook runs this on its own goroutine, outside gin's recovery, so a
	// panic here would end the process. It fails this job instead, reported
	// once like any other failure. Every failure also sends one notification.
	stage := "analyze"
	defer func() {
		if r := recover(); r != nil {
			err = logger.Recovered(r)
			log.Error(ctx, "job_failed", "Job failed", zap.String("stage", stage), zap.Error(err), logger.PanicStack(err))
		}
		if err != nil {
			pipelines.notifier.NotifyFailure(ctx, jobID, params.Path, params.MediaType, stage, err)
		}
	}()

	pctx := &ProcessingContext{
		VideoPath:       params.Path,
		MediaType:       params.MediaType,
		MediaTitle:      params.Title,
		JobID:           jobID,
		SourceSystem:    params.SourceSystem,
		MediaID:         params.MediaID,
		ExternalIDs:     params.ExternalIDs,
		Season:          params.Season,
		Episode:         params.Episode,
		SonarrSeriesID:  params.SonarrSeriesID,
		SonarrEpisodeID: params.SonarrEpisodeID,
		RadarrID:        params.RadarrID,
		Metadata:        make(map[string]interface{}),
	}

	log.Info(ctx, "job_received", "Job received",
		zap.String("stage", "analyze"), zap.String("media_title", params.Title), zap.String("media_type", params.MediaType))

	if err := pipelines.analyzePipeline.Execute(ctx, pctx); err != nil {
		log.Error(ctx, "job_failed", "Job failed", zap.String("stage", "analyze"), zap.Error(err))
		return err
	}

	if pctx.EnglishSubPath != "" && pctx.ChineseSubPath != "" {
		mergeJob := &queue.MergeJob{
			JobID:            jobID,
			RequestID:        logger.RequestID(ctx),
			VideoPath:        params.Path,
			EnglishPath:      pctx.EnglishSubPath,
			ChinesePath:      pctx.ChineseSubPath,
			MediaTitle:       params.Title,
			MediaType:        params.MediaType,
			ChineseSubSource: pctx.ChineseSubSource,
		}

		stage = "enqueue"
		if err := s.mergeQueue.Enqueue(mergeJob); err != nil {
			// A full or stopped queue is the final failure of this operation;
			// the webhook caller only receives the 202 and ignores the error,
			// so the single ERROR is reported here, at the boundary.
			log.Error(ctx, "job_failed", "Job failed", zap.String("stage", "enqueue"), zap.Error(err))
			return err
		}

		log.Info(ctx, "job_enqueued", "Analysis complete, merge job enqueued", zap.String("stage", "merge"))
	} else {
		log.Info(ctx, "analysis_completed", "Analysis complete")
	}

	return nil
}

// ProcessWithSubtitles processes pre-extracted subtitles (from translation callback).
// This enqueues a merge job with the provided subtitle paths. The merge job
// keeps its own new Job ID; parentJobID is recorded for correlation only.
func (s *Service) ProcessWithSubtitles(ctx context.Context, videoPath, engSubPath, chsSubPath, parentJobID string) error {
	// Generate job ID
	jobID := uuid.New().String()
	ctx = logger.WithJobID(ctx, jobID)
	if parentJobID != "" {
		ctx = logger.WithParentJobID(ctx, parentJobID)
	}

	// Enqueue merge job
	mergeJob := &queue.MergeJob{
		JobID:            jobID,
		ParentJobID:      parentJobID,
		RequestID:        logger.RequestID(ctx),
		VideoPath:        videoPath,
		EnglishPath:      engSubPath,
		ChinesePath:      chsSubPath,
		MediaTitle:       videoPath,
		MediaType:        MediaTypeCallback,
		ChineseSubSource: ChineseSourceTranslated,
	}

	if err := s.mergeQueue.Enqueue(mergeJob); err != nil {
		return err
	}

	log.Info(ctx, "job_linked", "Merge job linked to translation job")
	return nil
}

// ProcessMergeJob processes a merge job from the queue (called by worker).
func (s *Service) ProcessMergeJob(ctx context.Context, job *queue.MergeJob) (err error) {
	pipelines := s.currentPipelines()
	ctx = logger.WithJobID(ctx, job.JobID)
	// The merge queue reports job_failed. A panic is turned into that error here
	// first, so the failure notification covers it too; the error keeps the
	// stack for the queue's report.
	defer func() {
		if r := recover(); r != nil {
			err = logger.Recovered(r)
		}
		if err != nil {
			pipelines.notifier.NotifyFailure(ctx, job.JobID, job.VideoPath, job.MediaType, "merge", err)
		}
	}()
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

	// Execute merge pipeline (job start/completion lifecycle is owned by the
	// merge queue worker; this layer reports stage progress).
	if err := pipelines.mergePipeline.Execute(ctx, pctx); err != nil {
		log.Debug(ctx, "job_failed", "Job failed", zap.String("stage", "merge"), zap.Error(err))
		return err
	}

	return nil
}

// GetAnalyzePipeline returns the analyze pipeline (for testing).
func (s *Service) GetAnalyzePipeline() *Pipeline {
	return s.currentPipelines().analyzePipeline
}

// GetMergePipeline returns the merge pipeline (for testing).
func (s *Service) GetMergePipeline() *Pipeline {
	return s.currentPipelines().mergePipeline
}

// effectiveSubtitleEngine resolves the engine for directly constructed configs.
// config.Load always resolves a value; an unresolved config with no explicit
// engine keeps the legacy DuoSubs behavior.
func effectiveSubtitleEngine(engine string) string {
	engine = strings.ToLower(strings.TrimSpace(engine))
	if engine == "" {
		return config.EngineDuoSubs
	}
	return engine
}
