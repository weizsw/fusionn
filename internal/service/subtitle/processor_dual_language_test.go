package subtitle

import (
	"testing"

	"github.com/fusionn/internal/config"
	"github.com/fusionn/pkg/logger"
)

func init() {
	logger.Init(true)
}

func TestDualLanguageProcessor_Name(t *testing.T) {
	p := NewDualLanguageProcessor(config.OpenCCConfig{})
	if got := p.Name(); got != "DualLanguage" {
		t.Errorf("Name() = %v, want DualLanguage", got)
	}
}

func TestDualLanguageProcessor_ShouldRun(t *testing.T) {
	tests := []struct {
		name string
		pctx *ProcessingContext
		want bool
	}{
		{
			name: "bazarr source with chinese sub",
			pctx: &ProcessingContext{
				ChineseSubSource: ChineseSourceBazarr,
				ChineseSubPath:   "/path/to/sub.srt",
			},
			want: true,
		},
		{
			name: "non-bazarr source",
			pctx: &ProcessingContext{
				ChineseSubSource: ChineseSourceExtracted,
				ChineseSubPath:   "/path/to/sub.srt",
			},
			want: false,
		},
		{
			name: "translated source",
			pctx: &ProcessingContext{
				ChineseSubSource: ChineseSourceTranslated,
				ChineseSubPath:   "/path/to/sub.srt",
			},
			want: false,
		},
		{
			name: "bazarr but no chinese path",
			pctx: &ProcessingContext{
				ChineseSubSource: ChineseSourceBazarr,
				ChineseSubPath:   "",
			},
			want: false,
		},
		{
			name: "bazarr but merged already set",
			pctx: &ProcessingContext{
				ChineseSubSource: ChineseSourceBazarr,
				ChineseSubPath:   "/path/to/sub.srt",
				MergedSubPath:    "/path/to/merged.ass",
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewDualLanguageProcessor(config.OpenCCConfig{})
			if got := p.ShouldRun(tt.pctx); got != tt.want {
				t.Errorf("ShouldRun() = %v, want %v", got, tt.want)
			}
		})
	}
}
