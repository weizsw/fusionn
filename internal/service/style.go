package service

import (
	"fmt"
	"fusionn/config"
	"fusionn/internal/consts"
	"fusionn/logger"
	"fusionn/utils"
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
	AddStyle(sub *astisub.Subtitles) *astisub.Subtitles
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

func (s *styleService) AddStyle(sub *astisub.Subtitles) *astisub.Subtitles {
	if sub == nil {
		return sub
	}

	resX := 384
	resY := 288
	sub.Metadata = &astisub.Metadata{}
	sub.Metadata.Title = "Default Aegisub file"
	sub.Metadata.SSAScriptType = "v4.00+"
	sub.Metadata.SSAWrapStyle = config.C.Style.WrapStyle
	sub.Metadata.SSAPlayResX = &resX
	sub.Metadata.SSAPlayResY = &resY
	sub.Metadata.SSATimer = proto.Float64(100)
	var (
		primaryColor   *astisub.Color
		secondaryColor *astisub.Color
		outlineColor   *astisub.Color
		backColor      *astisub.Color
		err            error
	)

	primaryColor, err = s.parseASSColor(config.C.Style.ChsPrimaryColor)
	if err != nil {
		logger.S.Error("Error parsing primarycolor:", err)
		primaryColor = &astisub.Color{
			Alpha: 0,
			Blue:  197,
			Green: 197,
			Red:   197,
		}
	}
	secondaryColor, err = s.parseASSColor(config.C.Style.SecondaryColor)
	if err != nil {
		logger.S.Error("Error parsing secondarycolor:", err)
		secondaryColor = &astisub.Color{
			Alpha: 0,
			Blue:  255,
			Green: 255,
			Red:   0,
		}
	}
	outlineColor, err = s.parseASSColor(config.C.Style.OutlineColor)
	if err != nil {
		logger.S.Error("Error parsing outlinecolor:", err)
		outlineColor = &astisub.Color{
			Alpha: 0,
			Blue:  0,
			Green: 0,
			Red:   0,
		}
	}
	backColor, err = s.parseASSColor(config.C.Style.BackColor)
	if err != nil {
		logger.S.Error("Error parsing backcolor:", err)
		backColor = &astisub.Color{
			Alpha: 128,
			Blue:  0,
			Green: 0,
			Red:   0,
		}
	}
	borderStyle := config.C.Style.BorderStyle
	alignment := config.C.Style.Alignment
	marginLeft, marginRight := config.C.Style.MarginLeft, config.C.Style.MarginRight
	marginVertical := config.C.Style.MarginVertical
	outline := config.C.Style.Outline
	shadow := config.C.Style.Shadow
	encoding := 1
	defaultStyle := &astisub.StyleAttributes{
		SSAFontName:        config.C.Style.ChsFontName,
		SSAFontSize:        proto.Float64(config.C.Style.ChsFontSize),
		SSAPrimaryColour:   primaryColor,
		SSASecondaryColour: secondaryColor,
		SSAOutlineColour:   outlineColor,
		SSABackColour:      backColor,
		SSABold:            proto.Bool(config.C.Style.ChsBold),
		SSAItalic:          proto.Bool(false),
		SSAUnderline:       proto.Bool(false),
		SSAStrikeout:       proto.Bool(false),
		SSAScaleX:          proto.Float64(90),
		SSAScaleY:          proto.Float64(100),
		SSASpacing:         proto.Float64(0),
		SSAAngle:           proto.Float64(0),
		SSABorderStyle:     &borderStyle,
		SSAOutline:         proto.Float64(outline),
		SSAShadow:          proto.Float64(shadow),
		SSAAlignment:       &alignment,
		SSAMarginLeft:      &marginLeft,
		SSAMarginRight:     &marginRight,
		SSAMarginVertical:  &marginVertical,
		SSAEncoding:        &encoding,
	}

	// Create English style
	engPrimaryColor, err := s.parseASSColor(config.C.Style.EngPrimaryColor)
	if err != nil {
		logger.S.Error("Error parsing Eng primarycolor:", err)
		engPrimaryColor = &astisub.Color{
			Alpha: 0,
			Blue:  220,
			Green: 160,
			Red:   0,
		}
	}
	engSecondaryColor, err := s.parseASSColor(config.C.Style.SecondaryColor)
	if err != nil {
		logger.S.Error("Error parsing Eng secondarycolor:", err)
		engSecondaryColor = &astisub.Color{
			Alpha: 0,
			Blue:  255,
			Green: 255,
			Red:   0,
		}
	}
	engOutlineColor, err := s.parseASSColor(config.C.Style.OutlineColor)
	if err != nil {
		logger.S.Error("Error parsing Eng outlinecolor:", err)
		engOutlineColor = &astisub.Color{
			Alpha: 0,
			Blue:  0,
			Green: 0,
			Red:   0,
		}
	}
	engBackColor, err := s.parseASSColor(config.C.Style.BackColor)
	if err != nil {
		logger.S.Error("Error parsing Eng backcolor:", err)
		engBackColor = &astisub.Color{
			Alpha: 128,
			Blue:  0,
			Green: 0,
			Red:   0,
		}
	}

	engBorderStyle := config.C.Style.BorderStyle
	engAlignment := config.C.Style.Alignment
	engMarginLeft, engMarginRight := config.C.Style.MarginLeft, config.C.Style.MarginRight
	engMarginVertical := config.C.Style.MarginVertical
	engOutline := config.C.Style.Outline
	engShadow := config.C.Style.Shadow
	engEncoding := 1

	engStyle := &astisub.StyleAttributes{
		SSAFontName:        config.C.Style.EngFontName,
		SSAFontSize:        proto.Float64(config.C.Style.EngFontSize),
		SSAPrimaryColour:   engPrimaryColor,
		SSASecondaryColour: engSecondaryColor,
		SSAOutlineColour:   engOutlineColor,
		SSABackColour:      engBackColor,
		SSABold:            proto.Bool(config.C.Style.EngBold),
		SSAItalic:          proto.Bool(false),
		SSAUnderline:       proto.Bool(false),
		SSAStrikeout:       proto.Bool(false),
		SSAScaleX:          proto.Float64(100),
		SSAScaleY:          proto.Float64(100),
		SSASpacing:         proto.Float64(0),
		SSAAngle:           proto.Float64(0),
		SSABorderStyle:     &engBorderStyle,
		SSAOutline:         proto.Float64(engOutline),
		SSAShadow:          proto.Float64(engShadow),
		SSAAlignment:       &engAlignment,
		SSAMarginLeft:      &engMarginLeft,
		SSAMarginRight:     &engMarginRight,
		SSAMarginVertical:  &engMarginVertical,
		SSAEncoding:        &engEncoding,
	}

	sub.Styles = map[string]*astisub.Style{
		"Default": {
			ID:          "Default",
			InlineStyle: defaultStyle,
		},
		"Eng": {
			ID:          "Eng",
			InlineStyle: engStyle,
		},
	}

	for _, item := range sub.Items {
		if item.Style == nil {
			item.Style = &astisub.Style{
				ID: "Default",
			}
		}
	}
	return sub
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
