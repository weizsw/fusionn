package subtitle

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/fusionn/internal/config"
	"github.com/fusionn/internal/executor"
	"github.com/fusionn/internal/queue"
	"github.com/fusionn/pkg/logger"
)

// recordingProcessor captures the merge-stage context it runs in.
type recordingProcessor struct{ contexts chan *ProcessingContext }

func (recordingProcessor) Name() string                      { return "Recording" }
func (recordingProcessor) ShouldRun(*ProcessingContext) bool { return true }

func (p recordingProcessor) Process(_ context.Context, pctx *ProcessingContext) error {
	p.contexts <- pctx
	return nil
}

// traditionalAnalyzeStage stands in for an analyze stage that extracted both
// subtitle tracks and detected Traditional Chinese.
type traditionalAnalyzeStage struct{}

func (traditionalAnalyzeStage) Name() string                      { return "StubTraditional" }
func (traditionalAnalyzeStage) ShouldRun(*ProcessingContext) bool { return true }

func (traditionalAnalyzeStage) Process(_ context.Context, pctx *ProcessingContext) error {
	pctx.EnglishSubPath = "/tmp/eng.srt"
	pctx.ChineseSubPath = "/tmp/chs.srt"
	pctx.ChineseSubSource = ChineseSourceExtracted
	pctx.NeedsConversion = true
	return nil
}

// captureMergeJobs starts a queue whose worker hands every job to the test.
func captureMergeJobs(t *testing.T) (*queue.MergeQueue, chan *queue.MergeJob) {
	t.Helper()
	jobs := make(chan *queue.MergeJob, 2)
	q := queue.NewMergeQueue(queue.Config{Workers: 1, QueueSize: 2})
	q.Start(func(_ context.Context, job *queue.MergeJob) error {
		jobs <- job
		return nil
	})
	t.Cleanup(q.Stop)
	return q, jobs
}

func nextMergeJob(t *testing.T, jobs chan *queue.MergeJob) *queue.MergeJob {
	t.Helper()
	select {
	case job := <-jobs:
		return job
	case <-time.After(5 * time.Second):
		t.Fatal("merge worker did not receive the job")
		return nil
	}
}

func nextContext(t *testing.T, contexts chan *ProcessingContext) *ProcessingContext {
	t.Helper()
	select {
	case pctx := <-contexts:
		return pctx
	case <-time.After(5 * time.Second):
		t.Fatal("merge pipeline did not run")
		return nil
	}
}

// The Traditional flag crosses the merge-queue seam: the analyze stage sets it,
// the enqueued job carries it, and the merge stage gets it back.
func TestMergeJobCarriesTraditionalFlagToMergeStage(t *testing.T) {
	q, jobs := captureMergeJobs(t)
	service := newStubService(t, q, traditionalAnalyzeStage{})
	contexts := make(chan *ProcessingContext, 1)
	service.pipelines.mergePipeline = NewPipeline("merge").AddProcessor(recordingProcessor{contexts: contexts})

	if err := service.ProcessMedia(context.Background(), showParams); err != nil {
		t.Fatalf("ProcessMedia: %v", err)
	}
	job := nextMergeJob(t, jobs)
	if !job.NeedsConversion {
		t.Fatal("enqueued merge job does not carry the Traditional flag")
	}
	if err := service.ProcessMergeJob(context.Background(), job); err != nil {
		t.Fatalf("ProcessMergeJob: %v", err)
	}
	if pctx := nextContext(t, contexts); !pctx.NeedsConversion {
		t.Error("merge-stage context lost the Traditional flag")
	}
}

// Both hand-off mappings are complete: every exported MergeJob field outside
// the callback-owned and queue-lifecycle ones is filled from a fully populated
// analyze context and survives the round trip. A new hand-off field added to
// MergeJob without mapping it fails here.
func TestMergeJobHandoffMapsEveryField(t *testing.T) {
	pctx := &ProcessingContext{
		JobID:            "job-1",
		VideoPath:        "/media/Show.S01E01.mkv",
		MediaTitle:       "Show",
		MediaType:        MediaTypeEpisode,
		EnglishSubPath:   "/tmp/eng.srt",
		ChineseSubPath:   "/tmp/chs.srt",
		ChineseSubSource: ChineseSourceExtracted,
		NeedsConversion:  true,
	}
	job := newMergeJob(pctx, "req-1")

	// ParentJobID belongs to the callback path.
	excluded := map[string]bool{
		"ParentJobID": true,
	}
	value := reflect.ValueOf(job).Elem()
	typ := value.Type()
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.IsExported() && !excluded[field.Name] && value.Field(i).IsZero() {
			t.Errorf("MergeJob.%s is not mapped from the analyze context", field.Name)
		}
	}
	if job.RequestID != "req-1" {
		t.Errorf("MergeJob.RequestID = %q, want the request that queued the job", job.RequestID)
	}

	back := newMergeProcessingContext(job)
	if back.JobID != pctx.JobID || back.VideoPath != pctx.VideoPath ||
		back.EnglishSubPath != pctx.EnglishSubPath || back.ChineseSubPath != pctx.ChineseSubPath ||
		back.MediaTitle != pctx.MediaTitle || back.MediaType != pctx.MediaType ||
		back.ChineseSubSource != pctx.ChineseSubSource || back.NeedsConversion != pctx.NeedsConversion {
		t.Errorf("merge-stage context does not round-trip the analyze context:\n got %+v\nwant %+v", back, pctx)
	}
	if back.Metadata == nil {
		t.Error("merge-stage context has no metadata map")
	}
}

// The translation callback has no analyze context and keeps building its job
// directly, with the same observable values and without conversion.
func TestProcessWithSubtitlesJobValuesUnchanged(t *testing.T) {
	q, jobs := captureMergeJobs(t)
	service := newStubService(t, q, stubAnalyzingProcessor{})

	ctx := logger.WithRequestID(context.Background(), "req-callback")
	if err := service.ProcessWithSubtitles(ctx, "/media/v.mkv", "/subs/eng.srt", "/subs/chs.srt", "translation-job"); err != nil {
		t.Fatalf("ProcessWithSubtitles: %v", err)
	}
	job := nextMergeJob(t, jobs)
	if job.JobID == "" || job.JobID == "translation-job" {
		t.Errorf("callback job id = %q, want a new job id", job.JobID)
	}
	if job.ParentJobID != "translation-job" || job.RequestID != "req-callback" {
		t.Errorf("callback correlation = parent %q request %q", job.ParentJobID, job.RequestID)
	}
	if job.VideoPath != "/media/v.mkv" || job.EnglishPath != "/subs/eng.srt" || job.ChinesePath != "/subs/chs.srt" {
		t.Errorf("callback paths changed: %+v", job)
	}
	if job.MediaTitle != "/media/v.mkv" || job.MediaType != MediaTypeCallback {
		t.Errorf("callback media fields = title %q type %q", job.MediaTitle, job.MediaType)
	}
	if job.ChineseSubSource != ChineseSourceTranslated {
		t.Errorf("callback Chinese sub source = %q", job.ChineseSubSource)
	}
	if job.NeedsConversion {
		t.Error("callback job must not request conversion")
	}
}

// openccConverter is the OpenCC seam of the LLM merger.
type openccConverter func(ctx context.Context, inputPath, outputPath, configName string) error

// convertTraditionalStub replaces 後 with 后, standing in for a working OpenCC.
func convertTraditionalStub(_ context.Context, inputPath, outputPath, _ string) error {
	data, err := os.ReadFile(inputPath)
	if err != nil {
		return err
	}
	return os.WriteFile(outputPath, []byte(strings.ReplaceAll(string(data), "後", "后")), 0644)
}

// failingConverterStub stands in for a broken OpenCC binary.
func failingConverterStub(context.Context, string, string, string) error {
	return errors.New("opencc failed")
}

// llmTraditionalFixture writes a Traditional Chinese track and builds an LLM
// merger that runs convert as its OpenCC step. It returns the merge job that
// carries the Traditional flag and the merger to run it with.
func llmTraditionalFixture(t *testing.T, conversionEnabled bool, convert openccConverter) (*queue.MergeJob, *MergerProcessor) {
	t.Helper()
	videoPath, enPath, zhPath := mergerFixture(t)
	if err := os.WriteFile(zhPath, []byte(srtFixture(
		[2]string{"00:00:01,000 --> 00:00:03,000", "你好"},
		[2]string{"00:00:04,000 --> 00:00:06,000", "三天後"},
	)), 0644); err != nil {
		t.Fatalf("write Traditional fixture: %v", err)
	}

	merger := newLLMTestProcessor(t, llmMergerTestConfig(t, pairingHandler(t)))
	merger.probe = func(context.Context, string) (*executor.FFProbeOutput, error) { return nil, errProbeUnavailable }
	merger.openccConfig = config.OpenCCConfig{Enabled: conversionEnabled, Config: "t2s.json"}
	merger.convertOpenCC = convert

	return &queue.MergeJob{
		JobID:            "j-traditional",
		VideoPath:        videoPath,
		MediaTitle:       "Episode",
		MediaType:        MediaTypeEpisode,
		EnglishPath:      enPath,
		ChinesePath:      zhPath,
		ChineseSubSource: ChineseSourceExtracted,
		NeedsConversion:  true,
	}, merger
}

func mergedContentFromJob(t *testing.T, conversionEnabled bool, convert openccConverter) (string, *ProcessingContext) {
	t.Helper()
	job, merger := llmTraditionalFixture(t, conversionEnabled, convert)
	service := newStubService(t, idleQueue(), stubAnalyzingProcessor{})
	contexts := make(chan *ProcessingContext, 1)
	service.pipelines.mergePipeline = NewPipeline("merge").
		AddProcessor(merger).
		AddProcessor(recordingProcessor{contexts: contexts})

	if err := service.ProcessMergeJob(context.Background(), job); err != nil {
		t.Fatalf("ProcessMergeJob: %v", err)
	}
	pctx := nextContext(t, contexts)
	content, err := os.ReadFile(pctx.MergedSubPath)
	if err != nil {
		t.Fatalf("read merged subtitles: %v", err)
	}
	return string(content), pctx
}

// After the fix, a merge job that carries the Traditional flag renders
// converted text with the LLM engine.
func TestMergeJobConvertsTraditionalTextWithLLMEngine(t *testing.T) {
	content, pctx := mergedContentFromJob(t, true, convertTraditionalStub)
	if !strings.Contains(content, "三天后") || strings.Contains(content, "三天後") {
		t.Fatalf("converted text not rendered:\n%s", content)
	}
	if !pctx.ConversionApplied {
		t.Error("LLM conversion was not recorded as applied")
	}
}

// A Traditional job still succeeds with the original text when OpenCC is off.
func TestMergeJobKeepsTextWhenConversionDisabled(t *testing.T) {
	content, pctx := mergedContentFromJob(t, false, convertTraditionalStub)
	if !strings.Contains(content, "三天後") {
		t.Fatalf("original Traditional text missing:\n%s", content)
	}
	if pctx.ConversionApplied {
		t.Error("conversion recorded as applied although OpenCC is disabled")
	}
}

// A failing OpenCC does not fail the job: the merge still succeeds and renders
// the original Traditional text.
func TestMergeJobKeepsTextWhenConversionFails(t *testing.T) {
	buf := captureLogs(t)
	content, pctx := mergedContentFromJob(t, true, failingConverterStub)
	if !strings.Contains(content, "三天後") {
		t.Fatalf("original Traditional text missing after a conversion failure:\n%s", content)
	}
	if pctx.ConversionApplied {
		t.Error("conversion recorded as applied after a failure")
	}
	if !strings.Contains(buf.String(), "event=conversion_failed") {
		t.Errorf("no conversion_failed warning:\n%s", buf.String())
	}
}
