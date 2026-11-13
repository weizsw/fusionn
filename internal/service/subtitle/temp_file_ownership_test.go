package subtitle

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/fusionn/internal/queue"
	"github.com/fusionn/pkg/logger"
)

// failingMergeStage creates the temp merge directory the way a merger does and
// then fails or panics, so the failure cleanup is observable.
type failingMergeStage struct {
	panicWith string
}

func (failingMergeStage) Name() string                      { return "FailingMerge" }
func (failingMergeStage) ShouldRun(*ProcessingContext) bool { return true }

func (s failingMergeStage) Process(_ context.Context, pctx *ProcessingContext) error {
	dir := mergeFailureDir(pctx.VideoPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, llmMergedFileName), []byte("partial"), 0644); err != nil {
		return err
	}
	pctx.TempMergeDir = dir
	if s.panicWith != "" {
		panic(s.panicWith)
	}
	return errors.New("merge failed")
}

// mergeFailureDir is the deterministic merge directory of failingMergeStage, so
// the test can check it again after the job failed.
func mergeFailureDir(videoPath string) string {
	return filepath.Join(filepath.Dir(videoPath), ".fusionn-merge-failure")
}

// A failed merge removes what this job owns: its temp merge directory, and the
// input subtitle files only when fusionn created them. A Bazarr-downloaded
// Chinese subtitle and the paths of a translation callback stay, because the
// caller owns them.
func TestFailedMergeRemovesOnlyOwnedFiles(t *testing.T) {
	tests := []struct {
		name        string
		source      string
		keepEnglish bool
		keepChinese bool
	}{
		{name: "extracted inputs", source: ChineseSourceExtracted},
		{name: "bazarr chinese input", source: ChineseSourceBazarr, keepChinese: true},
		{name: "translation callback inputs", source: ChineseSourceTranslated, keepEnglish: true, keepChinese: true},
		{name: "unknown source keeps inputs", source: "", keepEnglish: true, keepChinese: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			enPath := writeSubtitleFixture(t, dir, "Episode.eng.srt", "en")
			zhPath := writeSubtitleFixture(t, dir, "Episode.chs.srt", "zh")
			videoPath := filepath.Join(dir, "Episode.mkv")

			service := newStubService(t, idleQueue(), stubAnalyzingProcessor{})
			service.pipelines.mergePipeline = NewPipeline("merge").AddProcessor(failingMergeStage{})
			job := &queue.MergeJob{
				JobID:            "job-fail",
				VideoPath:        videoPath,
				EnglishPath:      enPath,
				ChinesePath:      zhPath,
				MediaTitle:       "Episode",
				MediaType:        MediaTypeEpisode,
				ChineseSubSource: tt.source,
			}

			if err := service.ProcessMergeJob(context.Background(), job); err == nil {
				t.Fatal("ProcessMergeJob() succeeded, want the merge failure")
			}

			assertKept(t, enPath, tt.keepEnglish)
			assertKept(t, zhPath, tt.keepChinese)
			assertGone(t, mergeFailureDir(videoPath))
		})
	}
}

// A merge panic follows the same ownership rule as a merge error.
func TestPanickingMergeCleansLikeFailure(t *testing.T) {
	dir := t.TempDir()
	enPath := writeSubtitleFixture(t, dir, "Episode.eng.srt", "en")
	zhPath := writeSubtitleFixture(t, dir, "Episode.chs.srt", "zh")
	videoPath := filepath.Join(dir, "Episode.mkv")

	service := newStubService(t, idleQueue(), stubAnalyzingProcessor{})
	service.pipelines.mergePipeline = NewPipeline("merge").AddProcessor(failingMergeStage{panicWith: "merge bug"})
	job := &queue.MergeJob{
		JobID:            "job-panic",
		VideoPath:        videoPath,
		EnglishPath:      enPath,
		ChinesePath:      zhPath,
		MediaType:        MediaTypeEpisode,
		ChineseSubSource: ChineseSourceExtracted,
	}

	err := service.ProcessMergeJob(context.Background(), job)
	var pe *logger.PanicError
	if !errors.As(err, &pe) {
		t.Fatalf("ProcessMergeJob() error = %v, want a recovered panic", err)
	}

	assertKept(t, enPath, false)
	assertKept(t, zhPath, false)
	assertGone(t, mergeFailureDir(videoPath))
}

// extractingAnalyzeStage stands in for the analyzer and extractor: it writes
// the extraction files and records them in the analysis result, exactly like
// the real extractor does.
type extractingAnalyzeStage struct {
	englishPath string // empty: no English track
	chinesePath string // empty: no Chinese track
	// handOffChinese sets the analyze-stage Chinese path, which is what makes
	// the full pair eligible for a merge job.
	handOffChinese bool
	panicAfter     bool
}

func (extractingAnalyzeStage) Name() string                      { return "StubExtracting" }
func (extractingAnalyzeStage) ShouldRun(*ProcessingContext) bool { return true }

func (s extractingAnalyzeStage) Process(_ context.Context, pctx *ProcessingContext) error {
	pctx.Analysis = &AnalysisResult{VideoPath: pctx.VideoPath}
	if s.englishPath != "" {
		if err := os.WriteFile(s.englishPath, []byte("en"), 0644); err != nil {
			return err
		}
		pctx.Analysis.EnglishTrack = &Track{Index: 2, ExtractedPath: s.englishPath}
		pctx.EnglishSubPath = s.englishPath
	}
	if s.chinesePath != "" {
		if err := os.WriteFile(s.chinesePath, []byte("zh"), 0644); err != nil {
			return err
		}
		pctx.Analysis.ChineseTrack = &Track{Index: 3, ExtractedPath: s.chinesePath}
		if s.handOffChinese {
			pctx.ChineseSubPath = s.chinesePath
			pctx.ChineseSubSource = ChineseSourceExtracted
		}
	}
	if s.panicAfter {
		panic("extractor bug")
	}
	return nil
}

// failingAnalyzeStage fails the analyze stage after extraction.
type failingAnalyzeStage struct{}

func (failingAnalyzeStage) Name() string                      { return "StubFailing" }
func (failingAnalyzeStage) ShouldRun(*ProcessingContext) bool { return true }
func (failingAnalyzeStage) Process(context.Context, *ProcessingContext) error {
	return errors.New("bazarr failed")
}

func extractionFixture(t *testing.T) (videoPath, enPath, zhPath string) {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "Episode.mkv"),
		filepath.Join(dir, ".fusionn-extract-Episode-eng.srt"),
		filepath.Join(dir, ".fusionn-extract-Episode-chs.srt")
}

// An analyze stage that fails after extraction removes the files it extracted.
func TestAnalyzeFailureAfterExtractionRemovesExtractedFiles(t *testing.T) {
	videoPath, enPath, zhPath := extractionFixture(t)

	service := newStubService(t, idleQueue(), extractingAnalyzeStage{englishPath: enPath, chinesePath: zhPath})
	service.pipelines.analyzePipeline.AddProcessor(failingAnalyzeStage{})

	if err := service.ProcessMedia(context.Background(), MediaParams{Path: videoPath, Title: "Episode"}); err == nil {
		t.Fatal("ProcessMedia() succeeded, want the analyze failure")
	}

	assertGone(t, enPath)
	assertGone(t, zhPath)
}

// A rejected enqueue is a failed hand-off, so the extracted files go too.
func TestEnqueueFailureRemovesExtractedFiles(t *testing.T) {
	videoPath, enPath, zhPath := extractionFixture(t)

	// The single queue slot is taken, so the enqueue is rejected.
	q := queue.NewMergeQueue(queue.Config{QueueSize: 1})
	if err := q.Enqueue(&queue.MergeJob{JobID: "filler"}); err != nil {
		t.Fatalf("enqueue filler: %v", err)
	}
	service := newStubService(t, q, extractingAnalyzeStage{
		englishPath: enPath, chinesePath: zhPath, handOffChinese: true,
	})

	if err := service.ProcessMedia(context.Background(), MediaParams{Path: videoPath, Title: "Episode"}); err == nil {
		t.Fatal("ProcessMedia() succeeded, want the queue-full error")
	}

	assertGone(t, enPath)
	assertGone(t, zhPath)
}

// Analyze completion without a hand-off (here: no Chinese track and no Redis
// client) removes the extracted files it kept.
func TestAnalyzeCompletionWithoutHandoffRemovesExtractedFiles(t *testing.T) {
	videoPath, enPath, _ := extractionFixture(t)

	service := newStubService(t, idleQueue(), extractingAnalyzeStage{englishPath: enPath})

	if err := service.ProcessMedia(context.Background(), MediaParams{Path: videoPath, Title: "Episode"}); err != nil {
		t.Fatalf("ProcessMedia: %v", err)
	}

	assertGone(t, enPath)
}

// An analyze panic after extraction removes the files it extracted.
func TestAnalyzePanicAfterExtractionRemovesExtractedFiles(t *testing.T) {
	videoPath, enPath, zhPath := extractionFixture(t)

	service := newStubService(t, idleQueue(), extractingAnalyzeStage{
		englishPath: enPath, chinesePath: zhPath, handOffChinese: true, panicAfter: true,
	})

	err := service.ProcessMedia(context.Background(), MediaParams{Path: videoPath, Title: "Episode"})
	var pe *logger.PanicError
	if !errors.As(err, &pe) {
		t.Fatalf("ProcessMedia() error = %v, want a recovered panic", err)
	}

	assertGone(t, enPath)
	assertGone(t, zhPath)
}

// A queued translation job owns the English extraction, so it survives.
func TestQueuedTranslationKeepsExtractedEnglishFile(t *testing.T) {
	videoPath, enPath, _ := extractionFixture(t)

	queueClient := &recordingQueueClient{}
	service := newStubService(t, idleQueue(), extractingAnalyzeStage{englishPath: enPath})
	service.pipelines.analyzePipeline.AddProcessor(NewTranslationQueueProcessor(queueClient, ""))

	if err := service.ProcessMedia(context.Background(), MediaParams{Path: videoPath, Title: "Episode"}); err != nil {
		t.Fatalf("ProcessMedia: %v", err)
	}

	assertKept(t, enPath, true)
	if queueClient.job == nil {
		t.Fatal("no translation job was queued")
	}
	if queueClient.job.SubtitlePath != enPath {
		t.Errorf("translation job subtitle path = %q, want the kept extraction %q", queueClient.job.SubtitlePath, enPath)
	}
}

// A successful enqueue hands both files to the merge job, so both survive.
func TestSuccessfulEnqueueKeepsExtractedFiles(t *testing.T) {
	videoPath, enPath, zhPath := extractionFixture(t)

	q, jobs := captureMergeJobs(t)
	service := newStubService(t, q, extractingAnalyzeStage{
		englishPath: enPath, chinesePath: zhPath, handOffChinese: true,
	})

	if err := service.ProcessMedia(context.Background(), MediaParams{Path: videoPath, Title: "Episode"}); err != nil {
		t.Fatalf("ProcessMedia: %v", err)
	}
	job := nextMergeJob(t, jobs)
	if job.EnglishPath != enPath || job.ChinesePath != zhPath {
		t.Fatalf("merge job paths = %q, %q, want %q, %q", job.EnglishPath, job.ChinesePath, enPath, zhPath)
	}

	assertKept(t, enPath, true)
	assertKept(t, zhPath, true)
}

func assertKept(t *testing.T, path string, keep bool) {
	t.Helper()
	_, err := os.Stat(path)
	switch {
	case keep && err != nil:
		t.Errorf("file %q is gone, but fusionn does not own it: %v", path, err)
	case !keep && err == nil:
		t.Errorf("file %q survived cleanup that should have removed it", path)
	}
}

func assertGone(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err == nil {
		t.Errorf("path %q survived cleanup", path)
	}
}
