package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"

	"github.com/fusionn/internal/config"
	"github.com/fusionn/pkg/logger"
)

// TranslationJob represents a translation job to be queued.
type TranslationJob struct {
	JobID        string            `json:"job_id"`
	VideoPath    string            `json:"video_path"`
	SubtitlePath string            `json:"subtitle_path"`
	MediaType    string            `json:"media_type"`
	MediaTitle   string            `json:"media_title"`
	SourceSystem string            `json:"source_system,omitempty"`
	MediaID      string            `json:"media_id,omitempty"`
	ExternalIDs  map[string]string `json:"external_ids,omitempty"`
	Season       int               `json:"season,omitempty"`
	Episode      int               `json:"episode,omitempty"`
}

// RedisClient wraps Redis client for job queue operations.
type RedisClient struct {
	client   *redis.Client
	queueKey string
}

// NewRedisClient creates a new Redis client for job queueing.
func NewRedisClient(cfg config.RedisConfig) (*RedisClient, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
		Password: cfg.Password,
		DB:       cfg.Database,
	})

	// Test connection with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	logger.Infof("✅ Connected to Redis: %s:%d", cfg.Host, cfg.Port)

	return &RedisClient{
		client:   client,
		queueKey: cfg.QueueKey,
	}, nil
}

// EnqueueTranslation adds a translation job to the Redis queue.
func (c *RedisClient) EnqueueTranslation(ctx context.Context, job *TranslationJob) error {
	// Serialize job to JSON
	data, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("failed to marshal job: %w", err)
	}

	// Push to Redis list (LPUSH for queue)
	if err := c.client.LPush(ctx, c.queueKey, data).Err(); err != nil {
		return fmt.Errorf("failed to push to Redis: %w", err)
	}

	logger.Infof("Queued translation job to Redis: key=%s, job_id=%s", c.queueKey, job.JobID)
	return nil
}

// Close closes the Redis connection.
func (c *RedisClient) Close() error {
	return c.client.Close()
}
