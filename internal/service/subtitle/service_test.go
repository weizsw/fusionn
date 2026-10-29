package subtitle

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap/zapcore"

	"github.com/fusionn/internal/client/bazarr"
	"github.com/fusionn/internal/config"
	"github.com/fusionn/internal/queue"
	"github.com/fusionn/pkg/logger"
)

// lockedBuffer keeps the worker goroutine's writes and the test's reads safe
// under -race.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func (b *lockedBuffer) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf.Reset()
}

// stubAnalyzingProcessor makes the analyze stage succeed without media files.
type stubAnalyzingProcessor struct{}

func (stubAnalyzingProcessor) Name() string                      { return "StubAnalyze" }
func (stubAnalyzingProcessor) ShouldRun(*ProcessingContext) bool { return true }

func (stubAnalyzingProcessor) Process(_ context.Context, pctx *ProcessingContext) error {
	pctx.EnglishSubPath = "/tmp/eng.srt"
	pctx.ChineseSubPath = "/tmp/chs.srt"
	return nil
}

// A rejected enqueue is the final failure of the operation: exactly one ERROR
// with a stable stage, and no success events afterwards.
func TestProcessMediaReportsEnqueueFailureOnce(t *testing.T) {
	t.Setenv("LOG_LEVEL", "")
	buf := &lockedBuffer{}
	logger.Configure(logger.Config{Output: zapcore.AddSync(buf)})
	t.Cleanup(func() { logger.Configure(logger.Config{Output: zapcore.AddSync(&lockedBuffer{})}) })

	block := make(chan struct{})
	workerBusy := make(chan struct{}, 1)
	q := queue.NewMergeQueue(queue.Config{Workers: 1, QueueSize: 1}, func(context.Context, *queue.MergeJob) error {
		select {
		case workerBusy <- struct{}{}:
		default:
		}
		<-block
		return nil
	})
	q.Start()
	defer func() {
		close(block)
		q.Stop()
	}()

	// One job occupies the worker, one fills the single queue slot.
	if err := q.Enqueue(&queue.MergeJob{JobID: "filler-1"}); err != nil {
		t.Fatalf("enqueue filler-1: %v", err)
	}
	select {
	case <-workerBusy:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not pick up the filler job")
	}
	if err := q.Enqueue(&queue.MergeJob{JobID: "filler-2"}); err != nil {
		t.Fatalf("enqueue filler-2: %v", err)
	}
	buf.Reset()

	service, err := NewService(
		&config.Config{Subtitle: config.SubtitleConfig{DuoSubs: config.DuoSubsConfig{Mode: "local"}}},
		nil,
		q,
		nil,
	)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	service.pipelines = &servicePipelines{
		analyzePipeline: NewPipeline().AddProcessor(stubAnalyzingProcessor{}),
		mergePipeline:   NewPipeline(),
	}

	err = service.ProcessMedia(context.Background(), MediaParams{Path: "/media/v.mkv", Title: "My Show"})
	if err == nil {
		t.Fatal("expected the queue-full error")
	}

	out := buf.String()
	var failures []string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "event=job_failed") {
			failures = append(failures, line)
		}
	}
	if len(failures) != 1 {
		t.Fatalf("expected exactly one job_failed, got %d:\n%s", len(failures), out)
	}
	if !strings.Contains(failures[0], "stage=enqueue") || !strings.Contains(failures[0], "job_id=") {
		t.Errorf("failure line missing stage/job id: %s", failures[0])
	}
	if strings.Contains(out, "event=analysis_completed") || strings.Contains(out, "event=job_enqueued") {
		t.Errorf("success event logged after a rejected enqueue:\n%s", out)
	}
}

func TestNewServiceAnalyzePipelineRunsBazarrSDHFilterAfterBazarrSearch(t *testing.T) {
	mergeQueue := queue.NewMergeQueue(queue.Config{}, func(context.Context, *queue.MergeJob) error {
		return nil
	})
	service, err := NewService(
		&config.Config{Subtitle: config.SubtitleConfig{DuoSubs: config.DuoSubsConfig{Mode: "local"}}},
		&recordingQueueClient{},
		mergeQueue,
		bazarr.NewClient("http://bazarr", "key", 1),
	)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	processors := service.GetAnalyzePipeline().GetProcessors()
	var names []string
	for _, proc := range processors {
		names = append(names, proc.Name())
	}

	want := []string{
		ProcessorNameAnalyzer,
		ProcessorNameExtractor,
		ProcessorNameSDHFilter,
		ProcessorNameBazarrSearch,
		ProcessorNameBazarrSDHFilter,
		ProcessorNameTranslationQueue,
	}
	if len(names) != len(want) {
		t.Fatalf("processor names = %#v, want %#v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("processor names = %#v, want %#v", names, want)
		}
	}
}

func TestNewServiceAnalyzePipelineSkipsTranslationQueueWithoutRedisClient(t *testing.T) {
	mergeQueue := queue.NewMergeQueue(queue.Config{}, func(context.Context, *queue.MergeJob) error {
		return nil
	})
	service, err := NewService(
		&config.Config{Subtitle: config.SubtitleConfig{DuoSubs: config.DuoSubsConfig{Mode: "local"}}},
		nil,
		mergeQueue,
		bazarr.NewClient("http://bazarr", "key", 1),
	)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	for _, proc := range service.GetAnalyzePipeline().GetProcessors() {
		if proc.Name() == ProcessorNameTranslationQueue {
			t.Fatalf("translation queue processor should not be registered without Redis client")
		}
	}
}

// panickingProcessor stands in for a processor bug in the analyze stage.
type panickingProcessor struct{}

func (panickingProcessor) Name() string                      { return "StubPanic" }
func (panickingProcessor) ShouldRun(*ProcessingContext) bool { return true }

func (panickingProcessor) Process(context.Context, *ProcessingContext) error {
	panic("analyze bug")
}

func newStubService(t *testing.T, q *queue.MergeQueue, analyze Processor) *Service {
	t.Helper()
	service, err := NewService(
		&config.Config{Subtitle: config.SubtitleConfig{DuoSubs: config.DuoSubsConfig{Mode: "local"}}},
		nil,
		q,
		nil,
	)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	service.pipelines = &servicePipelines{
		analyzePipeline: NewPipeline().AddProcessor(analyze),
		mergePipeline:   NewPipeline(),
	}
	return service
}

// The webhook runs ProcessMedia on its own goroutine, outside gin's recovery.
// A panic there fails the job once, with its stack, instead of ending the
// process.
func TestProcessMediaRecoversPanic(t *testing.T) {
	t.Setenv("LOG_LEVEL", "")
	buf := &lockedBuffer{}
	logger.Configure(logger.Config{Output: zapcore.AddSync(buf)})
	t.Cleanup(func() { logger.Configure(logger.Config{Output: zapcore.AddSync(&lockedBuffer{})}) })

	q := queue.NewMergeQueue(queue.Config{}, func(context.Context, *queue.MergeJob) error { return nil })
	service := newStubService(t, q, panickingProcessor{})

	err := service.ProcessMedia(context.Background(), MediaParams{Path: "/media/v.mkv", Title: "My Show"})
	var pe *logger.PanicError
	if !errors.As(err, &pe) {
		t.Fatalf("ProcessMedia error = %v, want a recovered panic", err)
	}
	var failures []string
	for _, line := range strings.Split(buf.String(), "\n") {
		if strings.Contains(line, "event=job_failed") {
			failures = append(failures, line)
		}
	}
	if len(failures) != 1 {
		t.Fatalf("expected exactly one job_failed, got %d:\n%s", len(failures), buf.String())
	}
	for _, want := range []string{"stage=analyze", "job_id=", `error="panic: analyze bug"`, ` stack="`} {
		if !strings.Contains(failures[0], want) {
			t.Errorf("job_failed missing %q: %s", want, failures[0])
		}
	}
}

// Merge jobs keep the request that queued them; a callback merge job also keeps
// the translation job it came from.
func TestMergeJobsCarryTriggerCorrelation(t *testing.T) {
	t.Setenv("LOG_LEVEL", "")
	logger.Configure(logger.Config{Output: zapcore.AddSync(&lockedBuffer{})})
	t.Cleanup(func() { logger.Configure(logger.Config{Output: zapcore.AddSync(&lockedBuffer{})}) })

	jobs := make(chan *queue.MergeJob, 2)
	q := queue.NewMergeQueue(queue.Config{Workers: 1, QueueSize: 4}, func(_ context.Context, job *queue.MergeJob) error {
		jobs <- job
		return nil
	})
	q.Start()
	defer q.Stop()
	service := newStubService(t, q, stubAnalyzingProcessor{})

	webhookCtx := logger.WithRequestID(context.Background(), "req-webhook")
	if err := service.ProcessMedia(webhookCtx, MediaParams{Path: "/media/v.mkv", Title: "My Show"}); err != nil {
		t.Fatalf("ProcessMedia: %v", err)
	}
	callbackCtx := logger.WithRequestID(context.Background(), "req-callback")
	if err := service.ProcessWithSubtitles(callbackCtx, "/media/v.mkv", "/tmp/eng.srt", "/tmp/chs.srt", "translation-job"); err != nil {
		t.Fatalf("ProcessWithSubtitles: %v", err)
	}

	received := make([]*queue.MergeJob, 0, 2)
	for len(received) < 2 {
		select {
		case job := <-jobs:
			received = append(received, job)
		case <-time.After(5 * time.Second):
			t.Fatalf("worker received %d of 2 jobs", len(received))
		}
	}
	if direct := received[0]; direct.RequestID != "req-webhook" || direct.ParentJobID != "" {
		t.Errorf("direct merge job request_id=%q parent_job_id=%q", direct.RequestID, direct.ParentJobID)
	}
	if linked := received[1]; linked.RequestID != "req-callback" || linked.ParentJobID != "translation-job" {
		t.Errorf("callback merge job request_id=%q parent_job_id=%q", linked.RequestID, linked.ParentJobID)
	}
}
