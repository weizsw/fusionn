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
		Enabled:       true,
		PrimaryFont:   "Arial",
		PrimarySize:   20,
		PrimaryColor:  "&H00FFFFFF",
		SecondaryFont: "Arial",
		SecondarySize: 15,
		SecondaryColor: "&H0000FFFF",
		Bold:          false,
		Outline:       1.0,
		Shadow:        0.5,
		MarginV:       10,
		// Leave new fields at zero to test defaults
	}

	styles := GenerateStyles(cfg)
	
	// Verify defaults: Alignment=2, MarginL=10, MarginR=10, BorderStyle=1
	assert.Contains(t, styles, ",1,1.0,0.5,2,10,10,10,1")
}

func TestGetScriptInfoDefaultWrapStyle(t *testing.T) {
	scriptInfo := GetScriptInfo("")
	assert.Contains(t, scriptInfo, "WrapStyle: 0")
	assert.Contains(t, scriptInfo, "PlayResX: 384")
	assert.Contains(t, scriptInfo, "PlayResY: 288")
}
