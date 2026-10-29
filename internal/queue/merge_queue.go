package queue

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/fusionn/pkg/logger"
)

// MergeJob represents a subtitle merge job.
type MergeJob struct {
	JobID            string
	ParentJobID      string // translation Job a callback merge job came from
	RequestID        string // HTTP request that queued the job
	VideoPath        string
	EnglishPath      string
	ChinesePath      string
	MediaTitle       string
	MediaType        string
	ChineseSubSource string
	Status           string
	CreatedAt        time.Time
	StartedAt        *time.Time
	CompletedAt      *time.Time
	Error            string
}

const (
	JobStatusPending    = "pending"
	JobStatusProcessing = "processing"
	JobStatusFailed     = "failed"
	JobStatusCompleted  = "completed"
)

// JobHandler is a function that processes a merge job.
type JobHandler func(ctx context.Context, job *MergeJob) error

// MergeQueue is an in-process job queue for subtitle merging.
type MergeQueue struct {
	jobs       chan *MergeJob
	handler    JobHandler
	workers    int
	wg         sync.WaitGroup
	ctx        context.Context
	cancel     context.CancelFunc
	mu         sync.RWMutex
	jobsMap    map[string]*MergeJob // For job status tracking
	maxRetries int
}

// Config holds queue configuration.
type Config struct {
	Workers    int // Number of concurrent workers (default: 1)
	QueueSize  int // Max pending jobs (default: 100)
	MaxRetries int // Max retry attempts (default: 3)
}

// NewMergeQueue creates a new in-process merge queue.
func NewMergeQueue(cfg Config, handler JobHandler) *MergeQueue {
	if cfg.Workers <= 0 {
		cfg.Workers = 1 // Sequential processing by default
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 100
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 3
	}

	ctx, cancel := context.WithCancel(context.Background())

	q := &MergeQueue{
		jobs:       make(chan *MergeJob, cfg.QueueSize),
		handler:    handler,
		workers:    cfg.Workers,
		ctx:        ctx,
		cancel:     cancel,
		jobsMap:    make(map[string]*MergeJob),
		maxRetries: cfg.MaxRetries,
	}

	return q
}

// SetHandler sets the job handler. Must be called before Start.
func (q *MergeQueue) SetHandler(handler JobHandler) {
	q.handler = handler
}

// Start starts the worker pool.
func (q *MergeQueue) Start() {
	logger.Component("queue").Info(context.Background(), "queue_started", "Merge queue started",
		zap.Int("workers", q.workers))

	for i := 0; i < q.workers; i++ {
		q.wg.Add(1)
		go q.worker(i + 1)
	}
}

// Stop gracefully shuts down the queue.
func (q *MergeQueue) Stop() {
	logger.Component("queue").Info(context.Background(), "queue_stopping", "Merge queue stopping")
	q.cancel()
	close(q.jobs)
	q.wg.Wait()
	logger.Component("queue").Info(context.Background(), "queue_stopped", "Merge queue stopped")
}

// Enqueue adds a new job to the queue.
func (q *MergeQueue) Enqueue(job *MergeJob) error {
	if job.JobID == "" {
		job.JobID = uuid.New().String()
	}
	job.Status = JobStatusPending
	job.CreatedAt = time.Now()

	// Store job for status tracking
	q.mu.Lock()
	q.jobsMap[job.JobID] = job
	q.mu.Unlock()

	select {
	case q.jobs <- job:
		logger.Component("queue").Info(jobContext(q.ctx, job), "job_enqueued", "Job enqueued",
			zap.String("media_title", job.MediaTitle))
		return nil
	case <-q.ctx.Done():
		return q.ctx.Err()
	default:
		return ErrQueueFull
	}
}

// GetJob returns the status of a job by ID.
func (q *MergeQueue) GetJob(jobID string) (*MergeJob, bool) {
	q.mu.RLock()
	defer q.mu.RUnlock()
	job, ok := q.jobsMap[jobID]
	return job, ok
}

// GetPendingCount returns the number of jobs waiting in queue.
func (q *MergeQueue) GetPendingCount() int {
	return len(q.jobs)
}

// worker processes jobs from the queue.
func (q *MergeQueue) worker(id int) {
	defer q.wg.Done()

	logger.Component("queue").Debug(context.Background(), "worker_started", "Worker started", zap.Int("worker", id))

	for {
		select {
		case job, ok := <-q.jobs:
			if !ok {
				logger.Component("queue").Debug(context.Background(), "worker_stopped", "Worker shutting down", zap.Int("worker", id))
				return
			}

			q.processJob(id, job)

		case <-q.ctx.Done():
			logger.Component("queue").Debug(context.Background(), "worker_stopped", "Worker stopped", zap.Int("worker", id))
			return
		}
	}
}

// processJob processes a single job with retry logic.
func (q *MergeQueue) processJob(workerID int, job *MergeJob) {
	startTime := time.Now()
	job.StartedAt = &startTime
	job.Status = JobStatusProcessing

	log := logger.Component("queue")
	ctx := jobContext(q.ctx, job)
	log.Info(ctx, "job_started", "Job started",
		zap.Int("worker", workerID), zap.String("media_title", job.MediaTitle))

	// Execute job handler
	err := q.runHandler(ctx, job)

	completedTime := time.Now()
	job.CompletedAt = &completedTime
	duration := completedTime.Sub(startTime)

	if err != nil {
		job.Status = JobStatusFailed
		job.Error = err.Error()
		log.Error(ctx, "job_failed", "Job failed",
			zap.Int("worker", workerID), zap.String("media_title", job.MediaTitle),
			zap.String("stage", "merge"), logger.DurMs(duration), zap.Error(err), logger.PanicStack(err))

		// TODO: Implement retry logic here if needed
	} else {
		job.Status = JobStatusCompleted
		log.Info(ctx, "job_completed", "Job completed",
			zap.Int("worker", workerID), zap.String("media_title", job.MediaTitle), logger.DurMs(duration))
	}

	// Clean up old jobs after 1 hour
	go q.cleanupOldJob(job.JobID, 1*time.Hour)
}

// jobContext restores the correlation of the request that queued the job, so a
// worker's events still name that request and, for a callback merge job, the
// translation job it came from. Empty IDs render nothing.
func jobContext(ctx context.Context, job *MergeJob) context.Context {
	ctx = logger.WithJobID(ctx, job.JobID)
	ctx = logger.WithRequestID(ctx, job.RequestID)
	return logger.WithParentJobID(ctx, job.ParentJobID)
}

// runHandler runs the job handler and returns a panic as a *logger.PanicError,
// so one bad job fails like any other instead of ending the process.
func (q *MergeQueue) runHandler(ctx context.Context, job *MergeJob) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = logger.Recovered(r)
		}
	}()
	return q.handler(ctx, job)
}

// cleanupOldJob removes job from tracking after a delay.
func (q *MergeQueue) cleanupOldJob(jobID string, delay time.Duration) {
	time.Sleep(delay)
	q.mu.Lock()
	delete(q.jobsMap, jobID)
	q.mu.Unlock()
}

// ErrQueueFull is returned when the queue is at capacity.
var ErrQueueFull = &Error{Message: "merge queue is full"}

// Error represents a queue error.
type Error struct {
	Message string
}

func (e *Error) Error() string {
	return e.Message
}
