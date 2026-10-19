package subtitle

import (
	"fmt"
	"math"
	"strings"

	"github.com/fusionn/internal/config"
)

const stylesHeaderTemplate = `[V4+ Styles]
Format: Name, Fontname, Fontsize, PrimaryColour, SecondaryColour, OutlineColour, BackColour, Bold, Italic, Underline, StrikeOut, ScaleX, ScaleY, Spacing, Angle, BorderStyle, Outline, Shadow, Alignment, MarginL, MarginR, MarginV, Encoding
`

const (
	assPlayResX = 1920
	assPlayResY = 1080
)

// Shared bilingual style names. Default/Default_1 are the names used by the
// DuoSubs and dual-language paths; Eng/TopDefault/TopEng are used by the LLM
// renderer. Default_1 is an alias of Eng so both event vocabularies resolve to
// the same English appearance.
const (
	styleNameDefault    = "Default"
	styleNameDefault1   = "Default_1"
	styleNameEng        = "Eng"
	styleNameTopDefault = "TopDefault"
	styleNameTopEng     = "TopEng"
)

// GenerateStyles generates the complete V4+ Styles section from config.
//
// The base style names (Default, Eng, Default_1, TopDefault, TopEng) are always
// emitted because dialogue events reference them. ass_style.enabled=false
// disables the custom appearance, not the structural styles: neutral defaults
// are used instead so disabling custom styling never leaves dangling style
// references.
func GenerateStyles(cfg config.ASSStyleConfig) string {
	if !cfg.Enabled {
		cfg = neutralStyle(cfg)
	}
	return generateStyles(cfg, 1, cfg.MarginV)
}

// neutralStyle returns structural defaults while preserving the configured
// canvas. It is shared with the native renderer's disabled-style path.
func neutralStyle(cfg config.ASSStyleConfig) config.ASSStyleConfig {
	playResX, playResY := cfg.PlayResX, cfg.PlayResY
	if playResX <= 0 {
		playResX = assPlayResX
	}
	if playResY <= 0 {
		playResY = assPlayResY
	}
	return config.ASSStyleConfig{
		Enabled:         false,
		PlayResX:        playResX,
		PlayResY:        playResY,
		PrimaryFont:     "Arial",
		PrimarySize:     52,
		PrimaryColor:    "&H00FFFFFF",
		SecondaryFont:   "Arial",
		SecondarySize:   36,
		SecondaryColor:  "&H00FFFFFF",
		Outline:         1,
		OutlineColor:    "&H00000000",
		Shadow:          0,
		BackColor:       "&H00000000",
		MarginV:         32,
		MarginLeft:      80,
		MarginRight:     80,
		Alignment:       2,
		BorderStyle:     1,
		WrapStyle:       "0",
		ScaleX:          100,
		Spacing:         0,
		LineHeightRatio: 1.23,
		BlockPadding:    16,
		CollisionGap:    12,
	}
}

// generateStyles generates the shared styles for a playback canvas. scale
// compensates the reference 16:9 playback canvas on wider media; marginV is the
// already-adapted vertical margin in script coordinates.
func generateStyles(cfg config.ASSStyleConfig, scale float64, marginV int) string {
	var sb strings.Builder

	sb.WriteString(stylesHeaderTemplate)

	primarySize := int(math.Round(float64(cfg.PrimarySize) * scale))
	secondarySize := int(math.Round(float64(cfg.SecondarySize) * scale))
	bottomAlignment := cfg.Alignment
	if bottomAlignment < 1 || bottomAlignment > 9 {
		bottomAlignment = 2
	}
	topAlignment := bottomAlignment
	if bottomAlignment < 4 {
		topAlignment = bottomAlignment + 6
	}

	// Default style (Primary - Chinese, bottom)
	sb.WriteString(generateStyleLine(styleNameDefault, cfg.PrimaryFont, primarySize, cfg.PrimaryColor, bottomAlignment, marginV, cfg))

	// Eng style (Secondary - English, bottom)
	sb.WriteString(generateStyleLine(styleNameEng, cfg.SecondaryFont, secondarySize, cfg.SecondaryColor, bottomAlignment, marginV, cfg))

	// Default_1 is the DuoSubs/dual-language name for the English style.
	sb.WriteString(generateStyleLine(styleNameDefault1, cfg.SecondaryFont, secondarySize, cfg.SecondaryColor, bottomAlignment, marginV, cfg))

	// Top styles for native screen-top signs and background audio.
	sb.WriteString(generateStyleLine(styleNameTopDefault, cfg.PrimaryFont, primarySize, cfg.PrimaryColor, topAlignment, marginV, cfg))
	sb.WriteString(generateStyleLine(styleNameTopEng, cfg.SecondaryFont, secondarySize, cfg.SecondaryColor, topAlignment, marginV, cfg))

	sb.WriteString("\n")
	return sb.String()
}

// generateStyleLine generates a single Style: line.
func generateStyleLine(name, font string, size int, color string, alignment, marginV int, cfg config.ASSStyleConfig) string {
	bold := 0
	if cfg.Bold {
		bold = 1
	}

	// Margins are emitted verbatim. The loader always populates them, and an
	// explicit zero margin is valid configuration (flush to the edge).
	marginL := cfg.MarginLeft
	marginR := cfg.MarginRight
	borderStyle := cfg.BorderStyle
	if borderStyle == 0 {
		borderStyle = 1
	}
	scaleX := cfg.ScaleX
	if scaleX == 0 {
		scaleX = 100
	}
	outlineColor := cfg.OutlineColor
	if outlineColor == "" {
		outlineColor = "&H00000000"
	}
	backColor := cfg.BackColor
	if backColor == "" {
		backColor = "&H00000000"
	}

	return fmt.Sprintf(
		"Style: %s,%s,%d,%s,&H000000FF,%s,%s,%d,0,0,0,%d,100,%.1f,0,%d,%.1f,%.1f,%d,%d,%d,%d,1\n",
		name,
		font,
		size,
		color,
		outlineColor,
		backColor,
		bold,
		scaleX,
		cfg.Spacing,
		borderStyle,
		cfg.Outline,
		cfg.Shadow,
		alignment,
		marginL,
		marginR,
		marginV,
	)
}

// GetScriptInfo returns the Script Info section with configurable WrapStyle at
// the default 1920x1080 canvas.
func GetScriptInfo(wrapStyle string) string {
	return GetScriptInfoWithPlayRes(wrapStyle, assPlayResX, assPlayResY)
}

// GetScriptInfoWithPlayRes returns the Script Info section for an explicit
// script canvas.
func GetScriptInfoWithPlayRes(wrapStyle string, playResX, playResY int) string {
	if wrapStyle == "" {
		wrapStyle = "0"
	}
	if playResX <= 0 {
		playResX = assPlayResX
	}
	if playResY <= 0 {
		playResY = assPlayResY
	}
	return fmt.Sprintf(`[Script Info]
; Script generated by pysubs2
; https://pypi.python.org/pypi/pysubs2
WrapStyle: %s
ScaledBorderAndShadow: yes
Collisions: Normal
ScriptType: v4.00+
YCbCr Matrix: None
PlayResX: %d
PlayResY: %d

`, wrapStyle, playResX, playResY)
}
