package subtitle

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fusionn/internal/config"
	"github.com/fusionn/internal/notification"
)

// The dual-language step already wrote the merged subtitle, so converting the
// unused Chinese track would only be wasted work.
func TestConversionProcessorSkipsWhenMergedSubPathSet(t *testing.T) {
	processor := NewConversionProcessor(config.OpenCCConfig{Enabled: true, Config: "t2s.json"})
	pctx := &ProcessingContext{
		ChineseSubPath:  "/tmp/chs.srt",
		MergedSubPath:   "/tmp/dual_combined.ass",
		NeedsConversion: true,
	}
	if processor.ShouldRun(pctx) {
		t.Fatal("conversion ran although the merged subtitle already exists")
	}
}

// A failed conversion is not a job failure: the original Traditional text stays,
// no partial output survives, and the job continues.
func TestConversionProcessorFailureKeepsOriginalText(t *testing.T) {
	buf := captureLogs(t)
	dir := t.TempDir()
	originalPath := filepath.Join(dir, "Episode.chs.srt")
	if err := os.WriteFile(originalPath, []byte("三天後\n"), 0644); err != nil {
		t.Fatal(err)
	}

	processor := NewConversionProcessor(config.OpenCCConfig{Enabled: true, Config: "t2s.json"})
	processor.convertOpenCC = func(_ context.Context, _, outputPath, _ string) error {
		// A failing converter can leave a partial output behind.
		if err := os.WriteFile(outputPath, []byte("三天"), 0644); err != nil {
			return err
		}
		return errors.New("opencc failed")
	}
	pctx := &ProcessingContext{ChineseSubPath: originalPath, NeedsConversion: true}

	if err := processor.Process(context.Background(), pctx); err != nil {
		t.Fatalf("Process() = %v, want nil because conversion fails soft", err)
	}
	if pctx.ChineseSubPath != originalPath {
		t.Errorf("ChineseSubPath = %q, want the original %q", pctx.ChineseSubPath, originalPath)
	}
	if data, err := os.ReadFile(originalPath); err != nil || string(data) != "三天後\n" {
		t.Errorf("original subtitle changed: err=%v data=%q", err, data)
	}
	if _, err := os.Stat(originalPath + ".converted.srt"); !os.IsNotExist(err) {
		t.Errorf("partial converted output left behind: %v", err)
	}
	if !strings.Contains(buf.String(), "event=conversion_failed") {
		t.Errorf("no conversion_failed warning:\n%s", buf.String())
	}
	if !pctx.NeedsConversion {
		t.Error("the Traditional flag was consumed by a failed conversion")
	}
	if pctx.ConversionApplied {
		t.Error("conversion recorded as applied after a failure")
	}
}

// Cancellation is the caller's decision, not a soft conversion failure.
func TestConversionProcessorReturnsContextError(t *testing.T) {
	processor := NewConversionProcessor(config.OpenCCConfig{Enabled: true, Config: "t2s.json"})
	processor.convertOpenCC = func(ctx context.Context, _, _, _ string) error { return ctx.Err() }

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	pctx := &ProcessingContext{ChineseSubPath: filepath.Join(t.TempDir(), "chs.srt"), NeedsConversion: true}

	if err := processor.Process(ctx, pctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Process() = %v, want context.Canceled", err)
	}
}

// A successful conversion still swaps the context to the converted file.
func TestConversionProcessorSuccessSwapsToConvertedFile(t *testing.T) {
	dir := t.TempDir()
	originalPath := filepath.Join(dir, "Episode.chs.srt")
	if err := os.WriteFile(originalPath, []byte("三天後\n"), 0644); err != nil {
		t.Fatal(err)
	}

	processor := NewConversionProcessor(config.OpenCCConfig{Enabled: true, Config: "t2s.json"})
	processor.convertOpenCC = func(_ context.Context, _, outputPath, configName string) error {
		if configName != "t2s.json" {
			t.Errorf("configName = %q, want the configured OpenCC profile", configName)
		}
		return os.WriteFile(outputPath, []byte("三天后\n"), 0644)
	}
	pctx := &ProcessingContext{ChineseSubPath: originalPath, NeedsConversion: true}

	if err := processor.Process(context.Background(), pctx); err != nil {
		t.Fatalf("Process() = %v", err)
	}
	convertedPath := originalPath + ".converted.srt"
	if pctx.ChineseSubPath != convertedPath {
		t.Errorf("ChineseSubPath = %q, want %q", pctx.ChineseSubPath, convertedPath)
	}
	if data, err := os.ReadFile(convertedPath); err != nil || string(data) != "三天后\n" {
		t.Errorf("converted file = %q (err=%v), want converted text", data, err)
	}
	if _, err := os.Stat(originalPath); !os.IsNotExist(err) {
		t.Errorf("original Traditional subtitle not removed: %v", err)
	}
	if !pctx.ConversionApplied {
		t.Error("successful conversion was not recorded as applied")
	}
}

// mergedMarkerProcessor stands in for the merger that produced the output file.
type mergedMarkerProcessor struct{}

func (mergedMarkerProcessor) Name() string                      { return "StubMerged" }
func (mergedMarkerProcessor) ShouldRun(*ProcessingContext) bool { return true }

func (mergedMarkerProcessor) Process(_ context.Context, pctx *ProcessingContext) error {
	pctx.MergedSubPath = "/media/.fusionn-merge/merged.ass"
	return nil
}

// The success notification names a conversion only when one was applied. Both
// runs detect Traditional Chinese and both succeed; only the first converts.
func TestSuccessNotificationReportsOnlyAppliedConversion(t *testing.T) {
	captureLogs(t)
	cases := []struct {
		name     string
		convert  func(ctx context.Context, inputPath, outputPath, configName string) error
		wantLine bool
	}{
		{"applied", func(_ context.Context, _, outputPath, _ string) error {
			return os.WriteFile(outputPath, []byte("三天后\n"), 0644)
		}, true},
		{"failed", func(context.Context, string, string, string) error {
			return errors.New("opencc failed")
		}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			url, sent := appriseRecorder(t)
			dir := t.TempDir()
			chinesePath := filepath.Join(dir, "Episode.chs.srt")
			if err := os.WriteFile(chinesePath, []byte("三天後\n"), 0644); err != nil {
				t.Fatal(err)
			}

			conversion := NewConversionProcessor(config.OpenCCConfig{Enabled: true, Config: "t2s.json"})
			conversion.convertOpenCC = tc.convert
			notifier := NewNotificationProcessor(notification.NewAppriseClient(url, "key", "tag"), true)
			pipeline := NewPipeline("merge").
				AddProcessor(conversion).
				AddProcessor(mergedMarkerProcessor{}).
				AddProcessor(notifier)
			pctx := &ProcessingContext{
				VideoPath:       "/media/Show.S01E01.mkv",
				MediaType:       MediaTypeEpisode,
				ChineseSubPath:  chinesePath,
				NeedsConversion: true,
			}

			if err := pipeline.Execute(context.Background(), pctx); err != nil {
				t.Fatalf("Execute() = %v, want a successful pipeline", err)
			}
			got := sent()
			if len(got) != 1 {
				t.Fatalf("got %d notifications, want 1: %+v", len(got), got)
			}
			if got[0].kind != "success" {
				t.Errorf("notification kind = %q, want success", got[0].kind)
			}
			hasLine := strings.Contains(got[0].body, "Conversion: Traditional → Simplified Chinese")
			if hasLine != tc.wantLine {
				t.Errorf("conversion line present = %v, want %v:\n%s", hasLine, tc.wantLine, got[0].body)
			}
		})
	}
}
