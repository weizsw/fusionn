package subtitle

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"

	"go.uber.org/zap/zapcore"

	"github.com/fusionn/pkg/logger"
)

// namedProcessor is a pipeline step that runs (or skips) and returns err.
type namedProcessor struct {
	name string
	skip bool
	err  error
}

func (p namedProcessor) Name() string                                      { return p.name }
func (p namedProcessor) ShouldRun(*ProcessingContext) bool                 { return !p.skip }
func (p namedProcessor) Process(context.Context, *ProcessingContext) error { return p.err }

func captureDebugLogs(t *testing.T) *lockedBuffer {
	t.Helper()
	t.Setenv("LOG_LEVEL", "DEBUG")
	buf := &lockedBuffer{}
	logger.Configure(logger.Config{Output: zapcore.AddSync(buf)})
	t.Cleanup(func() { logger.Configure(logger.Config{Output: zapcore.AddSync(&lockedBuffer{})}) })
	return buf
}

// A pipeline runs one stage of a job. Its start and end lines carry that
// stage, and each processor is a step of it, so every line of the stage has
// the same stage value as the job's own events.
func TestPipelineEventsNameTheStageAndTheStep(t *testing.T) {
	buf := captureDebugLogs(t)
	ctx := logger.WithJobID(context.Background(), "j1")
	pipeline := NewPipeline("merge").
		AddProcessor(namedProcessor{name: "style", skip: true}).
		AddProcessor(namedProcessor{name: "merger"})

	if err := pipeline.Execute(ctx, &ProcessingContext{}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"INFO component=pipeline event=stage_started job_id=j1 stage=merge msg=",
		"DEBUG component=pipeline event=step_skipped job_id=j1 stage=merge step=style msg=",
		"INFO component=pipeline event=step_started job_id=j1 stage=merge step=merger msg=",
		"INFO component=pipeline event=step_completed job_id=j1 stage=merge duration_ms=",
		"INFO component=pipeline event=stage_completed job_id=j1 stage=merge duration_ms=",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "pipeline_started") || strings.Contains(out, "pipeline_completed") {
		t.Errorf("old pipeline events are still logged:\n%s", out)
	}
}

// A failed step is a DEBUG step_failed; the stage does not complete.
func TestPipelineFailureNamesTheStep(t *testing.T) {
	buf := captureDebugLogs(t)
	ctx := logger.WithJobID(context.Background(), "j1")
	pipeline := NewPipeline("analyze").AddProcessor(namedProcessor{name: "bazarr_search", err: errors.New("boom")})

	if err := pipeline.Execute(ctx, &ProcessingContext{}); err == nil {
		t.Fatal("expected the step failure")
	}
	out := buf.String()
	if !strings.Contains(out, `DEBUG component=pipeline event=step_failed job_id=j1 stage=analyze error="boom" step=bazarr_search msg=`) {
		t.Errorf("no step_failed line in:\n%s", out)
	}
	if strings.Contains(out, "event=stage_completed") {
		t.Errorf("a failed stage must not complete:\n%s", out)
	}
}

// Processor names are the step values, so they follow the field value style.
func TestProcessorNamesAreSnakeCase(t *testing.T) {
	names := []string{
		ProcessorNameAnalyzer, ProcessorNameExtractor, ProcessorNameConversion, ProcessorNameMerger,
		ProcessorNameStyle, ProcessorNameFontEmbedding, ProcessorNameOutput, ProcessorNameNotification,
		ProcessorNameCleanup, ProcessorNameSDHFilter, ProcessorNameBazarrSDHFilter,
		ProcessorNameTranslationQueue, ProcessorNameBazarrSearch, ProcessorNameDualLanguage,
	}
	snake := regexp.MustCompile(`^[a-z][a-z0-9]*(_[a-z0-9]+)*$`)
	seen := map[string]bool{}
	for _, name := range names {
		if !snake.MatchString(name) || seen[name] {
			t.Errorf("processor name %q is not unique snake_case", name)
		}
		seen[name] = true
	}
}
