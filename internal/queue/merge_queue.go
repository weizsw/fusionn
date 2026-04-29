package queue

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/fusionn/pkg/logger"
)

// MergeJob represents a subtitle merge job.
type MergeJob struct {
	JobID            string
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
	logger.Infof("🚀 Starting merge queue with %d worker(s)", q.workers)

	for i := 0; i < q.workers; i++ {
		q.wg.Add(1)
		go q.worker(i + 1)
	}
}

// Stop gracefully shuts down the queue.
func (q *MergeQueue) Stop() {
	logger.Info("⏹️  Stopping merge queue...")
	q.cancel()
	close(q.jobs)
	q.wg.Wait()
	logger.Info("✅ Merge queue stopped")
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
		logger.Infof("📥 Job enqueued: %s (%s)", job.JobID, job.MediaTitle)
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

	logger.Infof("🔧 Worker %d started", id)

	for {
		select {
		case job, ok := <-q.jobs:
			if !ok {
				logger.Infof("🔧 Worker %d shutting down", id)
				return
			}

			q.processJob(id, job)

		case <-q.ctx.Done():
			logger.Infof("🔧 Worker %d stopped", id)
			return
		}
	}
}

// processJob processes a single job with retry logic.
func (q *MergeQueue) processJob(workerID int, job *MergeJob) {
	startTime := time.Now()
	job.StartedAt = &startTime
	job.Status = JobStatusProcessing

	logger.Infof("🔧 Worker %d processing: %s (%s)", workerID, job.JobID, job.MediaTitle)

	// Execute job handler
	err := q.handler(q.ctx, job)

	completedTime := time.Now()
	job.CompletedAt = &completedTime
	duration := completedTime.Sub(startTime)

	if err != nil {
		job.Status = JobStatusFailed
		job.Error = err.Error()
		logger.Errorf("❌ Worker %d failed: %s (%s) - %v [%.1fs]",
			workerID, job.JobID, job.MediaTitle, err, duration.Seconds())

		// TODO: Implement retry logic here if needed
	} else {
		job.Status = JobStatusCompleted
		logger.Infof("✅ Worker %d completed: %s (%s) [%.1fs]",
			workerID, job.JobID, job.MediaTitle, duration.Seconds())
	}

	// Clean up old jobs after 1 hour
	go q.cleanupOldJob(job.JobID, 1*time.Hour)
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
