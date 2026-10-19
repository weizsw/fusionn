package subtitle

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fusionn/internal/client/bazarr"
	"github.com/fusionn/internal/config"
	"github.com/fusionn/internal/executor"
	"github.com/fusionn/internal/queue"
)

func newServiceForEngine(t *testing.T, subtitleCfg config.SubtitleConfig) *Service {
	t.Helper()
	mergeQueue := queue.NewMergeQueue(queue.Config{}, func(context.Context, *queue.MergeJob) error {
		return nil
	})
	service, err := NewService(&config.Config{Subtitle: subtitleCfg}, nil, mergeQueue, bazarr.NewClient("http://bazarr", "key", 1))
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return service
}

func processorNames(pipeline *Pipeline) []string {
	names := make([]string, 0, len(pipeline.GetProcessors()))
	for _, proc := range pipeline.GetProcessors() {
		names = append(names, proc.Name())
	}
	return names
}

func hasProcessorName(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}

func TestNewServiceLLMModeOmitsSDHProcessors(t *testing.T) {
	service := newServiceForEngine(t, config.SubtitleConfig{
		Engine:  config.EngineLLM,
		LLM:     config.LLMConfig{BaseURL: "http://127.0.0.1:1/v1", Model: "m"},
		DuoSubs: config.DuoSubsConfig{Mode: "http"}, // must not be initialized
	})
	names := processorNames(service.GetAnalyzePipeline())
	if hasProcessorName(names, ProcessorNameSDHFilter) {
		t.Fatalf("LLM mode must keep uncertain one-sided lines; analyze pipeline = %v", names)
	}
	if hasProcessorName(names, ProcessorNameBazarrSDHFilter) {
		t.Fatalf("LLM mode must not run Bazarr SDH filtering; analyze pipeline = %v", names)
	}

	mergeNames := processorNames(service.GetMergePipeline())
	if hasProcessorName(mergeNames, ProcessorNameConversion) {
		t.Fatalf("LLM mode converts inside the merger; merge pipeline = %v", mergeNames)
	}
	if !hasProcessorName(mergeNames, ProcessorNameMerger) || !hasProcessorName(mergeNames, ProcessorNameDualLanguage) {
		t.Fatalf("LLM merge pipeline = %v", mergeNames)
	}
}

func TestNewServiceDuoSubsModeKeepsSDHProcessors(t *testing.T) {
	service := newServiceForEngine(t, config.SubtitleConfig{
		Engine:  config.EngineDuoSubs,
		DuoSubs: config.DuoSubsConfig{Mode: "local"},
	})
	names := processorNames(service.GetAnalyzePipeline())
	for _, want := range []string{ProcessorNameSDHFilter, ProcessorNameBazarrSDHFilter} {
		if !hasProcessorName(names, want) {
			t.Fatalf("DuoSubs analyze pipeline missing %s: %v", want, names)
		}
	}
	mergeNames := processorNames(service.GetMergePipeline())
	if !hasProcessorName(mergeNames, ProcessorNameConversion) {
		t.Fatalf("DuoSubs merge pipeline missing conversion: %v", mergeNames)
	}
}

func TestNewServiceLLMModeAnalyzerPreservesNativeASS(t *testing.T) {
	service := newServiceForEngine(t, config.SubtitleConfig{
		Engine: config.EngineLLM,
		LLM:    config.LLMConfig{BaseURL: "http://127.0.0.1:1/v1", Model: "m"},
	})
	processor, ok := service.GetAnalyzePipeline().GetProcessors()[0].(*AnalyzerProcessor)
	if !ok {
		t.Fatal("first analyze processor is not the analyzer")
	}
	if !processor.analyzer.preserveNativeASS {
		t.Fatal("LLM mode did not enable native ASS preservation")
	}

	duo := newServiceForEngine(t, config.SubtitleConfig{
		Engine:  config.EngineDuoSubs,
		DuoSubs: config.DuoSubsConfig{Mode: "local"},
	})
	duoProcessor := duo.GetAnalyzePipeline().GetProcessors()[0].(*AnalyzerProcessor)
	if duoProcessor.analyzer.preserveNativeASS {
		t.Fatal("DuoSubs mode must keep SRT extraction")
	}
}

func TestAnalyzerExtractionModeSelection(t *testing.T) {
	var nativeCalls, srtCalls int
	originalNative, originalSRT := extractSubtitleNativeFunc, extractSubtitleFunc
	t.Cleanup(func() {
		extractSubtitleNativeFunc = originalNative
		extractSubtitleFunc = originalSRT
	})
	extractSubtitleNativeFunc = func(_ context.Context, _ string, _ int, outputPath string) error {
		nativeCalls++
		return os.WriteFile(outputPath, []byte("[Script Info]\n"), 0644)
	}
	extractSubtitleFunc = func(_ context.Context, _ string, _ int, outputPath string) error {
		srtCalls++
		return os.WriteFile(outputPath, []byte("1\n"), 0644)
	}

	dir := t.TempDir()
	videoPath := filepath.Join(dir, "Episode.mkv")
	if err := os.WriteFile(videoPath, []byte("video"), 0644); err != nil {
		t.Fatal(err)
	}
	result := func(codec string) *AnalysisResult {
		return &AnalysisResult{
			VideoPath:    videoPath,
			EnglishTrack: &Track{Index: 2, CodecName: codec},
			ChineseTrack: &Track{Index: 3, CodecName: codec},
		}
	}
	assertUniqueTemp := func(t *testing.T, path, suffix, ext string) {
		t.Helper()
		base := filepath.Base(path)
		wantSuffix := "." + suffix + "." + ext
		if !strings.HasPrefix(base, ".fusionn-extract-Episode-") || !strings.HasSuffix(base, wantSuffix) {
			t.Fatalf("extraction path %q does not match .fusionn-extract-Episode-*%s", base, wantSuffix)
		}
		if filepath.Dir(path) != dir {
			t.Fatalf("extraction path %q is outside the media dir %q", path, dir)
		}
	}

	analyzer := NewAnalyzer(nil, nil, nil, nil)
	analyzer.SetPreserveNativeASS(true)
	analysis := result("ass")
	if err := analyzer.ExtractSubtitles(context.Background(), analysis); err != nil {
		t.Fatalf("ExtractSubtitles: %v", err)
	}
	if nativeCalls != 2 || srtCalls != 0 {
		t.Fatalf("native=%d srt=%d, want 2/0", nativeCalls, srtCalls)
	}
	assertUniqueTemp(t, analysis.EnglishTrack.ExtractedPath, "eng", "ass")
	assertUniqueTemp(t, analysis.ChineseTrack.ExtractedPath, "chs", "ass")
	if analysis.EnglishTrack.ExtractedPath == analysis.ChineseTrack.ExtractedPath {
		t.Fatal("English and Chinese extraction paths are not unique")
	}

	// SRT streams still transcode in LLM mode.
	analysis = result("subrip")
	if err := analyzer.ExtractSubtitles(context.Background(), analysis); err != nil {
		t.Fatalf("ExtractSubtitles: %v", err)
	}
	if nativeCalls != 2 || srtCalls != 2 {
		t.Fatalf("native=%d srt=%d, want 2/2", nativeCalls, srtCalls)
	}
	assertUniqueTemp(t, analysis.EnglishTrack.ExtractedPath, "eng", "srt")
	assertUniqueTemp(t, analysis.ChineseTrack.ExtractedPath, "chs", "srt")

	// DuoSubs mode keeps deterministic sidecar names and SRT extraction.
	analysis = result("ass")
	duoAnalyzer := NewAnalyzer(nil, nil, nil, nil)
	if err := duoAnalyzer.ExtractSubtitles(context.Background(), analysis); err != nil {
		t.Fatalf("ExtractSubtitles: %v", err)
	}
	if nativeCalls != 2 || srtCalls != 4 {
		t.Fatalf("native=%d srt=%d, want 2/4", nativeCalls, srtCalls)
	}
	if analysis.EnglishTrack.ExtractedPath != filepath.Join(dir, "Episode.eng.srt") {
		t.Fatalf("duo extraction path = %q, want deterministic sidecar", analysis.EnglishTrack.ExtractedPath)
	}
	if analysis.ChineseTrack.ExtractedPath != filepath.Join(dir, "Episode.chs.srt") {
		t.Fatalf("duo extraction path = %q, want deterministic sidecar", analysis.ChineseTrack.ExtractedPath)
	}
}

// TestAnalyzerLLMExtractionNeverTouchesExistingSidecars verifies the unique
// temp extraction paths even when every conventional sidecar name is taken.
func TestAnalyzerLLMExtractionNeverTouchesExistingSidecars(t *testing.T) {
	dir := t.TempDir()
	videoPath := filepath.Join(dir, "Episode.mkv")
	if err := os.WriteFile(videoPath, []byte("video"), 0644); err != nil {
		t.Fatal(err)
	}
	sidecars := map[string]string{
		"Episode.eng.srt": "original eng srt",
		"Episode.chs.srt": "original chs srt",
		"Episode.eng.ass": "original eng ass",
		"Episode.chs.ass": "original chs ass",
	}
	for name, content := range sidecars {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	originalNative, originalSRT := extractSubtitleNativeFunc, extractSubtitleFunc
	t.Cleanup(func() {
		extractSubtitleNativeFunc = originalNative
		extractSubtitleFunc = originalSRT
	})
	extractSubtitleNativeFunc = func(_ context.Context, _ string, _ int, outputPath string) error {
		return os.WriteFile(outputPath, []byte("[Script Info]\n"), 0644)
	}
	extractSubtitleFunc = func(_ context.Context, _ string, _ int, outputPath string) error {
		return os.WriteFile(outputPath, []byte("1\n"), 0644)
	}

	analyzer := NewAnalyzer(nil, nil, nil, nil)
	analyzer.SetPreserveNativeASS(true)
	for run := 0; run < 2; run++ {
		analysis := &AnalysisResult{
			VideoPath:    videoPath,
			EnglishTrack: &Track{Index: 2, CodecName: "ass"},
			ChineseTrack: &Track{Index: 3, CodecName: "ass"},
		}
		if err := analyzer.ExtractSubtitles(context.Background(), analysis); err != nil {
			t.Fatalf("run %d ExtractSubtitles: %v", run, err)
		}
		for _, path := range []string{analysis.EnglishTrack.ExtractedPath, analysis.ChineseTrack.ExtractedPath} {
			if _, exists := sidecars[filepath.Base(path)]; exists {
				t.Fatalf("run %d reused sidecar name %q", run, filepath.Base(path))
			}
		}
		if analysis.EnglishTrack.ExtractedPath == analysis.ChineseTrack.ExtractedPath {
			t.Fatal("English and Chinese paths collide within a run")
		}
		// Cleanup removes exactly the paths handed to it; no directory leftovers.
		analyzer.Cleanup(analysis)
	}
	for name, content := range sidecars {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("existing sidecar %s was removed: %v", name, err)
		}
		if string(data) != content {
			t.Fatalf("existing sidecar %s was modified: %q", name, data)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".fusionn-extract-") {
			t.Fatalf("leftover extraction temp file %q", entry.Name())
		}
	}
}

// TestAnalyzerLLMExtractionFailureLeavesNoTempFiles verifies a failed
// extraction removes its reserved path and does not disturb existing files.
func TestAnalyzerLLMExtractionFailureLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	videoPath := filepath.Join(dir, "Episode.mkv")
	if err := os.WriteFile(videoPath, []byte("video"), 0644); err != nil {
		t.Fatal(err)
	}
	existing := filepath.Join(dir, "Episode.chs.srt")
	if err := os.WriteFile(existing, []byte("keep me"), 0644); err != nil {
		t.Fatal(err)
	}

	originalNative, originalSRT := extractSubtitleNativeFunc, extractSubtitleFunc
	t.Cleanup(func() {
		extractSubtitleNativeFunc = originalNative
		extractSubtitleFunc = originalSRT
	})
	// The Chinese (stream 3) extraction fails after the English one succeeded.
	extractSubtitleNativeFunc = func(_ context.Context, _ string, streamIndex int, outputPath string) error {
		if streamIndex == 3 {
			return errors.New("simulated extraction failure")
		}
		return os.WriteFile(outputPath, []byte("[Script Info]\n"), 0644)
	}
	extractSubtitleFunc = func(_ context.Context, _ string, _ int, outputPath string) error {
		return os.WriteFile(outputPath, []byte("1\n"), 0644)
	}

	analyzer := NewAnalyzer(nil, nil, nil, nil)
	analyzer.SetPreserveNativeASS(true)
	analysis := &AnalysisResult{
		VideoPath:    videoPath,
		EnglishTrack: &Track{Index: 2, CodecName: "ass"},
		ChineseTrack: &Track{Index: 3, CodecName: "ass"},
	}
	err := analyzer.ExtractSubtitles(context.Background(), analysis)
	if err == nil || !strings.Contains(err.Error(), "failed to extract Chinese subtitle") {
		t.Fatalf("ExtractSubtitles() error = %v, want Chinese extraction failure", err)
	}
	if analysis.EnglishTrack.ExtractedPath != "" {
		t.Fatalf("failed run kept English temp path %q", analysis.EnglishTrack.ExtractedPath)
	}
	data, err := os.ReadFile(existing)
	if err != nil || string(data) != "keep me" {
		t.Fatalf("existing sidecar touched: err=%v data=%q", err, data)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".fusionn-extract-") {
			t.Fatalf("failed extraction left temp file %q", entry.Name())
		}
	}
}

func TestLLMMergerCreatesAndUsesPrivateConversionCopy(t *testing.T) {
	videoPath, enPath, zhPath := mergerFixture(t)
	// Write Traditional Chinese text so the conversion is observable.
	zhPath = writeSubtitleFixture(t, filepath.Dir(videoPath), "Episode.chs.srt", srtFixture(
		[2]string{"00:00:01,000 --> 00:00:03,000", "你好"},
		[2]string{"00:00:04,000 --> 00:00:06,000", "三天後"},
	))
	original, err := os.ReadFile(zhPath)
	if err != nil {
		t.Fatal(err)
	}

	processor := newLLMTestProcessor(t, llmMergerTestConfig(t, pairingHandler(t)))
	processor.probe = func(context.Context, string) (*executor.FFProbeOutput, error) { return nil, errProbeUnavailable }
	processor.openccConfig = config.OpenCCConfig{Enabled: true, Config: "t2s.json"}
	processor.convertOpenCC = func(_ context.Context, inputPath, outputPath, _ string) error {
		data, err := os.ReadFile(inputPath)
		if err != nil {
			return err
		}
		return os.WriteFile(outputPath, []byte(strings.ReplaceAll(string(data), "後", "后")), 0644)
	}

	pctx := &ProcessingContext{
		VideoPath: videoPath, EnglishSubPath: enPath, ChineseSubPath: zhPath,
		NeedsConversion: true, Metadata: map[string]interface{}{},
	}
	if err := processor.Process(context.Background(), pctx); err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	after, err := os.ReadFile(zhPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(original) {
		t.Fatal("original Chinese subtitle was modified")
	}
	convertedPath := filepath.Join(pctx.TempMergeDir, "zh.converted.srt")
	if _, err := os.Stat(convertedPath); err != nil {
		t.Fatalf("private conversion copy missing: %v", err)
	}
	content, err := os.ReadFile(pctx.MergedSubPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "三天后") || strings.Contains(string(content), "三天後") {
		t.Fatalf("converted text not rendered:\n%s", content)
	}
}

var errProbeUnavailable = &probeError{}

type probeError struct{}

func (*probeError) Error() string { return "ffprobe unavailable" }
