package queue

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap/zapcore"

	"github.com/fusionn/pkg/logger"
)

// syncBuffer keeps the worker goroutine's writes and the test's reads race-free.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestFailedJobReportsItsStage(t *testing.T) {
	t.Setenv("LOG_LEVEL", "")
	buf := &syncBuffer{}
	logger.Configure(logger.Config{Output: zapcore.AddSync(buf)})
	t.Cleanup(func() { logger.Configure(logger.Config{Output: zapcore.AddSync(&syncBuffer{})}) })

	q := NewMergeQueue(Config{Workers: 1, QueueSize: 4}, func(ctx context.Context, job *MergeJob) error {
		return errors.New("merge boom")
	})
	q.Start()
	defer q.Stop()

	job := &MergeJob{VideoPath: "/media/v.mkv", MediaTitle: "My Show S01E02"}
	if err := q.Enqueue(job); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// The status flips before the log line is written, so wait for the line.
	var failed string
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, line := range strings.Split(buf.String(), "\n") {
			if strings.Contains(line, "event=job_failed") {
				failed = line
			}
		}
		if failed != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if failed == "" {
		t.Fatalf("no job_failed line in %q", buf.String())
	}
	for _, want := range []string{"stage=merge", "job_id=" + job.JobID, "worker=1"} {
		if !strings.Contains(failed, want) {
			t.Errorf("job_failed missing %q: %s", want, failed)
		}
	}
}

// waitForLine returns the first log line holding every want, failing after 5s.
func waitForLine(t *testing.T, buf *syncBuffer, want ...string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, line := range strings.Split(buf.String(), "\n") {
			found := true
			for _, w := range want {
				if !strings.Contains(line, w) {
					found = false
					break
				}
			}
			if found {
				return line
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no line with %q in:\n%s", want, buf.String())
	return ""
}

// A callback merge job runs on a worker, away from the request that queued it.
// Its events must still name the request and the translation job it came from.
func TestWorkerRestoresTriggerCorrelation(t *testing.T) {
	t.Setenv("LOG_LEVEL", "")
	buf := &syncBuffer{}
	logger.Configure(logger.Config{Output: zapcore.AddSync(buf)})
	t.Cleanup(func() { logger.Configure(logger.Config{Output: zapcore.AddSync(&syncBuffer{})}) })

	seen := make(chan string, 1)
	q := NewMergeQueue(Config{Workers: 1, QueueSize: 4}, func(ctx context.Context, job *MergeJob) error {
		seen <- logger.RequestID(ctx)
		return nil
	})
	q.Start()
	defer q.Stop()

	if err := q.Enqueue(&MergeJob{JobID: "j2", ParentJobID: "j1", RequestID: "req-1"}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	select {
	case got := <-seen:
		if got != "req-1" {
			t.Errorf("handler ctx request_id = %q, want req-1", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not run")
	}
	for _, event := range []string{"event=job_enqueued", "event=job_started", "event=job_completed"} {
		waitForLine(t, buf, event, "job_id=j2 request_id=req-1 parent_job_id=j1")
	}
}

// A panicking handler fails its own job; the worker survives and runs the next.
func TestWorkerSurvivesHandlerPanic(t *testing.T) {
	t.Setenv("LOG_LEVEL", "")
	buf := &syncBuffer{}
	logger.Configure(logger.Config{Output: zapcore.AddSync(buf)})
	t.Cleanup(func() { logger.Configure(logger.Config{Output: zapcore.AddSync(&syncBuffer{})}) })

	q := NewMergeQueue(Config{Workers: 1, QueueSize: 4}, func(ctx context.Context, job *MergeJob) error {
		if job.JobID == "boom" {
			panic("merge bug")
		}
		return nil
	})
	q.Start()
	defer q.Stop()

	for _, id := range []string{"boom", "after"} {
		if err := q.Enqueue(&MergeJob{JobID: id}); err != nil {
			t.Fatalf("enqueue %s: %v", id, err)
		}
	}

	failed := waitForLine(t, buf, "event=job_failed", "job_id=boom")
	for _, want := range []string{"stage=merge", `error="panic: merge bug"`, ` stack="`} {
		if !strings.Contains(failed, want) {
			t.Errorf("job_failed missing %q: %s", want, failed)
		}
	}
	waitForLine(t, buf, "event=job_completed", "job_id=after")
	if job, ok := q.GetJob("boom"); !ok || job.Status != JobStatusFailed {
		t.Errorf("panicked job status = %+v, want failed", job)
	}
}
