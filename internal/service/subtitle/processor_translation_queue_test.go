package subtitle

import (
	"context"
	"testing"

	"github.com/fusionn/internal/queue"
)

type recordingQueueClient struct {
	job *queue.TranslationJob
}

func (c *recordingQueueClient) EnqueueTranslation(_ context.Context, job *queue.TranslationJob) error {
	c.job = job
	return nil
}

func TestTranslationQueueShouldRun_SkipsWhenChineseSubPathSet(t *testing.T) {
	proc := &TranslationQueueProcessor{}

	pctx := &ProcessingContext{
		Analysis: &AnalysisResult{
			EnglishTrack: &Track{Index: 1, Language: "eng"},
			ChineseTrack: nil,
		},
		ChineseSubPath: "/path/to/bazarr-downloaded.zh.srt",
	}

	if proc.ShouldRun(pctx) {
		t.Error("TranslationQueue should NOT run when ChineseSubPath is already set (Bazarr found a subtitle)")
	}
}

func TestTranslationQueueShouldRun_RunsWhenNoChineseSub(t *testing.T) {
	proc := &TranslationQueueProcessor{}

	pctx := &ProcessingContext{
		Analysis: &AnalysisResult{
			EnglishTrack: &Track{Index: 1, Language: "eng"},
			ChineseTrack: nil,
		},
		ChineseSubPath: "",
	}

	if !proc.ShouldRun(pctx) {
		t.Error("TranslationQueue SHOULD run when no Chinese subtitle exists")
	}
}

func TestTranslationQueueShouldRun_SkipsWhenChineseTrackExists(t *testing.T) {
	proc := &TranslationQueueProcessor{}

	pctx := &ProcessingContext{
		Analysis: &AnalysisResult{
			EnglishTrack: &Track{Index: 1, Language: "eng"},
			ChineseTrack: &Track{Index: 2, Language: "chi"},
		},
	}

	if proc.ShouldRun(pctx) {
		t.Error("TranslationQueue should NOT run when Chinese track exists in media")
	}
}

func TestTranslationQueueProcessIncludesGlossaryIdentity(t *testing.T) {
	queueClient := &recordingQueueClient{}
	proc := NewTranslationQueueProcessor(queueClient, "")

	pctx := &ProcessingContext{
		JobID:          "job-1",
		VideoPath:      "/tv/The Capture/Season 01/S01E01.mkv",
		EnglishSubPath: "/tv/The Capture/Season 01/S01E01.eng.srt",
		MediaType:      MediaTypeEpisode,
		MediaTitle:     "The Capture S01E01",
		SourceSystem:   SourceSystemSonarr,
		MediaID:        "42",
		ExternalIDs: map[string]string{
			ExternalIDSonarr: "42",
			ExternalIDTVDB:   "355620",
			ExternalIDIMDB:   "tt8201186",
		},
		Season:  1,
		Episode: 1,
	}

	if err := proc.Process(context.Background(), pctx); err != nil {
		t.Fatalf("process: %v", err)
	}

	if queueClient.job == nil {
		t.Fatal("expected translation job to be queued")
	}
	if queueClient.job.SourceSystem != SourceSystemSonarr {
		t.Fatalf("source system = %q", queueClient.job.SourceSystem)
	}
	if queueClient.job.MediaID != "42" {
		t.Fatalf("media id = %q", queueClient.job.MediaID)
	}
	if queueClient.job.ExternalIDs[ExternalIDTVDB] != "355620" {
		t.Fatalf("tvdb id = %q", queueClient.job.ExternalIDs[ExternalIDTVDB])
	}
	if queueClient.job.Season != 1 || queueClient.job.Episode != 1 {
		t.Fatalf("season/episode = %d/%d", queueClient.job.Season, queueClient.job.Episode)
	}
}
