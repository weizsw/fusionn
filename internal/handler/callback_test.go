package handler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/fusionn/internal/config"
	"github.com/fusionn/internal/queue"
	"github.com/fusionn/internal/service/subtitle"
)

// A failed enqueue is logged as ERROR by the handler, so the request summary
// must not report it again.
func TestCallbackEnqueueFailureIsMarkedReported(t *testing.T) {
	// The queue is not started and its one slot is taken, so the next enqueue fails.
	q := queue.NewMergeQueue(queue.Config{Workers: 1, QueueSize: 1}, func(context.Context, *queue.MergeJob) error { return nil })
	if err := q.Enqueue(&queue.MergeJob{JobID: "filler"}); err != nil {
		t.Fatalf("enqueue filler: %v", err)
	}
	service, err := subtitle.NewService(
		&config.Config{Subtitle: config.SubtitleConfig{DuoSubs: config.DuoSubsConfig{Mode: "local"}}}, nil, q, nil)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	dir := t.TempDir()
	eng, chs := filepath.Join(dir, "v.en.srt"), filepath.Join(dir, "v.chs.srt")
	for _, path := range []string{eng, chs} {
		if err := os.WriteFile(path, []byte("1\n00:00:01,000 --> 00:00:02,000\nHi\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	var reported bool
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Next()
		reported = ErrorReported(c)
	})
	router.POST("/callback", NewCallbackHandler(service).HandleTranslationCallback)
	body := fmt.Sprintf(`{"job_id":"j1","video_path":"/media/v.mkv","eng_subtitle_path":%q,"chs_subtitle_path":%q}`, eng, chs)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/callback", strings.NewReader(body)))

	if rec.Code != http.StatusServiceUnavailable || !reported {
		t.Fatalf("status = %d, reported = %v; want 503 and the failure marked as reported", rec.Code, reported)
	}
}
