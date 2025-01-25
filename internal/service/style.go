package service

import (
	"fmt"
	"fusionn/config"
	"fusionn/internal/consts"
	"fusionn/logger"
	"fusionn/utils"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/asticode/go-astisub"
	"go.uber.org/zap"
	"google.golang.org/protobuf/proto"
)

type StyleService interface {
	AddStyle(sub *astisub.Subtitles, width, height int) *astisub.Subtitles
	FontSubSet(filePath string) error
	ReduceMargin(sub *astisub.Subtitles, engMargin, defaultMargin string) *astisub.Subtitles
	ReplaceSpecialCharacters(sub *astisub.Subtitles) *astisub.Subtitles
	RemovePunctuation(sub *astisub.Subtitles) *astisub.Subtitles
	ReduceMarginV2(ass *ASSContent, defaultMargin, engMargin string) *ASSContent
}

type styleService struct{}

func NewStyleService() *styleService {
	return &styleService{}
}

func (s *styleService) ReduceMarginV2(ass *ASSContent, defaultMargin, engMargin string) *ASSContent {
	var modifiedEvents []string
	// Calculate available width based on resolution and margins
	availableWidth := 1920 - 40 // 40 is total horizontal margins (20 left + 20 right)
	pixelsPerCharEng := 20.2    // 1880/93 pixels per character for English text

	skipNext := false

	// Process all lines
	for _, line := range ass.Events {
		if strings.HasPrefix(line, "Format:") {
			modifiedEvents = append(modifiedEvents, line)
			continue
		}
		if !strings.HasPrefix(line, "Dialogue:") {
			modifiedEvents = append(modifiedEvents, line)
			continue
		}

		// If this line should be skipped (Default line after skipped Eng line)
		if skipNext {
			modifiedEvents = append(modifiedEvents, line)
			skipNext = false
			continue
		}

		fields := strings.Split(line, ",")
		if len(fields) < 10 {
			modifiedEvents = append(modifiedEvents, line)
			continue
		}

		// Get the text part
		text := strings.Join(fields[9:], ",")
		style := fields[3]

		// Check if we should skip this line and the next
		if style == "Eng" {
			// Skip if the line contains {an8}
			if strings.Contains(strings.ToLower(text), "{\\an8}") {
				modifiedEvents = append(modifiedEvents, line)
				skipNext = true
				continue
			}

			// Check text length for English lines
			cleanText := regexp.MustCompile(`{\\[^}]*}`).ReplaceAllString(text, "")
			textLength := float64(len(cleanText)) * pixelsPerCharEng

			// Skip if text length exceeds available width
			if textLength > float64(availableWidth) {
				modifiedEvents = append(modifiedEvents, line)
				skipNext = true
				continue
			}
		}

		// Join the first 9 fields
		prefix := strings.Join(fields[:9], ",")

		// Add position tag based on style
		switch style {
		case "Default":
			text = defaultMargin + text
		case "Eng":
			text = engMargin + text
		}

		// Reconstruct the line
		modifiedLine := prefix + "," + text
		modifiedEvents = append(modifiedEvents, modifiedLine)
	}

	ass.Events = modifiedEvents
	return ass
}

func (s *styleService) RemovePunctuation(sub *astisub.Subtitles) *astisub.Subtitles {
	// Define full-width punctuation marks to replace
	punctuations := map[rune]struct{}{
		'，': {}, // Full-width comma
		'。': {}, // Full-width period
		'？': {}, // Full-width question mark
		'！': {}, // Full-width exclamation mark
		'；': {}, // Full-width semicolon
		'：': {}, // Full-width colon
		'、': {}, // Ideographic comma
		'…': {}, // Ellipsis
		'～': {}, // Full-width tilde
		'「': {}, // Left corner bracket
		'」': {}, // Right corner bracket
		'『': {}, // Left white corner bracket
		'』': {}, // Right white corner bracket
		'（': {}, // Full-width left parenthesis
		'）': {}, // Full-width right parenthesis
		'《': {}, // Left double angle bracket
		'》': {}, // Right double angle bracket
		'“': {}, // Full-width left quotation mark
		'”': {}, // Full-width right quotation mark
		'—': {}, // Full-width dash
	}

	for i := range sub.Items {
		for j := range sub.Items[i].Lines {
			for k := range sub.Items[i].Lines[j].Items {
				// Create a string builder for efficient string manipulation
				var result strings.Builder
				text := sub.Items[i].Lines[j].Items[k].Text

				// Iterate through each rune in the text
				for _, r := range text {
					if r == '-' {
						continue
					} else if _, isPunct := punctuations[r]; isPunct {
						result.WriteRune(' ') // Replace punctuation with space
					} else {
						result.WriteRune(r)
					}
				}

				sub.Items[i].Lines[j].Items[k].Text = result.String()
			}
		}
	}
	return sub
}

func (s *styleService) ReplaceSpecialCharacters(sub *astisub.Subtitles) *astisub.Subtitles {
	for i := range sub.Items {
		for j := range sub.Items[i].Lines {
			for k := range sub.Items[i].Lines[j].Items {
				sub.Items[i].Lines[j].Items[k].Text = utils.ReplaceSpecialCharacters(sub.Items[i].Lines[j].Items[k].Text)
			}
		}
	}
	return sub
}

func (s *styleService) ReduceMargin(sub *astisub.Subtitles, engMargin, defaultMargin string) *astisub.Subtitles {
	if sub == nil {
		return sub
	}

	// Calculate available width based on resolution and margins
	availableWidth := 1920 - 40 // 40 is total horizontal margins (20 left + 20 right)

	// Based on known max length of 93 chars at font size 12
	pixelsPerCharEng := 20.2 // 1880/93 pixels per character for English text

	// First pass: process English lines and find matching default lines
	for _, engItem := range sub.Items {
		if engItem.Style != nil && engItem.Style.ID == "Eng" {
			for _, line := range engItem.Lines {
				if len(line.Items) > 0 {
					// Handle {\an8} case differently
					if strings.Contains(line.Items[0].Text, "{\\an8}") {
						line.Items[0].Text = defaultMargin + line.Items[0].Text

						// Find matching default line
						for _, defaultItem := range sub.Items {
							if defaultItem.Style == nil || defaultItem.Style.ID != "Eng" {
								if defaultItem.StartAt == engItem.StartAt && defaultItem.EndAt == engItem.EndAt {
									for _, defaultLine := range defaultItem.Lines {
										if len(defaultLine.Items) > 0 {
											defaultLine.Items[0].Text = engMargin + defaultLine.Items[0].Text
											break
										}
									}
									break
								}
							}
						}
						break
					}

					// Regular case (existing code)
					var textLength float64
					for _, lineItem := range line.Items {
						textLength += float64(len(lineItem.Text)) * pixelsPerCharEng
					}

					// Only proceed if English margin won't cause wrapping
					if textLength <= float64(availableWidth) {
						line.Items[0].Text = engMargin + line.Items[0].Text

						// Find matching default line
						for _, defaultItem := range sub.Items {
							if defaultItem.Style == nil || defaultItem.Style.ID != "Eng" {
								if defaultItem.StartAt == engItem.StartAt && defaultItem.EndAt == engItem.EndAt {
									for _, defaultLine := range defaultItem.Lines {
										if len(defaultLine.Items) > 0 {
											defaultLine.Items[0].Text = defaultMargin + defaultLine.Items[0].Text
											break
										}
									}
									break
								}
							}
						}
					}
					break
				}
			}
		}
	}
	return sub
}

func (s *styleService) FontSubSet(filePath string) error {
	if !config.C.Subset.Enabled {
		return nil
	}

	assfonts := consts.ASSFONTS_PATH
	if env := os.Getenv("DOCKER_ENV"); env == "" || env == "false" {
		assfonts = filepath.Join(".", "asset", "bin", "assfonts")
	}

	outputPath := filepath.Dir(filePath)
	fontPath := filepath.Join(".", "asset", "fonts")
	cmd := exec.Command(assfonts, "-i", filePath, "-f", fontPath, "-o", outputPath)
	if config.C.Subset.EmbedOnly {
		cmd.Args = append(cmd.Args, "-e")
	}
	logger.L.Info("Running command:", zap.String("command", cmd.String()))

	output, err := cmd.CombinedOutput()
	logger.L.Info("Command output:", zap.String("output", string(output)))

	if err != nil {
		logger.L.Error("Error running command:", zap.Error(err))
		return err
	}

	fullPathWithoutExt := utils.GetFullPathWithoutExtension(filePath)
	subsetPath := fmt.Sprintf("%s_subsetted", fullPathWithoutExt)

	err = os.RemoveAll(subsetPath)
	if err != nil {
		logger.L.Error("Error removing subset file:", zap.Error(err))
	}
	logger.L.Info("Subset file removed:", zap.String("path", subsetPath))

	err = os.RemoveAll(filePath)
	if err != nil {
		logger.L.Error("Error removing original file:", zap.Error(err))
	}
	logger.L.Info("Original file removed:", zap.String("path", filePath))
	return nil
}

func (s *styleService) AddStyle(sub *astisub.Subtitles, width, height int) *astisub.Subtitles {
	if sub == nil {
		return sub
	}

	chsFS, _ := s.calculateScaling(config.C.Style.ChsStyle.FontSize, width, height)
	engFS, _ := s.calculateScaling(config.C.Style.EngStyle.FontSize, width, height)

	// Initialize metadata
	sub.Metadata = s.initializeMetadata()

	// Create default style
	defaultStyle := s.createStyleAttributes(
		config.C.Style.ChsStyle.PrimaryColor,
		config.C.Style.SecondaryColor,
		config.C.Style.OutlineColor,
		config.C.Style.BackColor,
		styleConfig{
			fontName: config.C.Style.ChsStyle.FontName,
			fontSize: chsFS,
			bold:     config.C.Style.ChsStyle.Bold,
			scaleX:   90,
			scaleY:   100,
		},
	)

	// Create English style
	engStyle := s.createStyleAttributes(
		config.C.Style.EngStyle.PrimaryColor,
		config.C.Style.SecondaryColor,
		config.C.Style.OutlineColor,
		config.C.Style.BackColor,
		styleConfig{
			fontName: config.C.Style.EngStyle.FontName,
			fontSize: engFS,
			bold:     config.C.Style.EngStyle.Bold,
			scaleX:   90,
			scaleY:   100,
		},
	)

	// Set styles
	sub.Styles = map[string]*astisub.Style{
		"Default": {ID: "Default", InlineStyle: defaultStyle},
		"Eng":     {ID: "Eng", InlineStyle: engStyle},
	}

	// Set default style for items without style
	for _, item := range sub.Items {
		if item.Style == nil {
			item.Style = &astisub.Style{ID: "Default"}
		}
	}

	return sub
}

type styleConfig struct {
	fontName string
	fontSize float64
	bold     bool
	scaleX   float64
	scaleY   float64
}

func (s *styleService) initializeMetadata() *astisub.Metadata {
	resX, resY := 384, 288
	return &astisub.Metadata{
		Title:         "Default Aegisub file",
		SSAScriptType: "v4.00+",
		SSAWrapStyle:  config.C.Style.WrapStyle,
		SSAPlayResX:   &resX,
		SSAPlayResY:   &resY,
		SSATimer:      proto.Float64(100),
	}
}

func (s *styleService) createStyleAttributes(primaryColorStr, secondaryColorStr, outlineColorStr, backColorStr string, cfg styleConfig) *astisub.StyleAttributes {
	// Parse colors with fallbacks
	primaryColor := s.parseColorWithFallback(primaryColorStr, astisub.Color{Blue: 197, Green: 197, Red: 197})
	secondaryColor := s.parseColorWithFallback(secondaryColorStr, astisub.Color{Blue: 255, Green: 255})
	outlineColor := s.parseColorWithFallback(outlineColorStr, astisub.Color{})
	backColor := s.parseColorWithFallback(backColorStr, astisub.Color{Alpha: 128})

	return &astisub.StyleAttributes{
		SSAFontName:        cfg.fontName,
		SSAFontSize:        proto.Float64(cfg.fontSize),
		SSAPrimaryColour:   primaryColor,
		SSASecondaryColour: secondaryColor,
		SSAOutlineColour:   outlineColor,
		SSABackColour:      backColor,
		SSABold:            proto.Bool(cfg.bold),
		SSAItalic:          proto.Bool(false),
		SSAUnderline:       proto.Bool(false),
		SSAStrikeout:       proto.Bool(false),
		SSAScaleX:          proto.Float64(cfg.scaleX),
		SSAScaleY:          proto.Float64(cfg.scaleY),
		SSASpacing:         proto.Float64(0),
		SSAAngle:           proto.Float64(0),
		SSABorderStyle:     &config.C.Style.BorderStyle,
		SSAOutline:         proto.Float64(config.C.Style.Outline),
		SSAShadow:          proto.Float64(config.C.Style.Shadow),
		SSAAlignment:       &config.C.Style.Alignment,
		SSAMarginLeft:      &config.C.Style.MarginLeft,
		SSAMarginRight:     &config.C.Style.MarginRight,
		SSAMarginVertical:  &config.C.Style.MarginVertical,
		SSAEncoding:        intPtr(1),
	}
}

func intPtr(i int) *int {
	return &i
}

func (s *styleService) parseColorWithFallback(colorStr string, fallback astisub.Color) *astisub.Color {
	color, err := s.parseASSColor(colorStr)
	if err != nil {
		logger.S.Error("Error parsing color:", err)
		return &fallback
	}
	return color
}

func (s *styleService) parseASSColor(assColor string) (*astisub.Color, error) {
	// Remove the "&H" prefix if present
	if len(assColor) > 2 && assColor[:2] == "&H" {
		assColor = assColor[2:]
	}

	// Pad with zeros if necessary
	for len(assColor) < 8 {
		assColor = "0" + assColor
	}

	// Parse the hexadecimal string
	value, err := strconv.ParseUint(assColor, 16, 32)
	if err != nil {
		return nil, fmt.Errorf("invalid color format: %v", err)
	}

	// Extract color components
	alpha := uint8((value >> 24) & 0xFF)
	blue := uint8((value >> 16) & 0xFF)
	green := uint8((value >> 8) & 0xFF)
	red := uint8(value & 0xFF)

	// Return the color in the correct order (no swapping needed)
	return &astisub.Color{
		Alpha: alpha,
		Blue:  blue,
		Green: green,
		Red:   red,
	}, nil
}

func ReplaceSpecialCharacters(inputString string) string {
	// Remove "\n"
	modifiedString := strings.ReplaceAll(inputString, "\\n", "")

	// Replace "<i>" with "{\i1}"
	modifiedString = strings.ReplaceAll(modifiedString, "<i>", "{\\i1}")

	// Replace "</i>" with "{\i0}"
	modifiedString = strings.ReplaceAll(modifiedString, "</i>", "{\\i0}")

	return modifiedString
}

func (s *styleService) calculateScaling(fontSize float64, width, height int) (float64, float64) {
	baseWidth := 1920
	baseHeight := 1080
	// 如果是4K分辨率，调整基准以保持相同比例
	if width >= 3840 {
		baseWidth = 3840
		baseHeight = 2160
	}

	ratio := math.Sqrt(float64(baseWidth*baseHeight) / float64(width*height))
	fs := int(math.Round(float64(fontSize) * ratio))
	sx := int(math.Round(float64(90) * ratio))
	return float64(fs), float64(sx)
}
