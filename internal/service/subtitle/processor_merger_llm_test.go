package subtitle

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fusionn/internal/config"
	"github.com/fusionn/internal/executor"
	"github.com/fusionn/internal/subtitlellm"
	"github.com/fusionn/pkg/logger"
)

func init() {
	if logger.Log == nil {
		logger.Init(true)
	}
}

// llmMergerTestConfig starts an OpenAI-compatible httptest server that answers
// every window by pairing same-position cue IDs, and returns LLM config
// pointing at it with an isolated persistent cache directory.
func llmMergerTestConfig(t *testing.T, handler http.HandlerFunc) config.LLMConfig {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return config.LLMConfig{
		BaseURL:        server.URL + "/v1",
		Model:          "test-model",
		ResponseFormat: config.ResponseFormatJSONObject,
		TimeoutSeconds: 5,
		Workers:        2,
		Retries:        0,
		WindowSeconds:  210,
		OverlapSeconds: 15,
		CacheEnabled:   true,
		CacheDir:       t.TempDir(),
		EnChars:        65,
		ZhChars:        25,
		EnGapMS:        400,
		ZhGapMS:        500,
		SpanMS:         5500,
		FillGapMS:      500,
	}
}

// pairingHandler aligns every EN and ZH cue at the same list position.
func pairingHandler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode payload: %v", err)
			return
		}
		view := struct {
			EN []struct {
				ID int `json:"id"`
			} `json:"en"`
			ZH []struct {
				ID int `json:"id"`
			} `json:"zh"`
		}{}
		if err := json.Unmarshal([]byte(payload.Messages[1].Content), &view); err != nil {
			t.Errorf("decode view: %v", err)
			return
		}
		alignments := make([]map[string]any, 0, len(view.EN))
		for i := range view.EN {
			zhIDs := []int{}
			if i < len(view.ZH) {
				zhIDs = append(zhIDs, view.ZH[i].ID)
			}
			alignments = append(alignments, map[string]any{
				"en_ids": []int{view.EN[i].ID}, "zh_ids": zhIDs, "layer": "bottom",
			})
		}
		content, _ := json.Marshal(map[string]any{"alignments": alignments})
		_ = json.NewEncoder(w).Encode(map[string]any{
			"model": "test-model",
			"choices": []any{map[string]any{
				"finish_reason": "stop",
				"message":       map[string]any{"content": string(content)},
			}},
		})
	}
}

func writeSubtitleFixture(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func srtFixture(entries ...[2]string) string {
	var b strings.Builder
	for i, entry := range entries {
		b.WriteString(strconv.Itoa(i + 1))
		b.WriteString("\n")
		b.WriteString(entry[0])
		b.WriteString("\n")
		b.WriteString(entry[1])
		b.WriteString("\n\n")
	}
	return b.String()
}

// mergerFixture creates a video-named directory with English and Chinese SRTs.
func mergerFixture(t *testing.T) (videoPath, enPath, zhPath string) {
	t.Helper()
	dir := t.TempDir()
	videoPath = filepath.Join(dir, "Episode.mkv")
	if err := os.WriteFile(videoPath, []byte("synthetic video"), 0644); err != nil {
		t.Fatalf("write video: %v", err)
	}
	enPath = writeSubtitleFixture(t, dir, "Episode.eng.srt", srtFixture(
		[2]string{"00:00:01,000 --> 00:00:03,000", "Hello"},
		[2]string{"00:00:04,000 --> 00:00:06,000", "[phone ringing]"},
	))
	zhPath = writeSubtitleFixture(t, dir, "Episode.chs.srt", srtFixture(
		[2]string{"00:00:01,000 --> 00:00:03,000", "你好"},
		[2]string{"00:00:04,000 --> 00:00:06,000", "【三天后】"},
	))
	return videoPath, enPath, zhPath
}

func llmStyleConfig() config.ASSStyleConfig {
	return config.ASSStyleConfig{
		Enabled: true, PlayResX: 1920, PlayResY: 1080,
		PrimaryFont: "PingFang SC", PrimarySize: 52, PrimaryColor: "&H08EFECEA",
		SecondaryFont: "Helvetica Neue", SecondarySize: 36, SecondaryColor: "&H087BC0E5",
		Outline: 1.4, OutlineColor: "&H30141414", Shadow: 1.5, BackColor: "&H80000000",
		MarginV: 32, MarginLeft: 80, MarginRight: 80, Alignment: 2, BorderStyle: 1,
		WrapStyle: "0", ScaleX: 100, LineHeightRatio: 1.23, BlockPadding: 16, CollisionGap: 12,
	}
}

func newLLMTestProcessor(t *testing.T, llm config.LLMConfig) *MergerProcessor {
	t.Helper()
	processor, err := NewConfiguredMergerProcessor(config.SubtitleConfig{
		Engine:   config.EngineLLM,
		LLM:      llm,
		ASSStyle: llmStyleConfig(),
		DuoSubs:  config.DuoSubsConfig{Mode: "http", HTTPURL: "http://127.0.0.1:1"},
	})
	if err != nil {
		t.Fatalf("NewConfiguredMergerProcessor: %v", err)
	}
	return processor
}

func TestConfiguredMergerProcessorSelectsOnlyEngineDependency(t *testing.T) {
	t.Run("llm never initializes duosubs", func(t *testing.T) {
		// The DuoSubs config is intentionally invalid for HTTP mode. Selecting
		// the LLM engine must not read or validate it.
		processor, err := NewConfiguredMergerProcessor(config.SubtitleConfig{
			Engine:  config.EngineLLM,
			DuoSubs: config.DuoSubsConfig{Mode: "http"},
		})
		if err != nil {
			t.Fatalf("NewConfiguredMergerProcessor() error = %v", err)
		}
		if processor.duosubsExecutor != nil {
			t.Fatal("LLM engine initialized a DuoSubs executor")
		}
		if processor.llmEngine == nil {
			t.Fatal("LLM engine was not initialized")
		}
	})

	t.Run("duosubs does not initialize llm", func(t *testing.T) {
		processor, err := NewConfiguredMergerProcessor(config.SubtitleConfig{
			Engine:  config.EngineDuoSubs,
			DuoSubs: config.DuoSubsConfig{Mode: "local"},
		})
		if err != nil {
			t.Fatalf("NewConfiguredMergerProcessor() error = %v", err)
		}
		if processor.llmEngine != nil {
			t.Fatal("DuoSubs engine initialized an LLM engine")
		}
		if processor.duosubsExecutor == nil {
			t.Fatal("DuoSubs executor was not initialized")
		}
	})

	t.Run("empty engine keeps legacy duosubs", func(t *testing.T) {
		processor, err := NewConfiguredMergerProcessor(config.SubtitleConfig{
			DuoSubs: config.DuoSubsConfig{Mode: "local"},
		})
		if err != nil {
			t.Fatalf("NewConfiguredMergerProcessor() error = %v", err)
		}
		if processor.engine != config.EngineDuoSubs {
			t.Fatalf("engine = %q, want %q", processor.engine, config.EngineDuoSubs)
		}
	})

	t.Run("unknown engine is rejected", func(t *testing.T) {
		if _, err := NewConfiguredMergerProcessor(config.SubtitleConfig{Engine: "bogus"}); err == nil {
			t.Fatal("expected an error for an unknown engine")
		}
	})
}

func TestLLMMergerProcessWritesArtifactsAndMarksStyled(t *testing.T) {
	videoPath, enPath, zhPath := mergerFixture(t)
	llm := llmMergerTestConfig(t, pairingHandler(t))
	processor := newLLMTestProcessor(t, llm)
	probed := 0
	processor.probe = func(context.Context, string) (*executor.FFProbeOutput, error) {
		probed++
		return nil, errors.New("ffprobe unavailable")
	}

	pctx := &ProcessingContext{
		VideoPath:      videoPath,
		EnglishSubPath: enPath,
		ChineseSubPath: zhPath,
		Metadata:       map[string]interface{}{},
	}
	if err := processor.Process(context.Background(), pctx); err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if probed != 1 {
		t.Fatalf("probe calls = %d, want 1", probed)
	}
	if pctx.Metadata[subtitlellm.MetadataLLMStyled] != true {
		t.Fatal("llm_styled metadata was not set")
	}
	if pctx.Metadata[MetadataLLMSourcesPath] == nil || pctx.Metadata[MetadataLLMAuditPath] == nil || pctx.Metadata[MetadataLLMMappingPath] == nil {
		t.Fatal("artifacts were not persisted with cache enabled")
	}
	if pctx.TempMergeDir == "" || !strings.HasPrefix(filepath.Base(pctx.TempMergeDir), ".fusionn-merge-") {
		t.Fatalf("temp merge dir = %q", pctx.TempMergeDir)
	}
	if filepath.Dir(pctx.MergedSubPath) != pctx.TempMergeDir {
		t.Fatalf("merged path %q is outside the isolated merge dir %q", pctx.MergedSubPath, pctx.TempMergeDir)
	}
	content, err := os.ReadFile(pctx.MergedSubPath)
	if err != nil {
		t.Fatalf("read merged output: %v", err)
	}
	for _, want := range []string{
		subtitlellm.HeaderMarker, "Style: Default,", "Style: Eng,",
		"你好", "Hello", "【三天后】", "[phone ringing]",
	} {
		if !strings.Contains(string(content), want) {
			t.Fatalf("merged output missing %q:\n%s", want, content)
		}
	}

	// The persistent cache directory keeps mapping/audit/sources even after
	// the merge directory is removed by the cleanup processor.
	mappingPath, _ := pctx.Metadata[MetadataLLMMappingPath].(string)
	auditPath, _ := pctx.Metadata[MetadataLLMAuditPath].(string)
	sourcesPath, _ := pctx.Metadata[MetadataLLMSourcesPath].(string)
	for name, path := range map[string]string{"mapping": mappingPath, "audit": auditPath, "sources": sourcesPath} {
		if path == "" {
			t.Fatalf("%s artifact path is not set", name)
		}
		if !strings.HasPrefix(path, llm.CacheDir) {
			t.Fatalf("%s artifact %q is not under persistent cache %q", name, path, llm.CacheDir)
		}
	}
	// Prototype-compatible layout: sources.json sits next to alignment/ so the
	// mapping is directly re-importable via mapping_path.
	if filepath.Dir(mappingPath) != filepath.Join(filepath.Dir(sourcesPath), "alignment") {
		t.Fatalf("mapping %q is not under alignment/ next to sources %q", mappingPath, sourcesPath)
	}
	if filepath.Dir(auditPath) != filepath.Dir(mappingPath) {
		t.Fatalf("audit %q is not next to mapping %q", auditPath, mappingPath)
	}

	if err := NewCleanupProcessor().Process(context.Background(), pctx); err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	for name, path := range map[string]string{"mapping": mappingPath, "audit": auditPath, "sources": sourcesPath} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("%s artifact did not survive cleanup: %v", name, err)
		}
	}

	// Artifacts never contain credentials or the raw model payload.
	for _, path := range []string{mappingPath, auditPath, sourcesPath} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read artifact %s: %v", path, err)
		}
		lower := strings.ToLower(string(data))
		for _, secret := range []string{"api_key", "authorization", "bearer"} {
			if strings.Contains(lower, secret) {
				t.Fatalf("artifact %s contains secret-like field %q", path, secret)
			}
		}
	}

	var audit subtitlellm.Audit
	data, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &audit); err != nil {
		t.Fatalf("parse audit: %v", err)
	}
	if audit.Coverage.EN != 2 || audit.Coverage.ZH != 2 {
		t.Fatalf("audit coverage = %+v", audit.Coverage)
	}
	if audit.Provenance == nil || audit.Provenance.Source != "native" {
		t.Fatalf("audit provenance = %+v", audit.Provenance)
	}
}

func TestLLMMergerStyleProcessorPreservesRenderedOutput(t *testing.T) {
	videoPath, enPath, zhPath := mergerFixture(t)
	processor := newLLMTestProcessor(t, llmMergerTestConfig(t, pairingHandler(t)))
	processor.probe = func(context.Context, string) (*executor.FFProbeOutput, error) {
		return nil, errors.New("ffprobe unavailable")
	}

	pctx := &ProcessingContext{VideoPath: videoPath, EnglishSubPath: enPath, ChineseSubPath: zhPath, Metadata: map[string]interface{}{}}
	if err := processor.Process(context.Background(), pctx); err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	before, err := os.ReadFile(pctx.MergedSubPath)
	if err != nil {
		t.Fatal(err)
	}

	style := NewStyleProcessor(llmStyleConfig())
	if err := style.Process(context.Background(), pctx); err != nil {
		t.Fatalf("style Process() error = %v", err)
	}
	after, err := os.ReadFile(pctx.MergedSubPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("style processor rewrote LLM-rendered output")
	}
}

func TestLLMMergerProbeFailureFallsBackToConfiguredStyle(t *testing.T) {
	videoPath, enPath, zhPath := mergerFixture(t)
	processor := newLLMTestProcessor(t, llmMergerTestConfig(t, pairingHandler(t)))
	processor.probe = func(context.Context, string) (*executor.FFProbeOutput, error) {
		return nil, errors.New("ffprobe not installed")
	}
	pctx := &ProcessingContext{VideoPath: videoPath, EnglishSubPath: enPath, ChineseSubPath: zhPath, Metadata: map[string]interface{}{}}
	if err := processor.Process(context.Background(), pctx); err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	content, err := os.ReadFile(pctx.MergedSubPath)
	if err != nil {
		t.Fatal(err)
	}
	// Unknown dimensions leave the configured 1080p canvas untouched.
	for _, want := range []string{"PlayResX: 1920", "PlayResY: 1080", "Style: Default,PingFang SC,52,"} {
		if !strings.Contains(string(content), want) {
			t.Fatalf("fallback output missing %q:\n%s", want, content)
		}
	}
}

func TestLLMMergerProbeDimensionsAdaptWideFrame(t *testing.T) {
	videoPath, enPath, zhPath := mergerFixture(t)
	processor := newLLMTestProcessor(t, llmMergerTestConfig(t, pairingHandler(t)))
	processor.probe = func(context.Context, string) (*executor.FFProbeOutput, error) {
		return &executor.FFProbeOutput{Streams: []executor.StreamInfo{
			{CodecType: "video", Width: 1920, Height: 800},
		}}, nil
	}
	pctx := &ProcessingContext{VideoPath: videoPath, EnglishSubPath: enPath, ChineseSubPath: zhPath, Metadata: map[string]interface{}{}}
	if err := processor.Process(context.Background(), pctx); err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	content, err := os.ReadFile(pctx.MergedSubPath)
	if err != nil {
		t.Fatal(err)
	}
	// 1920x800 wide frame: the 1080-tall reference canvas scale is 1.35, so
	// the 52px font grows to 70 script units.
	if !strings.Contains(string(content), "Style: Default,PingFang SC,70,") {
		t.Fatalf("wide-frame style was not adapted:\n%s", content)
	}
}

func TestLLMMergerRequestFailureFallsBackToNativeASS(t *testing.T) {
	videoPath, enPath, zhPath := mergerFixture(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "upstream unavailable", http.StatusInternalServerError)
	}))
	defer server.Close()
	llm := llmMergerTestConfig(t, pairingHandler(t))
	llm.BaseURL = server.URL + "/v1"
	llm.CacheDir = t.TempDir()
	processor := newLLMTestProcessor(t, llm)
	processor.probe = func(context.Context, string) (*executor.FFProbeOutput, error) { return nil, errors.New("no probe") }

	pctx := &ProcessingContext{VideoPath: videoPath, EnglishSubPath: enPath, ChineseSubPath: zhPath, Metadata: map[string]interface{}{}}
	err := processor.Process(context.Background(), pctx)
	if err != nil {
		t.Fatalf("Process() error = %v, want fallback with nil error", err)
	}
	if pctx.MergedSubPath == "" {
		t.Fatal("merged path not set on fallback")
	}
	content, readErr := os.ReadFile(pctx.MergedSubPath)
	if readErr != nil {
		t.Fatalf("failed reading merged ASS: %v", readErr)
	}
	if len(content) == 0 {
		t.Fatal("merged ASS is empty")
	}
	if degraded, ok := pctx.Metadata["llm_degraded"].(bool); !ok || !degraded {
		t.Fatalf("expected llm_degraded metadata to be true, got %v", pctx.Metadata["llm_degraded"])
	}
	// Fallback audit evidence is still persisted for diagnosis.
	if path, _ := pctx.Metadata[MetadataLLMAuditPath].(string); path == "" {
		t.Fatal("audit artifact was not persisted for fallback alignment")
	}
}

func TestLLMMergerCancellationReturnsContextError(t *testing.T) {
	videoPath, enPath, zhPath := mergerFixture(t)
	// A handler that blocks until the test releases it ensures the request is
	// in flight when cancellation is observed.
	var started sync.WaitGroup
	started.Add(1)
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started.Done()
		select {
		case <-r.Context().Done():
		case <-release:
		case <-time.After(10 * time.Second):
		}
	}))
	defer func() {
		close(release)
		server.Close()
	}()

	llm := llmMergerTestConfig(t, pairingHandler(t))
	llm.BaseURL = server.URL + "/v1"
	llm.CacheDir = t.TempDir()
	llm.CacheEnabled = false
	llm.Retries = 1
	processor := newLLMTestProcessor(t, llm)
	processor.probe = func(context.Context, string) (*executor.FFProbeOutput, error) { return nil, errors.New("no probe") }

	ctx, cancel := context.WithCancel(context.Background())
	pctx := &ProcessingContext{VideoPath: videoPath, EnglishSubPath: enPath, ChineseSubPath: zhPath, Metadata: map[string]interface{}{}}
	done := make(chan error, 1)
	go func() { done <- processor.Process(ctx, pctx) }()

	started.Wait()
	cancel()

	err := <-done
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Process() error = %v, want context.Canceled", err)
	}
	if pctx.MergedSubPath != "" {
		t.Fatalf("merged path set after cancellation: %s", pctx.MergedSubPath)
	}
}

func TestLLMMergerCanceledBeforeStartMakesNoRequest(t *testing.T) {
	videoPath, enPath, zhPath := mergerFixture(t)
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
	}))
	defer server.Close()
	llm := llmMergerTestConfig(t, pairingHandler(t))
	llm.BaseURL = server.URL + "/v1"
	llm.CacheDir = t.TempDir()
	processor := newLLMTestProcessor(t, llm)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	pctx := &ProcessingContext{VideoPath: videoPath, EnglishSubPath: enPath, ChineseSubPath: zhPath, Metadata: map[string]interface{}{}}
	err := processor.Process(ctx, pctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Process() error = %v, want context.Canceled", err)
	}
	if calls != 0 {
		t.Fatalf("endpoint calls = %d, want 0", calls)
	}
}

func TestLLMMergerUsesImportMappingWithoutHTTP(t *testing.T) {
	videoPath, _, _ := mergerFixture(t)
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		http.Error(w, "must not be called", http.StatusInternalServerError)
	}))
	defer server.Close()

	// Import a persisted mapping by absolute path; the production merger must
	// not mutate or copy it. Load-time text cleanup changed the parsed source
	// fingerprint, so the committed prototype mapping no longer matches these
	// sources: a first merge with a pairing server persists a fresh one.
	repoRoot, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	prototypeRoot := filepath.Join(repoRoot, "experiments", "llm-subtitle-merge", "results", "dark-matter-s02e07")
	en, err := os.ReadFile(filepath.Join(prototypeRoot, "en.source.srt"))
	if err != nil {
		t.Fatalf("read prototype English source: %v", err)
	}
	zh, err := os.ReadFile(filepath.Join(prototypeRoot, "zh.source.srt"))
	if err != nil {
		t.Fatalf("read prototype Chinese source: %v", err)
	}
	dir := filepath.Dir(videoPath)
	enPath := writeSubtitleFixture(t, dir, "Episode.eng.srt", string(en))
	zhPath := writeSubtitleFixture(t, dir, "Episode.chs.srt", string(zh))

	first := newLLMTestProcessor(t, llmMergerTestConfig(t, pairingHandler(t)))
	first.probe = func(context.Context, string) (*executor.FFProbeOutput, error) { return nil, errors.New("no probe") }
	firstCtx := &ProcessingContext{VideoPath: videoPath, EnglishSubPath: enPath, ChineseSubPath: zhPath, Metadata: map[string]interface{}{}}
	if err := first.Process(context.Background(), firstCtx); err != nil {
		t.Fatalf("first Process() error = %v", err)
	}
	mappingPath, _ := firstCtx.Metadata[MetadataLLMMappingPath].(string)
	persisted, err := os.ReadFile(mappingPath)
	if err != nil {
		t.Fatalf("read persisted mapping: %v", err)
	}

	llm := llmMergerTestConfig(t, pairingHandler(t))
	llm.BaseURL = server.URL + "/v1"
	llm.MappingPath = mappingPath
	llm.CacheDir = t.TempDir()
	processor := newLLMTestProcessor(t, llm)
	processor.probe = func(context.Context, string) (*executor.FFProbeOutput, error) { return nil, errors.New("no probe") }

	pctx := &ProcessingContext{VideoPath: videoPath, EnglishSubPath: enPath, ChineseSubPath: zhPath, Metadata: map[string]interface{}{}}
	if err := processor.Process(context.Background(), pctx); err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if calls != 0 {
		t.Fatalf("HTTP calls = %d, want 0 for imported mapping", calls)
	}
	content, err := os.ReadFile(pctx.MergedSubPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(content), "\nDialogue:") == 0 || !strings.Contains(string(content), subtitlellm.HeaderMarker) {
		t.Fatal("imported mapping did not produce native bilingual ASS events")
	}

	auditPath := pctx.Metadata[MetadataLLMAuditPath].(string)
	data, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatal(err)
	}
	var audit subtitlellm.Audit
	if err := json.Unmarshal(data, &audit); err != nil {
		t.Fatal(err)
	}
	if audit.Provenance == nil || audit.Provenance.Source != "prototype-import" {
		t.Fatalf("provenance = %+v", audit.Provenance)
	}
	if audit.Provenance.MappingPath != llm.MappingPath {
		t.Fatalf("mapping path = %q, want %q", audit.Provenance.MappingPath, llm.MappingPath)
	}

	// Downstream parity: the style processor must leave the rendered file byte
	// identical, and the source media/subtitles must remain untouched.
	before := append([]byte(nil), content...)
	style := NewStyleProcessor(llmStyleConfig())
	if err := style.Process(context.Background(), pctx); err != nil {
		t.Fatalf("style Process() error = %v", err)
	}
	after, err := os.ReadFile(pctx.MergedSubPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("style processor rewrote the merged output")
	}
	if data, err := os.ReadFile(videoPath); err != nil || string(data) != "synthetic video" {
		t.Fatalf("source video changed: err=%v data=%q", err, data)
	}
	originalEN, err := os.ReadFile(enPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(originalEN) != string(en) {
		t.Fatal("English source subtitle was modified")
	}
	originalZH, err := os.ReadFile(zhPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(originalZH) != string(zh) {
		t.Fatal("Chinese source subtitle was modified")
	}
	if data, err := os.ReadFile(mappingPath); err != nil || string(data) != string(persisted) {
		t.Fatalf("imported mapping changed: err=%v", err)
	}
}

func TestLLMMergerReplaysWithoutNetworkFromPersistentCache(t *testing.T) {
	videoPath, enPath, zhPath := mergerFixture(t)
	var calls int
	var mu sync.Mutex
	handler := pairingHandler(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		handler(w, r)
	}))
	llm := llmMergerTestConfig(t, pairingHandler(t))
	llm.BaseURL = server.URL + "/v1"
	llm.CacheDir = t.TempDir()

	first := newLLMTestProcessor(t, llm)
	first.probe = func(context.Context, string) (*executor.FFProbeOutput, error) { return nil, errors.New("no probe") }
	pctx := &ProcessingContext{VideoPath: videoPath, EnglishSubPath: enPath, ChineseSubPath: zhPath, Metadata: map[string]interface{}{}}
	if err := first.Process(context.Background(), pctx); err != nil {
		t.Fatalf("first Process() error = %v", err)
	}
	firstOutput, err := os.ReadFile(pctx.MergedSubPath)
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	firstCalls := calls
	mu.Unlock()
	if firstCalls == 0 {
		t.Fatal("first run made no HTTP calls")
	}

	// Shut the endpoint down. A second run must replay the persistent cache,
	// which cleanup must not have removed.
	server.Close()
	second := newLLMTestProcessor(t, llm)
	second.probe = func(context.Context, string) (*executor.FFProbeOutput, error) { return nil, errors.New("no probe") }
	secondCtx := &ProcessingContext{VideoPath: videoPath, EnglishSubPath: enPath, ChineseSubPath: zhPath, Metadata: map[string]interface{}{}}
	if err := second.Process(context.Background(), secondCtx); err != nil {
		t.Fatalf("cached Process() error = %v", err)
	}
	secondOutput, err := os.ReadFile(secondCtx.MergedSubPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(firstOutput) != string(secondOutput) {
		t.Fatal("cached rerun produced different output")
	}
	mu.Lock()
	defer mu.Unlock()
	if calls != firstCalls {
		t.Fatalf("cached rerun made %d extra HTTP calls", calls-firstCalls)
	}
}

// TestLLMMergerCacheDisabledWithoutCacheDirKeepsDiagnosticsPrivate covers
// directly constructed configs that pass the subtitlellm config validation
// (cache disabled, no cache dir): alignment runs request-only and diagnostics
// are still written inside the isolated merge dir for this run.
func TestLLMMergerCacheDisabledWithoutCacheDirKeepsDiagnosticsPrivate(t *testing.T) {
	videoPath, enPath, zhPath := mergerFixture(t)
	llm := llmMergerTestConfig(t, pairingHandler(t))
	llm.CacheEnabled = false
	llm.CacheDir = ""
	processor := newLLMTestProcessor(t, llm)
	processor.probe = func(context.Context, string) (*executor.FFProbeOutput, error) { return nil, errors.New("no probe") }

	// Cache disabled means no diagnostics at all, even with no cache dir.
	pctx := &ProcessingContext{VideoPath: videoPath, EnglishSubPath: enPath, ChineseSubPath: zhPath, Metadata: map[string]interface{}{}}
	if err := processor.Process(context.Background(), pctx); err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if pctx.Metadata[MetadataLLMAuditPath] != nil {
		t.Fatal("cache disabled still persisted artifacts")
	}
	if _, err := os.Stat(pctx.MergedSubPath); err != nil {
		t.Fatalf("merged output missing: %v", err)
	}
}

func TestLLMMergerCacheDisabledPersistsNothing(t *testing.T) {
	videoPath, enPath, zhPath := mergerFixture(t)
	llm := llmMergerTestConfig(t, pairingHandler(t))
	llm.CacheEnabled = false
	cacheDir := llm.CacheDir
	processor := newLLMTestProcessor(t, llm)
	processor.probe = func(context.Context, string) (*executor.FFProbeOutput, error) { return nil, errors.New("no probe") }

	pctx := &ProcessingContext{VideoPath: videoPath, EnglishSubPath: enPath, ChineseSubPath: zhPath, Metadata: map[string]interface{}{}}
	if err := processor.Process(context.Background(), pctx); err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	for _, key := range []string{MetadataLLMSourcesPath, MetadataLLMAuditPath, MetadataLLMMappingPath} {
		if value := pctx.Metadata[key]; value != nil {
			t.Fatalf("%s = %v, want nothing persisted when the cache is disabled", key, value)
		}
	}
	entries, err := os.ReadDir(cacheDir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("cache disabled still wrote %d entries to %s", len(entries), cacheDir)
	}
	if pctx.Metadata[subtitlellm.MetadataLLMStyled] != true {
		t.Fatal("llm_styled metadata missing")
	}
	if _, err := os.Stat(pctx.MergedSubPath); err != nil {
		t.Fatalf("merged output missing with cache disabled: %v", err)
	}
}

// TestLLMMergerPersistedMappingIsReimportable round-trips the artifact that
// production wrote through the production import path: the mapping is loaded
// from the persisted alignment/mapping.json with no HTTP access.
func TestLLMMergerPersistedMappingIsReimportable(t *testing.T) {
	videoPath, enPath, zhPath := mergerFixture(t)
	llm := llmMergerTestConfig(t, pairingHandler(t))
	processor := newLLMTestProcessor(t, llm)
	processor.probe = func(context.Context, string) (*executor.FFProbeOutput, error) { return nil, errors.New("no probe") }
	pctx := &ProcessingContext{VideoPath: videoPath, EnglishSubPath: enPath, ChineseSubPath: zhPath, Metadata: map[string]interface{}{}}
	if err := processor.Process(context.Background(), pctx); err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	mappingPath := pctx.Metadata[MetadataLLMMappingPath].(string)
	if _, err := os.Stat(filepath.Join(filepath.Dir(filepath.Dir(mappingPath)), "sources.json")); err != nil {
		t.Fatalf("persisted sources are not importable siblings: %v", err)
	}
	firstOutput, err := os.ReadFile(pctx.MergedSubPath)
	if err != nil {
		t.Fatal(err)
	}

	var calls int
	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		http.Error(w, "must not be called", http.StatusInternalServerError)
	}))
	defer dead.Close()
	importLLM := llm
	importLLM.BaseURL = dead.URL + "/v1"
	importLLM.CacheDir = t.TempDir()
	importLLM.MappingPath = mappingPath
	importProcessor := newLLMTestProcessor(t, importLLM)
	importProcessor.probe = func(context.Context, string) (*executor.FFProbeOutput, error) { return nil, errors.New("no probe") }

	importCtx := &ProcessingContext{VideoPath: videoPath, EnglishSubPath: enPath, ChineseSubPath: zhPath, Metadata: map[string]interface{}{}}
	if err := importProcessor.Process(context.Background(), importCtx); err != nil {
		t.Fatalf("reimport Process() error = %v", err)
	}
	if calls != 0 {
		t.Fatalf("reimport made %d HTTP calls", calls)
	}
	importedOutput, err := os.ReadFile(importCtx.MergedSubPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(firstOutput) != string(importedOutput) {
		t.Fatal("reimported mapping rendered different output")
	}
	var audit subtitlellm.Audit
	data, err := os.ReadFile(importCtx.Metadata[MetadataLLMAuditPath].(string))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &audit); err != nil {
		t.Fatal(err)
	}
	if audit.Provenance == nil || audit.Provenance.Source != "prototype-import" {
		t.Fatalf("provenance = %+v, want prototype-import", audit.Provenance)
	}
}

// TestLLMMergerNilMetadataStillMarksStyled covers contexts created without a
// metadata map, such as hand-built jobs and tests.
func TestLLMMergerNilMetadataStillMarksStyled(t *testing.T) {
	videoPath, enPath, zhPath := mergerFixture(t)
	processor := newLLMTestProcessor(t, llmMergerTestConfig(t, pairingHandler(t)))
	processor.probe = func(context.Context, string) (*executor.FFProbeOutput, error) { return nil, errors.New("no probe") }

	pctx := &ProcessingContext{VideoPath: videoPath, EnglishSubPath: enPath, ChineseSubPath: zhPath}
	if err := processor.Process(context.Background(), pctx); err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if pctx.Metadata == nil {
		t.Fatal("metadata map was not initialized")
	}
	if pctx.Metadata[subtitlellm.MetadataLLMStyled] != true {
		t.Fatal("llm_styled metadata missing for nil starting map")
	}
}

func TestLLMMergerArtifactsNeverContainAPIKey(t *testing.T) {
	videoPath, enPath, zhPath := mergerFixture(t)
	llm := llmMergerTestConfig(t, pairingHandler(t))
	llm.APIKey = "sk-super-secret-value"
	processor := newLLMTestProcessor(t, llm)
	processor.probe = func(context.Context, string) (*executor.FFProbeOutput, error) { return nil, errors.New("no probe") }

	pctx := &ProcessingContext{VideoPath: videoPath, EnglishSubPath: enPath, ChineseSubPath: zhPath, Metadata: map[string]interface{}{}}
	if err := processor.Process(context.Background(), pctx); err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	for _, key := range []string{MetadataLLMMappingPath, MetadataLLMAuditPath, MetadataLLMSourcesPath} {
		path := pctx.Metadata[key].(string)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read artifact %s: %v", path, err)
		}
		if strings.Contains(string(data), "sk-super-secret-value") {
			t.Fatalf("artifact %s leaked the API key", path)
		}
	}
}
