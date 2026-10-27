package subtitle

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fusionn/internal/config"
	"github.com/fusionn/internal/queue"
)

func TestReloadableServiceNewMergeJobUsesChangedConfig(t *testing.T) {
	models := make(chan string, 10)
	pair := pairingHandler(t)
	llm := llmMergerTestConfig(t, func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		var payload struct{ Model string }
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Error(err)
			return
		}
		models <- payload.Model
		r.Body = io.NopCloser(bytes.NewReader(body))
		pair(w, r)
	})
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	writeConfig := func(model, font string) {
		t.Helper()
		content := fmt.Sprintf(`server:
  port: 8080
subtitle:
  enabled: true
  engine: llm
  llm:
    base_url: %q
    model: %q
    cache_enabled: false
  ass_style:
    enabled: true
    primary_font: %q
`, llm.BaseURL, model, font)
		if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	writeConfig("old-model", "Old Font")
	manager, err := config.NewManager(configPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Stop)
	service, err := NewReloadableService(manager, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	runJob := func(wantModel, wantFont string) {
		t.Helper()
		video, en, zh := mergerFixture(t)
		err := service.ProcessMergeJob(context.Background(), &queue.MergeJob{
			JobID: wantModel, VideoPath: video, EnglishPath: en, ChinesePath: zh,
		})
		if err != nil {
			t.Fatal(err)
		}
		select {
		case got := <-models:
			if got != wantModel {
				t.Errorf("new merge job used model %q, want %q", got, wantModel)
			}
		default:
			t.Error("merge job made no model request")
		}
		output, err := os.ReadFile(strings.TrimSuffix(video, filepath.Ext(video)) + ".zh-CN.ass")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(output), wantFont) {
			t.Errorf("new merge job output does not use font %q", wantFont)
		}
	}
	runJob("old-model", "Old Font")
	changed := make(chan struct{}, 1)
	manager.OnChange(func(_, _ *config.Config) { changed <- struct{}{} })
	writeConfig("new-model", "New Font")
	future := time.Now().Add(time.Second)
	if err := os.Chtimes(configPath, future, future); err != nil {
		t.Fatal(err)
	}
	select {
	case <-changed:
	case <-time.After(15 * time.Second):
		t.Fatal("config manager did not detect changed config")
	}
	runJob("new-model", "New Font")
}

func TestReloadableServiceRunningJobKeepsItsConfiguration(t *testing.T) {
	for _, cacheEnabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("cache=%t", cacheEnabled), func(t *testing.T) {
			testReloadableServiceRunningJob(t, cacheEnabled)
		})
	}
}

func testReloadableServiceRunningJob(t *testing.T, cacheEnabled bool) {
	t.Helper()
	started, release := make(chan struct{}), make(chan struct{})
	signalStarted := sync.OnceFunc(func() { close(started) })
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	models := make(chan string, 10)
	pair := pairingHandler(t)
	llm := llmMergerTestConfig(t, func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			return
		}
		var payload struct{ Model string }
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Error(err)
			return
		}
		models <- payload.Model
		if payload.Model == "old-model" {
			signalStarted()
			<-release
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		pair(w, r)
	})
	llm.CacheEnabled = cacheEnabled
	llm.Model = "old-model"
	oldCfg := &config.Config{Subtitle: config.SubtitleConfig{
		Engine: config.EngineLLM, LLM: llm, ASSStyle: llmStyleConfig(),
	}}
	oldCfg.Subtitle.ASSStyle.PrimaryFont = "Old Font"
	var source atomic.Pointer[config.Config]
	source.Store(oldCfg)
	service, err := newConfiguredService(oldCfg, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	service.configSource = source.Load
	oldVideo, oldEn, oldZh := mergerFixture(t)
	done := make(chan error, 1)
	go func() {
		done <- service.ProcessMergeJob(context.Background(), &queue.MergeJob{
			JobID: "running", VideoPath: oldVideo, EnglishPath: oldEn, ChinesePath: oldZh,
		})
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("old job did not reach the LLM")
	}
	newCfg := *oldCfg
	newCfg.Subtitle.LLM.Model = "new-model"
	newCfg.Subtitle.ASSStyle.PrimaryFont = "New Font"
	source.Store(&newCfg)
	newVideo, newEn, newZh := mergerFixture(t)
	if err := service.ProcessMergeJob(context.Background(), &queue.MergeJob{
		JobID: "new", VideoPath: newVideo, EnglishPath: newEn, ChinesePath: newZh,
	}); err != nil {
		t.Fatal(err)
	}
	unblock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("running job did not finish")
	}
	for _, want := range []string{"old-model", "new-model"} {
		select {
		case got := <-models:
			if got != want {
				t.Errorf("request model = %q, want %q", got, want)
			}
		default:
			t.Errorf("missing request for %q", want)
		}
	}
	for video, font := range map[string]string{oldVideo: "Old Font", newVideo: "New Font"} {
		output, err := os.ReadFile(strings.TrimSuffix(video, filepath.Ext(video)) + ".zh-CN.ass")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(output), font) {
			t.Errorf("output for %s does not use font %q", video, font)
		}
	}
}

func TestReloadableServiceRebuildsBothPipelinesAndKeepsLastGoodConfig(t *testing.T) {
	cfg := &config.Config{Subtitle: config.SubtitleConfig{
		Engine: config.EngineDuoSubs, DuoSubs: config.DuoSubsConfig{Mode: "local"},
	}}
	service, err := newConfiguredService(cfg, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	service.configSource = func() *config.Config { return cfg }
	initial := service.currentPipelines()
	if got := service.currentPipelines(); got != initial {
		t.Fatal("unchanged configuration rebuilt pipelines")
	}
	cfg = &config.Config{Subtitle: config.SubtitleConfig{
		Engine: config.EngineLLM, LLM: config.LLMConfig{Model: "new-model"},
	}, Bazarr: config.BazarrConfig{Enabled: true, URL: "http://bazarr", LanguageCode: "zh"},
		Apprise: config.AppriseConfig{Enabled: true, BaseURL: "http://apprise"}}
	next := service.currentPipelines()
	if next.config != cfg || next == initial || next.analyzePipeline == initial.analyzePipeline || next.mergePipeline == initial.mergePipeline {
		t.Fatal("reload did not replace both pipelines together")
	}
	names := processorNames(next.analyzePipeline)
	if hasProcessorName(names, ProcessorNameSDHFilter) || !hasProcessorName(names, ProcessorNameBazarrSearch) {
		t.Fatalf("reloaded analyze pipeline = %v", names)
	}
	if hasProcessorName(processorNames(next.mergePipeline), ProcessorNameConversion) {
		t.Fatal("reloaded LLM pipeline still has the DuoSubs conversion step")
	}
	analyzer := next.analyzePipeline.GetProcessors()[0].(*AnalyzerProcessor)
	if !analyzer.analyzer.preserveNativeASS {
		t.Fatal("reloaded analyzer did not adopt LLM extraction mode")
	}
	var notificationEnabled bool
	for _, processor := range next.mergePipeline.GetProcessors() {
		if p, ok := processor.(*NotificationProcessor); ok {
			notificationEnabled = p.enabled && p.appriseClient != nil
		}
	}
	if !notificationEnabled {
		t.Fatal("reload did not enable Apprise notifications")
	}
	cfg = &config.Config{Subtitle: config.SubtitleConfig{
		Engine: config.EngineDuoSubs, DuoSubs: config.DuoSubsConfig{Mode: "http"},
	}}
	if got := service.currentPipelines(); got != next {
		t.Fatal("failed rebuild discarded the last working pipelines")
	}
	if service.lastConfig != cfg {
		t.Fatal("failed configuration will be retried for every job")
	}
	cfg = initial.config
	if got := service.currentPipelines(); got == next || got.config != cfg {
		t.Fatal("service did not recover after configuration was fixed")
	}
	if hasProcessorName(processorNames(service.GetAnalyzePipeline()), ProcessorNameBazarrSearch) {
		t.Fatal("reload did not disable Bazarr search")
	}
}
