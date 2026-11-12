package handler

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// A failed enqueue is logged as ERROR by the handler, so the request summary
// must not report it again.
func TestCallbackEnqueueFailureIsMarkedReported(t *testing.T) {
	processor := newRecordingProcessor()
	processor.subErr = errors.New("queue full")
	eng, chs := writeSubtitleFiles(t)

	var reported bool
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Next()
		reported = ErrorReported(c)
	})
	router.POST("/callback", NewCallbackHandler(processor).HandleTranslationCallback)
	body := fmt.Sprintf(`{"job_id":"j1","video_path":"/media/v.mkv","eng_subtitle_path":%q,"chs_subtitle_path":%q}`, eng, chs)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/callback", strings.NewReader(body)))

	if rec.Code != http.StatusServiceUnavailable || !reported {
		t.Fatalf("status = %d, reported = %v; want 503 and the failure marked as reported", rec.Code, reported)
	}
}

// A translation callback must hand the payload's paths to the merge job.
func TestTranslationCallbackCallsProcessWithSubtitles(t *testing.T) {
	processor := newRecordingProcessor()
	eng, chs := writeSubtitleFiles(t)

	rec := serveWebhook(t, NewCallbackHandler(processor).HandleTranslationCallback, "/callback", TranslationCallbackPayload{
		JobID:           "job-7",
		VideoPath:       "/media/v.mkv",
		EngSubtitlePath: eng,
		ChsSubtitlePath: chs,
	})

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body %s", rec.Code, rec.Body)
	}

	want := withSubtitlesCall{videoPath: "/media/v.mkv", engSubPath: eng, chsSubPath: chs, parentJobID: "job-7"}
	if got := processor.waitSubCall(t); got != want {
		t.Errorf("ProcessWithSubtitles call = %+v, want %+v", got, want)
	}
}
