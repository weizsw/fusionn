package subtitle

import (
	"context"
	"errors"
	"strings"
	"testing"

	"go.uber.org/zap/zapcore"

	"github.com/fusionn/internal/executor"
	"github.com/fusionn/pkg/logger"
)

func captureLogs(t *testing.T) *lockedBuffer {
	t.Helper()
	t.Setenv("LOG_LEVEL", "")
	buf := &lockedBuffer{}
	logger.Configure(logger.Config{Output: zapcore.AddSync(buf)})
	t.Cleanup(func() { logger.Configure(logger.Config{Output: zapcore.AddSync(&lockedBuffer{})}) })
	return buf
}

// Webhooks analyze in parallel, so a track warning must name its job.
func TestUnknownChineseTypeWarningNamesJob(t *testing.T) {
	buf := captureLogs(t)
	ctx := logger.WithJobID(context.Background(), "j1")

	track := newAnalyzer().detectChineseSubtitle(ctx, []executor.StreamInfo{chineseStream(2, "Chinese", nil, "", "")})
	if track == nil || track.Priority != 3 {
		t.Fatalf("track = %+v, want the priority-3 fallback", track)
	}
	if !strings.Contains(buf.String(), "event=chinese_type_unknown job_id=j1") {
		t.Errorf("warning does not name the job:\n%s", buf.String())
	}
}

func TestResolutionProbeWarningNamesJob(t *testing.T) {
	buf := captureLogs(t)
	ctx := logger.WithJobID(context.Background(), "j1")
	processor := &MergerProcessor{probe: func(context.Context, string) (*executor.FFProbeOutput, error) {
		return nil, errors.New("no probe")
	}}

	if w, h := processor.videoDimensions(ctx, "/media/v.mkv"); w != 0 || h != 0 {
		t.Fatalf("dimensions = %dx%d, want unknown", w, h)
	}
	if !strings.Contains(buf.String(), "event=resolution_probe_failed job_id=j1") {
		t.Errorf("warning does not name the job:\n%s", buf.String())
	}
}
