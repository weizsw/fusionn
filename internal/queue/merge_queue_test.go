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
