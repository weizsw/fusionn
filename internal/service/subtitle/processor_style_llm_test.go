package subtitle

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fusionn/internal/config"
	"github.com/fusionn/internal/subtitlellm"
)

// llmRenderFixture builds a minimal bundle/mapping pair for integration checks
// between the native renderer and the style processor.
func llmRenderFixture(t *testing.T) (subtitlellm.Bundle, subtitlellm.Mapping) {
	t.Helper()
	cue := func(id, start, end int, text string) subtitlellm.Cue {
		return subtitlellm.Cue{
			ID: id, Start: start, End: end, OriginalStart: start, OriginalEnd: end,
			Raw: text, Plain: text, Display: text, Style: "Default", Alignment: 2,
		}
	}
	bundle := subtitlellm.Bundle{
		Version: subtitlellm.Version,
		EN: []subtitlellm.Cue{
			cue(1, 1000, 3000, "Hello"),
			cue(2, 3100, 5000, "World"),
		},
		ZH: []subtitlellm.Cue{
			cue(1, 1000, 5000, "你好世界"),
		},
	}
	// Render only requires the mapping fingerprint to match its bundle; the
	// canonical digest format is covered by subtitlellm's own tests.
	bundle.Fingerprint = "processor-integration-fixture"
	mapping := subtitlellm.Mapping{
		Version:           subtitlellm.Version,
		SourceFingerprint: bundle.Fingerprint,
		Groups: []subtitlellm.Group{{
			ENIDs: []int{1, 2}, ZHIDs: []int{1}, Layer: subtitlellm.LayerBottom, Origin: subtitlellm.OriginModel,
		}},
	}
	return bundle, mapping
}

func styleTestConfig() config.ASSStyleConfig {
	return config.ASSStyleConfig{
		Enabled: true, PlayResX: 1920, PlayResY: 1080,
		PrimaryFont: "PingFang SC", PrimarySize: 52, PrimaryColor: "&H08EFECEA",
		SecondaryFont: "Helvetica Neue", SecondarySize: 36, SecondaryColor: "&H087BC0E5",
		Outline: 1.4, OutlineColor: "&H30141414", Shadow: 1.5, BackColor: "&H80000000",
		MarginV: 32, MarginLeft: 80, MarginRight: 80, Alignment: 2, BorderStyle: 1,
		WrapStyle: "0", ScaleX: 100, LineHeightRatio: 1.23, BlockPadding: 16, CollisionGap: 12,
	}
}

func TestStyleProcessorLeavesNativeLLMRenderUnchanged(t *testing.T) {
	bundle, mapping := llmRenderFixture(t)
	rendered, err := subtitlellm.Render(bundle, mapping, config.LLMConfig{
		EnChars: 65, ZhChars: 25, EnGapMS: 400, ZhGapMS: 500, SpanMS: 5500, FillGapMS: 500,
	}, styleTestConfig(), 1920, 1080)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	content := string(rendered)
	for _, want := range []string{
		"Style: Default,", "Style: Eng,", "Style: Default_1,", "Style: TopDefault,", "Style: TopEng,",
		headerMarkerLLMStyled, `\an2\pos(960,`, `Style: TopDefault,`,
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("render output missing %q:\n%s", want, content)
		}
	}

	for _, tc := range []struct {
		name string
		meta map[string]interface{}
	}{
		{name: "header marker only"},
		{name: "metadata flag", meta: map[string]interface{}{metadataLLMStyled: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "llm.ass")
			if err := os.WriteFile(path, rendered, 0644); err != nil {
				t.Fatalf("write: %v", err)
			}
			processor := NewStyleProcessor(styleTestConfig())
			if err := processor.Process(context.Background(), &ProcessingContext{
				MergedSubPath: path, Metadata: tc.meta,
			}); err != nil {
				t.Fatalf("Process: %v", err)
			}
			result, _ := os.ReadFile(path)
			if string(result) != content {
				t.Fatalf("style processor modified native LLM output:\n%s", result)
			}
		})
	}
}
