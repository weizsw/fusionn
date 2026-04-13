package subtitle

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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

func TestDualLanguageProcessor_Process_DualSRT(t *testing.T) {
	tempDir := t.TempDir()

	srtContent := "1\n00:00:01,000 --> 00:00:03,000\n你好世界\nHello World\n\n" +
		"2\n00:00:04,000 --> 00:00:06,000\n再见\nGoodbye\n\n" +
		"3\n00:00:07,000 --> 00:00:09,000\n谢谢\nThank you\n\n"

	subPath := filepath.Join(tempDir, "test.zh.srt")
	if err := os.WriteFile(subPath, []byte(srtContent), 0644); err != nil {
		t.Fatal(err)
	}

	videoPath := filepath.Join(tempDir, "video.mkv")

	pctx := &ProcessingContext{
		VideoPath:        videoPath,
		ChineseSubPath:   subPath,
		ChineseSubSource: ChineseSourceBazarr,
		Metadata:         make(map[string]interface{}),
	}

	p := NewDualLanguageProcessor(config.OpenCCConfig{Enabled: false})
	err := p.Process(context.Background(), pctx)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	if pctx.MergedSubPath == "" {
		t.Fatal("Process() did not set MergedSubPath")
	}

	content, err := os.ReadFile(pctx.MergedSubPath)
	if err != nil {
		t.Fatalf("Failed to read merged file: %v", err)
	}

	result := string(content)
	if !strings.Contains(result, "Style: Default,") {
		t.Error("missing Default style")
	}
	if !strings.Contains(result, "Style: Default_1,") {
		t.Error("missing Default_1 style")
	}
	if !strings.Contains(result, "Default,,0,0,0,,你好世界") {
		t.Error("missing Chinese dialogue")
	}
	if !strings.Contains(result, "Default_1,,0,0,0,,Hello World") {
		t.Error("missing English dialogue")
	}

	if pctx.TempMergeDir != "" {
		os.RemoveAll(pctx.TempMergeDir)
	}
}

func TestDualLanguageProcessor_Process_NotDual(t *testing.T) {
	tempDir := t.TempDir()

	srtContent := "1\n00:00:01,000 --> 00:00:03,000\n你好世界\n\n" +
		"2\n00:00:04,000 --> 00:00:06,000\n再见\n\n"

	subPath := filepath.Join(tempDir, "test.zh.srt")
	if err := os.WriteFile(subPath, []byte(srtContent), 0644); err != nil {
		t.Fatal(err)
	}

	videoPath := filepath.Join(tempDir, "video.mkv")

	pctx := &ProcessingContext{
		VideoPath:        videoPath,
		ChineseSubPath:   subPath,
		ChineseSubSource: ChineseSourceBazarr,
		Metadata:         make(map[string]interface{}),
	}

	p := NewDualLanguageProcessor(config.OpenCCConfig{Enabled: false})
	err := p.Process(context.Background(), pctx)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	if pctx.MergedSubPath != "" {
		t.Errorf("Process() should not set MergedSubPath for non-dual subtitle, got %q", pctx.MergedSubPath)
	}
}

func TestDualLanguageProcessor_Process_DualASS(t *testing.T) {
	tempDir := t.TempDir()

	assContent := `[Script Info]
ScriptType: v4.00+

[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding
Style: CHS,Arial,20,&H00FFFFFF,&H0000ffff,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,1.0,0.0,2,10,10,10,1
Style: ENG,Arial,14,&H00FFFFFF,&H0000ffff,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,1.0,0.0,2,10,10,10,1

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Dialogue: 0,0:00:01.00,0:00:03.00,CHS,,0,0,0,,你好世界
Dialogue: 0,0:00:01.00,0:00:03.00,ENG,,0,0,0,,Hello World
Dialogue: 0,0:00:04.00,0:00:06.00,CHS,,0,0,0,,再见
Dialogue: 0,0:00:04.00,0:00:06.00,ENG,,0,0,0,,Goodbye
`

	subPath := filepath.Join(tempDir, "test.zh.ass")
	if err := os.WriteFile(subPath, []byte(assContent), 0644); err != nil {
		t.Fatal(err)
	}

	videoPath := filepath.Join(tempDir, "video.mkv")

	pctx := &ProcessingContext{
		VideoPath:        videoPath,
		ChineseSubPath:   subPath,
		ChineseSubSource: ChineseSourceBazarr,
		Metadata:         make(map[string]interface{}),
	}

	p := NewDualLanguageProcessor(config.OpenCCConfig{Enabled: false})
	err := p.Process(context.Background(), pctx)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	if pctx.MergedSubPath == "" {
		t.Fatal("Process() did not set MergedSubPath for dual-language ASS")
	}

	content, err := os.ReadFile(pctx.MergedSubPath)
	if err != nil {
		t.Fatalf("Failed to read merged file: %v", err)
	}

	result := string(content)
	if !strings.Contains(result, "Default,,0,0,0,,你好世界") {
		t.Error("missing Chinese dialogue with Default style")
	}
	if !strings.Contains(result, "Default_1,,0,0,0,,Hello World") {
		t.Error("missing English dialogue with Default_1 style")
	}

	if pctx.TempMergeDir != "" {
		os.RemoveAll(pctx.TempMergeDir)
	}
}
