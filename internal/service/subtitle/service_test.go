package subtitle

import (
	"context"
	"testing"

	"github.com/fusionn/internal/client/bazarr"
	"github.com/fusionn/internal/config"
	"github.com/fusionn/internal/queue"
)

func TestNewServiceAnalyzePipelineRunsBazarrSDHFilterAfterBazarrSearch(t *testing.T) {
	mergeQueue := queue.NewMergeQueue(queue.Config{}, func(context.Context, *queue.MergeJob) error {
		return nil
	})
	service, err := NewService(
		&config.Config{Subtitle: config.SubtitleConfig{DuoSubs: config.DuoSubsConfig{Mode: "local"}}},
		nil,
		mergeQueue,
		bazarr.NewClient("http://bazarr", "key", 1),
	)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	processors := service.GetAnalyzePipeline().GetProcessors()
	var names []string
	for _, proc := range processors {
		names = append(names, proc.Name())
	}

	want := []string{
		ProcessorNameAnalyzer,
		ProcessorNameExtractor,
		ProcessorNameSDHFilter,
		ProcessorNameBazarrSearch,
		ProcessorNameBazarrSDHFilter,
		ProcessorNameTranslationQueue,
	}
	if len(names) != len(want) {
		t.Fatalf("processor names = %#v, want %#v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("processor names = %#v, want %#v", names, want)
		}
	}
}
