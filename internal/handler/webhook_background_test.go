package handler

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap/zapcore"

	"github.com/fusionn/internal/config"
	"github.com/fusionn/internal/queue"
	"github.com/fusionn/internal/service/subtitle"
	"github.com/fusionn/pkg/logger"
)

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

// The background job triggered by a webhook must keep the request's
// correlation values (request_id -> new job_id) while ignoring the request's
// cancellation after the 202 response.
func TestWebhookBackgroundJobKeepsRequestCorrelation(t *testing.T) {
	t.Setenv("LOG_LEVEL", "")
	buf := &syncBuffer{}
	logger.Configure(logger.Config{Output: zapcore.AddSync(buf)})
	t.Cleanup(func() { logger.Configure(logger.Config{Output: zapcore.AddSync(&syncBuffer{})}) })

	mergeQueue := queue.NewMergeQueue(queue.Config{}, func(context.Context, *queue.MergeJob) error { return nil })
	service, err := subtitle.NewService(
		&config.Config{Subtitle: config.SubtitleConfig{DuoSubs: config.DuoSubsConfig{Mode: "local"}}},
		nil,
		mergeQueue,
		nil,
	)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Request = c.Request.WithContext(
			logger.WithRequestID(c.Request.Context(), "req-bg-1"))
		c.Next()
	})
	router.POST("/api/v1/webhook/sonarr", NewWebhookHandler(service).HandleSonarr)

	payload := `{"eventType":"Download","series":{"id":1,"title":"My Show"},` +
		`"episodes":[{"id":7,"seasonNumber":1,"episodeNumber":2,"title":"Pilot"}],` +
		`"episodeFile":{"path":"/nonexistent/My Show/S01E02.mkv"}}`
	reqCtx, cancel := context.WithCancel(context.Background())
	request := httptest.NewRequest(http.MethodPost, "/api/v1/webhook/sonarr", strings.NewReader(payload)).
		WithContext(reqCtx)
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", recorder.Code)
	}
	// The client is gone the moment the webhook is acknowledged.
	cancel()

	var received, failed string
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, line := range strings.Split(buf.String(), "\n") {
			if strings.Contains(line, "event=job_received") {
				received = line
			}
			if strings.Contains(line, "event=job_failed") {
				failed = line
			}
		}
		if received != "" && failed != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if received == "" {
		t.Fatalf("no job_received line in %q", buf.String())
	}
	if !strings.Contains(received, "request_id=req-bg-1") {
		t.Errorf("job_received lost the request correlation: %s", received)
	}
	if !strings.Contains(received, "job_id=") {
		t.Errorf("job_received has no job id: %s", received)
	}
	if failed == "" {
		t.Fatalf("no job_failed line in %q", buf.String())
	}
	if !strings.Contains(failed, "request_id=req-bg-1") {
		t.Errorf("job_failed lost the request correlation: %s", failed)
	}
	if strings.Contains(failed, "context canceled") {
		t.Errorf("canceling the request canceled the background job: %s", failed)
	}
	if !strings.Contains(failed, "video file not found") {
		t.Errorf("expected the real failure cause: %s", failed)
	}
}
