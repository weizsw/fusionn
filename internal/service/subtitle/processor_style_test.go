package subtitle

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/fusionn/internal/config"
	"github.com/fusionn/internal/executor"
	"github.com/fusionn/pkg/logger"
)

func init() {
	// Initialize logger for tests
	logger.Init(true)
}

func TestStyleProcessor(t *testing.T) {
	// Create temp directory for test files
	tempDir := t.TempDir()

	tests := []struct {
		name        string
		assContent  string
		config      config.ASSStyleConfig
		shouldStyle bool
		wantErr     bool
	}{
		{
			name: "Valid ASS file with styling enabled",
			assContent: `[Script Info]
Title: Test

[V4+ Styles]
Format: Name, Fontname, Fontsize
Style: Default,Arial,16

[Events]
Format: Layer, Start, End, Style, Text
Dialog: 0,0:00:00.00,0:00:05.00,Default,Test subtitle
`,
			config: config.ASSStyleConfig{
				Enabled:        true,
				PrimaryFont:    "WenQuanYi Micro Hei",
				PrimarySize:    20,
				PrimaryColor:   "&H00c8c8c8",
				SecondaryFont:  "WenQuanYi Micro Hei",
				SecondarySize:  13,
				SecondaryColor: "&H0010b8ff",
				Bold:           true,
				Outline:        0.5,
				Shadow:         0.5,
				MarginV:        5,
			},
			shouldStyle: true,
			wantErr:     false,
		},
		{
			name: "Styling disabled",
			assContent: `[Script Info]
[V4+ Styles]
[Events]
Dialog: test
`,
			config: config.ASSStyleConfig{
				Enabled: false,
			},
			shouldStyle: false,
			wantErr:     false,
		},
		{
			name: "Invalid ASS file (missing Events)",
			assContent: `[Script Info]
[V4+ Styles]
No events here
`,
			config: config.ASSStyleConfig{
				Enabled: true,
			},
			shouldStyle: true,
			wantErr:     false, // Should not error, just skip styling
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test ASS file
			assFile := filepath.Join(tempDir, "test.ass")
			if err := os.WriteFile(assFile, []byte(tt.assContent), 0644); err != nil {
				t.Fatalf("Failed to create test file: %v", err)
			}

			// Create processor
			processor := NewStyleProcessor(tt.config)

			// Create context
			pctx := &ProcessingContext{
				MergedSubPath: assFile,
			}

			// Test ShouldRun
			if processor.ShouldRun(pctx) != tt.shouldStyle {
				t.Errorf("ShouldRun() = %v, want %v", processor.ShouldRun(pctx), tt.shouldStyle)
			}

			if !tt.shouldStyle {
				return // Skip processing test
			}

			// Process
			err := processor.Process(context.Background(), pctx)
			if (err != nil) != tt.wantErr {
				t.Errorf("Process() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// Read result
			result, err := os.ReadFile(assFile)
			if err != nil {
				t.Fatalf("Failed to read result file: %v", err)
			}

			resultStr := string(result)

			// Verify results if styling should have happened
			if strings.Contains(tt.assContent, "[Events]") {
				// Should have custom Script Info
				if !strings.Contains(resultStr, "WrapStyle: 0") {
					t.Error("Result missing custom Script Info")
				}

				// Should have custom styles
				if !strings.Contains(resultStr, "Style: Default,WenQuanYi Micro Hei,20") {
					t.Error("Result missing Default style")
				}
				if !strings.Contains(resultStr, "Style: Default_1,WenQuanYi Micro Hei,13") {
					t.Error("Result missing Default_1 style")
				}

				// Should preserve Events section
				if !strings.Contains(resultStr, "[Events]") {
					t.Error("Result missing Events section")
				}
				if strings.Contains(tt.assContent, "Test subtitle") {
					if !strings.Contains(resultStr, "Test subtitle") {
						t.Error("Result missing subtitle content")
					}
				}
			}
		})
	}
}

func TestStyleProcessorAdaptsFontSizeAndPlacementOnly(t *testing.T) {
	tests := []struct {
		name      string
		width     int
		height    int
		wantParts []string
	}{
		{
			name:   "4K full frame",
			width:  3840,
			height: 2160,
			wantParts: []string{
				"Style: Default,Arial,20,",
				"Style: Default_1,Arial,13,",
				",1,0.5,0.5,2,0,0,5,1",
			},
		},
		{
			name:   "4:3 frame",
			width:  1440,
			height: 1080,
			wantParts: []string{
				"Style: Default,Arial,20,",
				"Style: Default_1,Arial,13,",
				",1,0.5,0.5,2,0,0,5,1",
			},
		},
		{
			name:   "confirmed Infuse sample",
			width:  3840,
			height: 1606,
			wantParts: []string{
				"Style: Default,Arial,27,",
				"Style: Default_1,Arial,17,",
				",1,0.5,0.5,2,0,0,-180,1",
			},
		},
		{
			name:   "extreme-wide frame",
			width:  3840,
			height: 800,
			wantParts: []string{
				"Style: Default,Arial,54,",
				"Style: Default_1,Arial,35,",
				",1,0.5,0.5,2,0,0,-905,1",
			},
		},
	}
	styleConfig := config.ASSStyleConfig{
		Enabled:        true,
		PrimaryFont:    "Arial",
		PrimarySize:    20,
		PrimaryColor:   "&H00FFFFFF",
		SecondaryFont:  "Arial",
		SecondarySize:  13,
		SecondaryColor: "&H00FF0000",
		Outline:        0.5,
		Shadow:         0.5,
		MarginV:        5,
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assFile := filepath.Join(t.TempDir(), "test.ass")
			if err := os.WriteFile(assFile, []byte("[Events]\nDialogue: test\n"), 0644); err != nil {
				t.Fatalf("Failed to create test file: %v", err)
			}

			processor := NewStyleProcessor(styleConfig)
			processor.probe = func(context.Context, string) (*executor.FFProbeOutput, error) {
				return &executor.FFProbeOutput{Streams: []executor.StreamInfo{
					{CodecType: "video", Width: tt.width, Height: tt.height},
				}}, nil
			}

			err := processor.Process(context.Background(), &ProcessingContext{
				VideoPath:     "/media/movie.mkv",
				MergedSubPath: assFile,
			})
			if err != nil {
				t.Fatalf("Process() error = %v", err)
			}

			result, err := os.ReadFile(assFile)
			if err != nil {
				t.Fatalf("Failed to read result file: %v", err)
			}
			for _, wantPart := range tt.wantParts {
				if !strings.Contains(string(result), wantPart) {
					t.Fatalf("Result does not contain %q:\n%s", wantPart, result)
				}
			}
		})
	}
}

func TestAdjustedMarginVDoesNotAdjustTallerVideo(t *testing.T) {
	current := adjustedMarginV(32, 1440, 1080, 1080)
	legacy := adjustedMarginV(5, 1440, 1080, 288)
	if current != 32 || legacy != 5 {
		t.Fatalf("adjustedMarginV() = %d/%d, want 32/5", current, legacy)
	}
}

func TestAdjustedMarginVWorkedExamples(t *testing.T) {
	tests := []struct {
		name   string
		width  int
		height int
		want   int
	}{
		// Legacy 288-canvas expectations are unchanged.
		{name: "4K full frame", width: 3840, height: 2160, want: 5},
		{name: "4K scope frame", width: 3840, height: 1560, want: -48},
		{name: "confirmed Infuse sample", width: 3840, height: 1606, want: -43},
		{name: "1080p full frame", width: 1920, height: 1080, want: 5},
		{name: "1080p scope frame", width: 1920, height: 800, want: -44},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := adjustedMarginV(5, tt.width, tt.height, 288); got != tt.want {
				t.Fatalf("adjustedMarginV() legacy = %d, want %d", got, tt.want)
			}
		})
	}

	tests1080 := []struct {
		name   string
		width  int
		height int
		want   int
	}{
		{name: "4K full frame", width: 3840, height: 2160, want: 32},
		{name: "4K scope frame", width: 3840, height: 1560, want: -163},
		{name: "confirmed Infuse sample", width: 3840, height: 1606, want: -143},
		{name: "3840x1920", width: 3840, height: 1920, want: -32},
		{name: "1080p full frame", width: 1920, height: 1080, want: 32},
		{name: "1080p scope frame", width: 1920, height: 800, want: -146},
		{name: "4:3 frame", width: 1440, height: 1080, want: 32},
	}
	for _, tt := range tests1080 {
		t.Run("1080 canvas "+tt.name, func(t *testing.T) {
			if got := adjustedMarginV(32, tt.width, tt.height, 1080); got != tt.want {
				t.Fatalf("adjustedMarginV() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestVideoDimensionsIgnoresAttachedPictureStream(t *testing.T) {
	probeOutput := &executor.FFProbeOutput{Streams: []executor.StreamInfo{
		{
			CodecType:   "video",
			Width:       600,
			Height:      600,
			Disposition: map[string]int{"attached_pic": 1},
		},
		{CodecType: "video", Width: 3840, Height: 1606},
	}}

	width, height, ok := videoDimensions(probeOutput)
	if !ok || width != 3840 || height != 1606 {
		t.Fatalf("videoDimensions() = (%d, %d, %v), want (3840, 1606, true)", width, height, ok)
	}
}

func TestStyleProcessorFallsBackAndPreservesDialogueMargin(t *testing.T) {
	const dialogue = "Dialogue: 0,0:00:00.00,0:00:05.00,Default,,0000,0000,0007,,Test subtitle"
	tests := []struct {
		name        string
		probeOutput *executor.FFProbeOutput
		probeErr    error
	}{
		{name: "probe failure", probeErr: errors.New("probe failed")},
		{name: "no video stream", probeOutput: &executor.FFProbeOutput{
			Streams: []executor.StreamInfo{{CodecType: "subtitle"}},
		}},
		{name: "invalid video dimensions", probeOutput: &executor.FFProbeOutput{
			Streams: []executor.StreamInfo{{CodecType: "video", Width: 3840}},
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assFile := filepath.Join(t.TempDir(), "test.ass")
			if err := os.WriteFile(assFile, []byte("[Events]\n"+dialogue+"\n"), 0644); err != nil {
				t.Fatalf("Failed to create test file: %v", err)
			}

			processor := NewStyleProcessor(config.ASSStyleConfig{
				Enabled:        true,
				PrimaryFont:    "Arial",
				PrimarySize:    20,
				PrimaryColor:   "&H00FFFFFF",
				SecondaryFont:  "Arial",
				SecondarySize:  13,
				SecondaryColor: "&H00FF0000",
				Outline:        0.5,
				Shadow:         0.5,
				MarginV:        5,
			})
			processor.probe = func(context.Context, string) (*executor.FFProbeOutput, error) {
				return tt.probeOutput, tt.probeErr
			}

			err := processor.Process(context.Background(), &ProcessingContext{
				VideoPath:     "/media/movie.mkv",
				MergedSubPath: assFile,
			})
			if err != nil {
				t.Fatalf("Process() error = %v", err)
			}

			result, err := os.ReadFile(assFile)
			if err != nil {
				t.Fatalf("Failed to read result file: %v", err)
			}
			resultString := string(result)
			if !strings.Contains(resultString, ",2,0,0,5,1") {
				t.Fatalf("Result does not contain configured MarginV 5:\n%s", result)
			}
			if !strings.Contains(resultString, "Style: Default,Arial,20") {
				t.Fatalf("Result changed configured primary font size:\n%s", result)
			}
			if !strings.Contains(resultString, "Style: Default_1,Arial,13") {
				t.Fatalf("Result changed configured secondary font size:\n%s", result)
			}
			if !strings.Contains(resultString, ",1,0.5,0.5,2,0,0,5,1") {
				t.Fatalf("Result changed configured outline or shadow:\n%s", result)
			}
			if !strings.Contains(resultString, dialogue) {
				t.Fatalf("Result changed dialogue-level MarginV:\n%s", result)
			}
		})
	}
}

func TestGenerateStyles(t *testing.T) {
	cfg := config.ASSStyleConfig{
		Enabled:        true,
		PrimaryFont:    "Arial",
		PrimarySize:    20,
		PrimaryColor:   "&H00FFFFFF",
		SecondaryFont:  "Arial",
		SecondarySize:  13,
		SecondaryColor: "&H00FF0000",
		Bold:           true,
		Outline:        0.5,
		Shadow:         0.5,
		MarginV:        5,
	}

	result := GenerateStyles(cfg)

	// Check format line
	if !strings.Contains(result, "[V4+ Styles]") {
		t.Error("Missing [V4+ Styles] header")
	}

	// Check Format line
	if !strings.Contains(result, "Format: Name, Fontname, Fontsize") {
		t.Error("Missing Format line")
	}

	// Check Default style
	if !strings.Contains(result, "Style: Default,Arial,20,&H00FFFFFF") {
		t.Error("Missing or incorrect Default style")
	}

	// Check Default_1 style
	if !strings.Contains(result, "Style: Default_1,Arial,13,&H00FF0000") {
		t.Error("Missing or incorrect Default_1 style")
	}

	// Check bold flag (should be 1)
	if !strings.Contains(result, ",1,0,0,0,") { // bold=1, italic=0, underline=0, strikeout=0
		t.Error("Bold flag not set correctly")
	}
}

func TestStyleProcessorPreservesUnknownStylesAndEvents(t *testing.T) {
	assContent := `[Script Info]
Title: DuoSubs output
PlayResX: 640
PlayResY: 480
YCbCr Matrix: TV.709

[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding
Style: DuoMain,Arial,30,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,1,1,2,10,10,10,1
Style: DuoSecondary,Arial,20,&H00FFFF00,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,1,1,2,10,10,10,1
Style: Default,Old,10,&H00FFFFFF,&H000000FF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,1,1,2,1,1,1,1

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Dialogue: 0,0:00:01.00,0:00:02.00,DuoMain,,10,10,10,,Hello
Dialogue: 0,0:00:01.00,0:00:02.00,DuoSecondary,,10,10,10,,你好
Dialogue: 0,0:00:03.00,0:00:04.00,Default,,10,10,10,,{\i1}Italic{\i0}
`
	assFile := filepath.Join(t.TempDir(), "test.ass")
	if err := os.WriteFile(assFile, []byte(assContent), 0644); err != nil {
		t.Fatalf("write test file: %v", err)
	}
	processor := NewStyleProcessor(config.ASSStyleConfig{
		Enabled: true, PlayResX: 1920, PlayResY: 1080,
		PrimaryFont: "PingFang SC", PrimarySize: 52, PrimaryColor: "&H08EFECEA",
		SecondaryFont: "Helvetica Neue", SecondarySize: 36, SecondaryColor: "&H087BC0E5",
		Outline: 1.4, Shadow: 1.5, MarginV: 32, MarginLeft: 80, MarginRight: 80,
		Alignment: 2, BorderStyle: 1, WrapStyle: "0", ScaleX: 100,
		LineHeightRatio: 1.23, BlockPadding: 16, CollisionGap: 12,
	})
	err := processor.Process(context.Background(), &ProcessingContext{MergedSubPath: assFile})
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	result, err := os.ReadFile(assFile)
	if err != nil {
		t.Fatalf("read result: %v", err)
	}
	text := string(result)

	// DuoSubs-only styles and events survive.
	for _, want := range []string{
		"Style: DuoMain,Arial,30,", "Style: DuoSecondary,Arial,20,",
		"Dialogue: 0,0:00:01.00,0:00:02.00,DuoMain,,10,10,10,,Hello",
		"Dialogue: 0,0:00:01.00,0:00:02.00,DuoSecondary,,10,10,10,,你好",
		"{\\i1}Italic{\\i0}",
		"PlayResX: 1920", "PlayResY: 1080",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("result missing %q:\n%s", want, text)
		}
	}
	// The pre-existing Default is replaced, not duplicated.
	if strings.Contains(text, "Style: Default,Old,10,") {
		t.Fatalf("stale Default style survived:\n%s", text)
	}
	if strings.Count(text, "Style: Default,") != 1 {
		t.Fatalf("expected exactly one Default style:\n%s", text)
	}
	// Shared names exist and carry the new palette.
	for _, want := range []string{
		"Style: Default,PingFang SC,52,&H08EFECEA",
		"Style: Eng,Helvetica Neue,36,&H087BC0E5",
		"Style: Default_1,Helvetica Neue,36,&H087BC0E5",
		"Style: TopDefault,PingFang SC,52,",
		"Style: TopEng,Helvetica Neue,36,",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("result missing shared style %q:\n%s", want, text)
		}
	}
	// Styles must precede Events.
	if strings.Index(text, "[V4+ Styles]") > strings.Index(text, "[Events]") {
		t.Fatalf("styles section placed after events:\n%s", text)
	}
	// Header marker allows skipping on a second pass.
	if !strings.Contains(text, headerMarkerLLMStyled) {
		t.Fatalf("missing LLM header marker:\n%s", text)
	}
}

func TestStyleProcessorSkipsLLMStyledOutput(t *testing.T) {
	const llmOutput = `[Script Info]
; fusionn-llm-styled
PlayResX: 1920
PlayResY: 1080

[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding
Style: Default,PingFang SC,52,&H08EFECEA,&H000000FF,&H30141414,&H80000000,0,0,0,0,100,100,0.0,0,1,1.4,1.5,2,80,80,32,1
Style: Eng,Helvetica Neue,36,&H087BC0E5,&H000000FF,&H30141414,&H80000000,0,0,0,0,100,100,0.0,0,1,1.4,1.5,2,80,80,32,1
Style: TopDefault,PingFang SC,52,&H08EFECEA,&H000000FF,&H30141414,&H80000000,0,0,0,0,100,100,0.0,0,1,1.4,1.5,8,80,80,32,1
Style: TopEng,Helvetica Neue,36,&H087BC0E5,&H000000FF,&H30141414,&H80000000,0,0,0,0,100,100,0.0,0,1,1.4,1.5,8,80,80,32,1

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Dialogue: 0,0:00:01.00,0:00:02.00,Default,,0,0,0,,{\an2\pos(960,1048)}你好\N{\rEng}Hello
`
	for _, tc := range []struct {
		name    string
		content string
		meta    map[string]interface{}
	}{
		{name: "header marker", content: llmOutput},
		{name: "metadata flag", content: strings.Replace(llmOutput, "; fusionn-llm-styled\n", "", 1), meta: map[string]interface{}{metadataLLMStyled: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assFile := filepath.Join(t.TempDir(), "llm.ass")
			if err := os.WriteFile(assFile, []byte(tc.content), 0644); err != nil {
				t.Fatalf("write: %v", err)
			}
			processor := NewStyleProcessor(config.ASSStyleConfig{Enabled: true, PrimarySize: 20, SecondarySize: 13})
			err := processor.Process(context.Background(), &ProcessingContext{
				MergedSubPath: assFile, Metadata: tc.meta,
			})
			if err != nil {
				t.Fatalf("Process() error = %v", err)
			}
			result, _ := os.ReadFile(assFile)
			if string(result) != tc.content {
				t.Fatalf("LLM output was modified:\n%s", result)
			}
		})
	}
}

func TestStyleProcessorConvertsCoordinatesFromOldCanvas(t *testing.T) {
	assContent := `[Script Info]
PlayResX: 384
PlayResY: 288

[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding
Style: Default,Arial,16,&H00FFFFFF,&H0000FFFF,&H00000000,&H00000000,0,0,0,0,100,100,0,0,1,1,0,2,10,10,10,1

[Events]
Format: Layer, Start, End, Style, Name, MarginL, MarginR, MarginV, Effect, Text
Dialogue: 0,0:00:01.00,0:00:02.00,Default,,0,0,0,,{\pos(192,144)}居中
Dialogue: 0,0:00:01.00,0:00:02.00,Default,,0,0,0,,{\an8\pos(192,20)}顶部
Dialogue: 0,0:00:03.00,0:00:04.00,Default,,0,0,0,,{\move(0,0,384,288)}移动
Dialogue: 0,0:00:05.00,0:00:06.00,Default,,0,0,0,,plain
`
	assFile := filepath.Join(t.TempDir(), "old.ass")
	if err := os.WriteFile(assFile, []byte(assContent), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	processor := NewStyleProcessor(config.ASSStyleConfig{
		Enabled: true, PlayResX: 1920, PlayResY: 1080,
		PrimaryFont: "Arial", PrimarySize: 52, SecondaryFont: "Arial", SecondarySize: 36,
		MarginV: 32, MarginLeft: 80, MarginRight: 80, Alignment: 2, BorderStyle: 1,
		ScaleX: 100, LineHeightRatio: 1.23, BlockPadding: 16, CollisionGap: 12,
	})
	if err := processor.Process(context.Background(), &ProcessingContext{MergedSubPath: assFile}); err != nil {
		t.Fatalf("Process() error = %v", err)
	}
	result, _ := os.ReadFile(assFile)
	text := string(result)
	for _, want := range []string{
		`\pos(960,540)`,        // center scales 5x
		`\an8\pos(960,75)`,     // top keeps its top offset, scaled 5x/3.75x
		`\move(0,0,1920,1080)`, // move endpoints scale to the canvas
		`plain`,                // events without coordinates are untouched
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("result missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, `\pos(192,144)`) {
		t.Fatalf("old coordinates survived:\n%s", text)
	}
}

func TestGenerateStylesIncludesAllSharedStyles(t *testing.T) {
	cfg := config.ASSStyleConfig{
		PrimaryFont: "PingFang SC", PrimarySize: 52, PrimaryColor: "&H08EFECEA",
		SecondaryFont: "Helvetica Neue", SecondarySize: 36, SecondaryColor: "&H087BC0E5",
		Outline: 1.4, OutlineColor: "&H30141414", Shadow: 1.5, BackColor: "&H80000000",
		MarginV: 32, MarginLeft: 80, MarginRight: 80, Alignment: 2, BorderStyle: 1,
		ScaleX: 100, Spacing: 0, LineHeightRatio: 1.23, BlockPadding: 16, CollisionGap: 12,
	}
	styles := GenerateStyles(cfg)
	for _, name := range []string{"Default", "Default_1", "Eng", "TopDefault", "TopEng"} {
		if !strings.Contains(styles, "Style: "+name+",") {
			t.Fatalf("missing style %q:\n%s", name, styles)
		}
	}
	// Aliases carry the same English appearance.
	eng := styleLine(t, styles, "Eng")
	alias := styleLine(t, styles, "Default_1")
	if strings.Replace(eng, "Style: Eng,", "", 1) != strings.Replace(alias, "Style: Default_1,", "", 1) {
		t.Fatalf("Default_1 alias differs from Eng:\n%s\n%s", eng, alias)
	}
	// Top styles use top alignment.
	if !strings.Contains(styleLine(t, styles, "TopDefault"), ",8,") {
		t.Fatalf("TopDefault is not top-aligned: %s", styleLine(t, styles, "TopDefault"))
	}
	// ScaleX and Spacing are emitted from config.
	if !strings.Contains(styles, ",100,100,0.0,") {
		t.Fatalf("ScaleX/Spacing not emitted:\n%s", styles)
	}
}

func styleLine(t *testing.T, styles, name string) string {
	t.Helper()
	for _, line := range strings.Split(styles, "\n") {
		if strings.HasPrefix(line, "Style: "+name+",") {
			return line
		}
	}
	t.Fatalf("style %q not found", name)
	return ""
}

func TestStyleProcessorAdaptsLegacyCanvasSizesForWideVideo(t *testing.T) {
	// Legacy 384x288 config values must be migrated by the loader, but a
	// hand-built 1080p config must scale for a 2.35:1 encoded frame while
	// keeping the same 16:9 output placement.
	cfg := config.ASSStyleConfig{
		Enabled: true, PlayResX: 1920, PlayResY: 1080,
		PrimaryFont: "Arial", PrimarySize: 52, SecondaryFont: "Arial", SecondarySize: 36,
		MarginV: 32, MarginLeft: 80, MarginRight: 80, Alignment: 2, BorderStyle: 1,
		ScaleX: 100, LineHeightRatio: 1.23, BlockPadding: 16, CollisionGap: 12,
	}
	for _, tc := range []struct {
		name       string
		width      int
		height     int
		wantSize   int
		wantMargin int
	}{
		{name: "1920x1080", width: 1920, height: 1080, wantSize: 52, wantMargin: 32},
		{name: "3840x1606", width: 3840, height: 1606, wantSize: 70, wantMargin: -143},
		{name: "3840x1920", width: 3840, height: 1920, wantSize: 59, wantMargin: -32},
		{name: "1440x1080", width: 1440, height: 1080, wantSize: 52, wantMargin: 32},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assFile := filepath.Join(t.TempDir(), "test.ass")
			if err := os.WriteFile(assFile, []byte("[Events]\nDialogue: test\n"), 0644); err != nil {
				t.Fatalf("write: %v", err)
			}
			processor := NewStyleProcessor(cfg)
			processor.probe = func(context.Context, string) (*executor.FFProbeOutput, error) {
				return &executor.FFProbeOutput{Streams: []executor.StreamInfo{
					{CodecType: "video", Width: tc.width, Height: tc.height},
				}}, nil
			}
			if err := processor.Process(context.Background(), &ProcessingContext{VideoPath: "/media/movie.mkv", MergedSubPath: assFile}); err != nil {
				t.Fatalf("Process() error = %v", err)
			}
			result, _ := os.ReadFile(assFile)
			want := "Style: Default,Arial," + strconv.Itoa(tc.wantSize) + ","
			if !strings.Contains(string(result), want) {
				t.Fatalf("missing %q:\n%s", want, result)
			}
			wantMargin := fmt.Sprintf(",2,80,80,%d,1", tc.wantMargin)
			if !strings.Contains(string(result), wantMargin) {
				t.Fatalf("missing margin %q:\n%s", wantMargin, result)
			}
		})
	}
}
