package subtitle

import (
	"testing"

	"github.com/fusionn/internal/config"

	"github.com/stretchr/testify/assert"
)

func TestGenerateStylesWithNewParameters(t *testing.T) {
	cfg := config.ASSStyleConfig{
		Enabled:        true,
		PrimaryFont:    "Arial",
		PrimarySize:    20,
		PrimaryColor:   "&H00FFFFFF",
		SecondaryFont:  "Arial",
		SecondarySize:  15,
		SecondaryColor: "&H0000FFFF",
		Bold:           true,
		Outline:        1.0,
		Shadow:         0.5,
		MarginV:        10,
		MarginLeft:     15,
		MarginRight:    20,
		Alignment:      5,
		BorderStyle:    3,
		WrapStyle:      "2",
	}

	// Test GetScriptInfo with custom wrap_style
	scriptInfo := GetScriptInfo(cfg.WrapStyle)
	assert.Contains(t, scriptInfo, "WrapStyle: 2")
	assert.Contains(t, scriptInfo, "[Script Info]")

	// Test GenerateStyles with new parameters
	styles := GenerateStyles(cfg)
	assert.Contains(t, styles, "[V4+ Styles]")
	assert.Contains(t, styles, "Style: Default")
	assert.Contains(t, styles, "Style: Default_1")

	// Verify new parameters appear in style line
	// Format: Style: Name,Font,Size,Color,...,BorderStyle,Outline,Shadow,Alignment,MarginL,MarginR,MarginV,Encoding
	assert.Contains(t, styles, ",3,1.0,0.5,5,15,20,10,1") // BorderStyle=3, Alignment=5, MarginL=15, MarginR=20, MarginV=10
}

func TestGenerateStylesWithDefaults(t *testing.T) {
	cfg := config.ASSStyleConfig{
		Enabled:        true,
		PrimaryFont:    "Arial",
		PrimarySize:    20,
		PrimaryColor:   "&H00FFFFFF",
		SecondaryFont:  "Arial",
		SecondarySize:  15,
		SecondaryColor: "&H0000FFFF",
		Bold:           false,
		Outline:        1.0,
		Shadow:         0.5,
		MarginV:        10,
		// Zero margins are explicit configuration, not "unset": this is a
		// hand-built config, while the loader always fills real defaults.
	}

	styles := GenerateStyles(cfg)

	// Structural defaults: Alignment=2, BorderStyle=1; margins stay zero.
	assert.Contains(t, styles, ",1,1.0,0.5,2,0,0,10,1")
}

func TestGetScriptInfoDefaultWrapStyle(t *testing.T) {
	scriptInfo := GetScriptInfo("")
	assert.Contains(t, scriptInfo, "WrapStyle: 0")
	assert.Contains(t, scriptInfo, "YCbCr Matrix: None")
	assert.Contains(t, scriptInfo, "PlayResX: 1920")
	assert.Contains(t, scriptInfo, "PlayResY: 1080")
}

func TestGenerateStylesAdaptsScaleAndMargin(t *testing.T) {
	cfg := config.ASSStyleConfig{
		PrimaryFont: "PingFang SC", PrimarySize: 52, PrimaryColor: "&H08EFECEA",
		SecondaryFont: "Helvetica Neue", SecondarySize: 36, SecondaryColor: "&H087BC0E5",
		Outline: 1.4, OutlineColor: "&H30141414", Shadow: 1.5, BackColor: "&H80000000",
		MarginV: 32, MarginLeft: 80, MarginRight: 80, Alignment: 2, BorderStyle: 1,
		ScaleX: 90, Spacing: 1.5, LineHeightRatio: 1.23, BlockPadding: 16, CollisionGap: 12,
	}
	styles := generateStyles(cfg, 1.5, -120)
	assert.Contains(t, styles, "Style: Default,PingFang SC,78,")
	assert.Contains(t, styles, "Style: Eng,Helvetica Neue,54,")
	assert.Contains(t, styles, ",1,1.4,1.5,2,80,80,-120,1")
	// ScaleX and Spacing come from config in the ScaleY/Spacing columns.
	assert.Contains(t, styles, ",1.4,1.5,2,", "outline fields must stay unchanged")
	assert.Contains(t, styles, ",0,0,0,90,100,1.5,0,1,")
}

func TestGetScriptInfoWithPlayRes(t *testing.T) {
	scriptInfo := GetScriptInfoWithPlayRes("2", 1280, 720)
	assert.Contains(t, scriptInfo, "WrapStyle: 2")
	assert.Contains(t, scriptInfo, "PlayResX: 1280")
	assert.Contains(t, scriptInfo, "PlayResY: 720")
	// Invalid values fall back to the default canvas rather than emitting zero.
	fallback := GetScriptInfoWithPlayRes("", 0, -1)
	assert.Contains(t, fallback, "PlayResX: 1920")
	assert.Contains(t, fallback, "PlayResY: 1080")
}

func TestGenerateStylesPreservesZeroMargins(t *testing.T) {
	cfg := config.ASSStyleConfig{
		Enabled:     true,
		PrimaryFont: "Arial", PrimarySize: 52, PrimaryColor: "&H00FFFFFF",
		SecondaryFont: "Arial", SecondarySize: 36, SecondaryColor: "&H00FFFFFF",
		Outline: 1.4, Shadow: 1.5, MarginV: 0, MarginLeft: 0, MarginRight: 0,
		Alignment: 2, BorderStyle: 1, ScaleX: 100, LineHeightRatio: 1.23,
	}
	styles := GenerateStyles(cfg)
	// Explicit zero margins must reach the style line unchanged; the loader,
	// not the formatter, is responsible for defaults.
	assert.Contains(t, styles, ",2,0,0,0,1")
	assert.NotContains(t, styles, ",2,10,10,")
}

func TestGenerateStylesNeutralWhenDisabled(t *testing.T) {
	cfg := config.ASSStyleConfig{
		Enabled: false, PlayResX: 1920, PlayResY: 1080,
		PrimaryFont: "PingFang SC", PrimarySize: 52, PrimaryColor: "&H08EFECEA",
		SecondaryFont: "Helvetica Neue", SecondarySize: 36, SecondaryColor: "&H087BC0E5",
		Outline: 1.4, Shadow: 1.5, MarginV: 32, MarginLeft: 80, MarginRight: 80,
		Alignment: 2, BorderStyle: 1, ScaleX: 100, LineHeightRatio: 1.23,
	}
	styles := GenerateStyles(cfg)
	// All structural names remain available so events never reference a
	// missing style; custom fonts/colors are replaced by neutral defaults.
	for _, want := range []string{
		"Style: Default,Arial,52,&H00FFFFFF", "Style: Eng,Arial,36,&H00FFFFFF",
		"Style: Default_1,Arial,36,", "Style: TopDefault,Arial,52,", "Style: TopEng,Arial,36,",
	} {
		assert.Contains(t, styles, want)
	}
	for _, unwanted := range []string{"PingFang SC", "Helvetica Neue", "&H08EFECEA", "&H087BC0E5"} {
		assert.NotContains(t, styles, unwanted)
	}
}
