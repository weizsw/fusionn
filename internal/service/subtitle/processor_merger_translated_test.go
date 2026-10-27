package subtitle

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/fusionn/internal/subtitlellm"
)

func TestLLMMergerTranslatedSkipsModel(t *testing.T) {
	videoPath, enPath, zhPath := mergerFixture(t)
	var calls atomic.Int32
	llm := llmMergerTestConfig(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "must not be called", http.StatusInternalServerError)
	})
	llm.APIKeyEnv = "FUSIONN_TRANSLATED_TEST_KEY"
	t.Setenv(llm.APIKeyEnv, "")
	llm.MappingPath = filepath.Join(t.TempDir(), "missing-mapping.json")
	processor := newLLMTestProcessor(t, llm)
	processor.probe = nil
	pctx := &ProcessingContext{
		VideoPath: videoPath, EnglishSubPath: enPath, ChineseSubPath: zhPath,
		ChineseSubSource: ChineseSourceTranslated,
	}
	if err := processor.Process(context.Background(), pctx); err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("model calls = %d, want 0", calls.Load())
	}
	content, err := os.ReadFile(pctx.MergedSubPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{subtitlellm.HeaderMarker, "Hello", "你好", "三天后"} {
		if !strings.Contains(string(content), want) {
			t.Fatalf("output missing %q", want)
		}
	}
	if pctx.Metadata[subtitlellm.MetadataLLMStyled] != true || pctx.Metadata[MetadataLLMDegraded] != nil {
		t.Fatalf("unexpected metadata: %+v", pctx.Metadata)
	}
	var audit subtitlellm.Audit
	data, err := os.ReadFile(pctx.Metadata[MetadataLLMAuditPath].(string))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &audit); err != nil {
		t.Fatal(err)
	}
	if audit.Provenance == nil || audit.Provenance.Source != "translation" || audit.HTTPAttempts != 0 {
		t.Fatalf("unexpected audit: %+v", audit)
	}
	if audit.Coverage.EN != 2 || audit.Coverage.ZH != 2 {
		t.Fatalf("coverage = %+v", audit.Coverage)
	}

	// Persisted direct mappings must remain usable by the offline import path.
	llm.MappingPath = pctx.Metadata[MetadataLLMMappingPath].(string)
	bundle, err := subtitlellm.LoadSources(enPath, zhPath)
	if err != nil {
		t.Fatal(err)
	}
	mapping, _, err := subtitlellm.NewEngine().Align(context.Background(), bundle, llm)
	if err != nil {
		t.Fatalf("reimport translated mapping: %v", err)
	}
	for i, group := range mapping.Groups {
		if len(group.ENIDs) != 1 || len(group.ZHIDs) != 1 || group.ENIDs[0] != i+1 || group.ZHIDs[0] != i+1 || group.Origin != "translation" {
			t.Fatalf("group %d = %+v, want direct 1:1 mapping", i, group)
		}
	}
}

func TestLLMMergerExternalSourcesStillUseModel(t *testing.T) {
	for _, source := range []string{ChineseSourceExtracted, ChineseSourceBazarr, ""} {
		t.Run(source, func(t *testing.T) {
			videoPath, enPath, zhPath := mergerFixture(t)
			var calls atomic.Int32
			handler := pairingHandler(t)
			llm := llmMergerTestConfig(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				handler(w, r)
			})
			processor := newLLMTestProcessor(t, llm)
			processor.probe = nil
			pctx := &ProcessingContext{
				VideoPath: videoPath, EnglishSubPath: enPath, ChineseSubPath: zhPath,
				ChineseSubSource: source,
			}
			if err := processor.Process(context.Background(), pctx); err != nil {
				t.Fatal(err)
			}
			if calls.Load() == 0 {
				t.Fatal("external subtitle skipped model despite having no translation provenance")
			}
		})
	}
}

func TestLLMMergerTranslatedRejectsMismatchedSources(t *testing.T) {
	for _, tc := range []struct {
		name, zh, want string
	}{
		{"count", srtFixture([2]string{"00:00:01,000 --> 00:00:03,000", "你好"}), "cue counts differ"},
		{"start", srtFixture(
			[2]string{"00:00:01,100 --> 00:00:03,000", "你好"},
			[2]string{"00:00:04,000 --> 00:00:06,000", "电话响了"},
		), "cue 1 timing differs"},
		{"end", srtFixture(
			[2]string{"00:00:01,000 --> 00:00:03,100", "你好"},
			[2]string{"00:00:04,000 --> 00:00:06,000", "电话响了"},
		), "cue 1 timing differs"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			videoPath, enPath, _ := mergerFixture(t)
			zhPath := writeSubtitleFixture(t, filepath.Dir(videoPath), "translated.srt", tc.zh)
			var calls atomic.Int32
			llm := llmMergerTestConfig(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				http.Error(w, "must not be called", http.StatusInternalServerError)
			})
			processor := newLLMTestProcessor(t, llm)
			processor.probe = nil
			pctx := &ProcessingContext{
				VideoPath: videoPath, EnglishSubPath: enPath, ChineseSubPath: zhPath,
				ChineseSubSource: ChineseSourceTranslated,
			}
			err := processor.Process(context.Background(), pctx)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Process() error = %v, want %q", err, tc.want)
			}
			if pctx.MergedSubPath != "" || calls.Load() != 0 {
				t.Fatalf("mismatched translation rendered %q or made %d model calls", pctx.MergedSubPath, calls.Load())
			}
		})
	}
}
