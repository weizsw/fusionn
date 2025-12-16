package subtitle

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/redis/go-redis/v9"

	"github.com/fusionn/pkg/logger"
)

// TranslationQueueProcessor queues translation jobs when Chinese subtitle is missing.
type TranslationQueueProcessor struct {
	redisClient *redis.Client
	queueKey    string
}

// NewTranslationQueueProcessor creates a new translation queue processor.
func NewTranslationQueueProcessor(client *redis.Client, queueKey string) *TranslationQueueProcessor {
	return &TranslationQueueProcessor{
		redisClient: client,
		queueKey:    queueKey,
	}
}

// Name returns the processor name.
func (p *TranslationQueueProcessor) Name() string {
	return "TranslationQueueProcessor"
}

// ShouldRun runs if translation is needed (Chinese missing, English present).
func (p *TranslationQueueProcessor) ShouldRun(pctx *ProcessingContext) bool {
	return pctx.NeedsTranslation && pctx.EnglishSubPath != ""
}

// JobMessage matches the format expected by fusionn-subs.
// See: fusionn-subs/internal/types/job.go
type JobMessage struct {
	FileName  string `json:"file_name"`
	Path      string `json:"path"`       // Path to English subtitle (required)
	VideoPath string `json:"video_path"` // Path to video file
	Overview  string `json:"overview"`   // Optional description
	Provider  string `json:"provider"`   // Source: "sonarr", "radarr", "manual"
}

// Process queues a translation job in Redis.
func (p *TranslationQueueProcessor) Process(ctx context.Context, pctx *ProcessingContext) error {
	logger.Infof("📬 Queuing translation job: %s", pctx.MediaTitle)

	// Build translation job matching fusionn-subs format
	job := JobMessage{
		FileName:  filepath.Base(pctx.VideoPath),
		Path:      pctx.EnglishSubPath, // English subtitle path (required by fusionn-subs)
		VideoPath: pctx.VideoPath,
		Overview:  pctx.MediaTitle,
		Provider:  pctx.MediaType, // "episode" or "movie"
	}

	// Serialize to JSON
	jobJSON, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("failed to marshal translation job: %w", err)
	}

	// Push to Redis list (LPUSH for FIFO with BRPOP on consumer side)
	if err := p.redisClient.LPush(ctx, p.queueKey, jobJSON).Err(); err != nil {
		return fmt.Errorf("failed to queue translation job: %w", err)
	}

	logger.Infof("✅ Translation job queued: %s (eng: %s)", pctx.MediaTitle, pctx.EnglishSubPath)
	return nil
}
