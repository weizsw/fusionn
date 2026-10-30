package subtitle

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/fusionn/internal/config"
	"github.com/fusionn/internal/notification"
	"github.com/fusionn/internal/queue"
	"github.com/fusionn/pkg/logger"
)

type sentNotification struct{ title, body, kind string }

// appriseRecorder stands in for the Apprise API and records each notification.
func appriseRecorder(t *testing.T) (string, func() []sentNotification) {
	t.Helper()
	var mu sync.Mutex
	var got []sentNotification
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		mu.Lock()
		got = append(got, sentNotification{r.PostForm.Get("title"), r.PostForm.Get("body"), r.PostForm.Get("type")})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(server.Close)
	return server.URL, func() []sentNotification {
		mu.Lock()
		defer mu.Unlock()
		return append([]sentNotification(nil), got...)
	}
}

// failingProcessor ends its stage with err; a nil err passes.
type failingProcessor struct{ err error }

func (failingProcessor) Name() string                                        { return "StubFail" }
func (failingProcessor) ShouldRun(*ProcessingContext) bool                   { return true }
func (p failingProcessor) Process(context.Context, *ProcessingContext) error { return p.err }

// panicProcessor stands in for a processor bug.
type panicProcessor struct{ msg string }

func (panicProcessor) Name() string                                        { return "StubPanic" }
func (panicProcessor) ShouldRun(*ProcessingContext) bool                   { return true }
func (p panicProcessor) Process(context.Context, *ProcessingContext) error { panic(p.msg) }

var jobIDLine = regexp.MustCompile(`(?m)^Job ID: [0-9a-f-]{36}$`)

func idleQueue() *queue.MergeQueue {
	return queue.NewMergeQueue(queue.Config{}, func(context.Context, *queue.MergeJob) error { return nil })
}

// notifyingService is a stub service whose failure notifications go to a recorder.
func notifyingService(t *testing.T, q *queue.MergeQueue, analyze Processor) (*Service, func() []sentNotification) {
	t.Helper()
	url, sent := appriseRecorder(t)
	service := newStubService(t, q, analyze)
	service.pipelines.notifier = NewNotificationProcessor(notification.NewAppriseClient(url, "key", "tag"), true)
	return service, sent
}

func requireOneFailure(t *testing.T, sent []sentNotification, wants ...string) sentNotification {
	t.Helper()
	if len(sent) != 1 {
		t.Fatalf("got %d notifications, want 1: %+v", len(sent), sent)
	}
	n := sent[0]
	if n.kind != "failure" || n.title != "❌ Subtitle Job Failed" {
		t.Errorf("notification = %+v, want a failure notification", n)
	}
	for _, want := range wants {
		if !strings.Contains(n.body, want) {
			t.Errorf("body lacks %q:\n%s", want, n.body)
		}
	}
	return n
}

var showParams = MediaParams{Path: "/media/Show.S01E01.mkv", Title: "Show", MediaType: "episode"}

func TestAnalyzeFailureSendsAFailureNotification(t *testing.T) {
	captureLogs(t)
	service, sent := notifyingService(t, idleQueue(), failingProcessor{errors.New("bazarr search failed: api_key=SECRET123 rejected")})

	if err := service.ProcessMedia(context.Background(), showParams); err == nil {
		t.Fatal("expected the analyze failure")
	}
	n := requireOneFailure(t, sent(), "Media: Show.S01E01.mkv", "Type: episode", "Stage: analyze", "Error: processor StubFail failed: bazarr search failed: api_key=")
	if !jobIDLine.MatchString(n.body) {
		t.Errorf("body lacks the job ID:\n%s", n.body)
	}
	if strings.Contains(n.body, "SECRET123") {
		t.Errorf("body leaks the key:\n%s", n.body)
	}
}

func TestAnalyzePanicSendsAFailureNotification(t *testing.T) {
	captureLogs(t)
	service, sent := notifyingService(t, idleQueue(), panicProcessor{"analyze bug"})

	if err := service.ProcessMedia(context.Background(), showParams); err == nil {
		t.Fatal("expected the recovered panic")
	}
	requireOneFailure(t, sent(), "Stage: analyze", "Error: panic: analyze bug")
}

func TestEnqueueFailureSendsAFailureNotification(t *testing.T) {
	captureLogs(t)
	// The queue is not started and its one slot is taken, so the merge job cannot be queued.
	q := queue.NewMergeQueue(queue.Config{Workers: 1, QueueSize: 1}, func(context.Context, *queue.MergeJob) error { return nil })
	if err := q.Enqueue(&queue.MergeJob{JobID: "filler"}); err != nil {
		t.Fatalf("enqueue filler: %v", err)
	}
	service, sent := notifyingService(t, q, stubAnalyzingProcessor{})

	if err := service.ProcessMedia(context.Background(), showParams); err == nil {
		t.Fatal("expected the enqueue failure")
	}
	requireOneFailure(t, sent(), "Stage: enqueue", "Error: ")
}

func TestMergeFailureSendsAFailureNotification(t *testing.T) {
	captureLogs(t)
	service, sent := notifyingService(t, idleQueue(), stubAnalyzingProcessor{})
	service.pipelines.mergePipeline = NewPipeline().AddProcessor(failingProcessor{errors.New("ffmpeg failed: exit status 1")})

	job := &queue.MergeJob{JobID: "j-merge", VideoPath: "/media/Show.S01E01.mkv", MediaType: "episode"}
	if err := service.ProcessMergeJob(context.Background(), job); err == nil {
		t.Fatal("expected the merge failure")
	}
	requireOneFailure(t, sent(), "Media: Show.S01E01.mkv", "Stage: merge", "Job ID: j-merge", "Error: processor StubFail failed: ffmpeg failed: exit status 1")
}

// The merge queue logs job_failed with the stack. ProcessMergeJob turns the
// panic into that error itself, so the notification covers a panic too.
func TestMergePanicSendsAFailureNotification(t *testing.T) {
	captureLogs(t)
	service, sent := notifyingService(t, idleQueue(), stubAnalyzingProcessor{})
	service.pipelines.mergePipeline = NewPipeline().AddProcessor(panicProcessor{"merge bug"})

	err := service.ProcessMergeJob(context.Background(), &queue.MergeJob{JobID: "j-merge", VideoPath: "/media/v.mkv"})
	var pe *logger.PanicError
	if !errors.As(err, &pe) || !strings.Contains(pe.Stack, "panicProcessor") {
		t.Fatalf("error = %v, want the recovered panic with its stack", err)
	}
	requireOneFailure(t, sent(), "Stage: merge", "Job ID: j-merge", "Error: panic: merge bug")
}

func TestNoFailureNotificationWhenOffOrSucceeded(t *testing.T) {
	captureLogs(t)
	url, sent := appriseRecorder(t)
	ctx := context.Background()

	off := newStubService(t, idleQueue(), failingProcessor{errors.New("boom")})
	off.pipelines.notifier = NewNotificationProcessor(notification.NewAppriseClient(url, "key", "tag"), false)
	_ = off.ProcessMedia(ctx, showParams)

	none := newStubService(t, idleQueue(), failingProcessor{errors.New("boom")})
	_ = none.ProcessMedia(ctx, showParams)

	ok := newStubService(t, idleQueue(), failingProcessor{})
	ok.pipelines.notifier = NewNotificationProcessor(notification.NewAppriseClient(url, "key", "tag"), true)
	if err := ok.ProcessMedia(ctx, showParams); err != nil {
		t.Fatalf("ProcessMedia: %v", err)
	}
	if err := ok.ProcessMergeJob(ctx, &queue.MergeJob{JobID: "j-ok"}); err != nil {
		t.Fatalf("ProcessMergeJob: %v", err)
	}

	if n := sent(); len(n) != 0 {
		t.Errorf("got %d notifications, want none: %+v", len(n), n)
	}
}

// NewService builds the failure notifier from the Apprise config.
func TestNewServiceWiresFailureNotifications(t *testing.T) {
	captureLogs(t)
	url, sent := appriseRecorder(t)
	service, err := NewService(&config.Config{
		Subtitle: config.SubtitleConfig{DuoSubs: config.DuoSubsConfig{Mode: "local"}},
		Apprise:  config.AppriseConfig{Enabled: true, BaseURL: url, Key: "key", Tag: "tag"},
	}, nil, idleQueue(), nil)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	service.pipelines.analyzePipeline = NewPipeline().AddProcessor(failingProcessor{errors.New("boom")})

	_ = service.ProcessMedia(context.Background(), MediaParams{Path: "/media/v.mkv", MediaType: "movie"})
	requireOneFailure(t, sent(), "Stage: analyze", "Error: processor StubFail failed: boom")
}
