package service

import (
	"fmt"
	"fusionn/logger"
	"fusionn/utils"
	"strconv"
	"strings"

	"github.com/asticode/go-astisub"
	"google.golang.org/protobuf/proto"
)

type StyleService interface {
	AddStyle(sub *astisub.Subtitles) *astisub.Subtitles
	FontSubSet(path string) error
	ReduceMargin(sub *astisub.Subtitles, engMargin, defaultMargin string) *astisub.Subtitles
	ReplaceSpecialCharacters(sub *astisub.Subtitles) *astisub.Subtitles
}

type styleService struct{}

func NewStyleService() *styleService {
	return &styleService{}
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

func (s *styleService) FontSubSet(path string) error {
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
	sub.Metadata.SSAWrapStyle = "0"
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

	primaryColor, err = s.parseASSColor("&H00C5C5C5")
	if err != nil {
		logger.S.Error("Error parsing primarycolor:", err)
		primaryColor = &astisub.Color{
			Alpha: 0,
			Blue:  197,
			Green: 197,
			Red:   197,
		}
	}
	secondaryColor, err = s.parseASSColor("&H0000FFFF")
	if err != nil {
		logger.S.Error("Error parsing secondarycolor:", err)
		secondaryColor = &astisub.Color{
			Alpha: 0,
			Blue:  255,
			Green: 255,
			Red:   0,
		}
	}
	outlineColor, err = s.parseASSColor("&H00000000")
	if err != nil {
		logger.S.Error("Error parsing outlinecolor:", err)
		outlineColor = &astisub.Color{
			Alpha: 0,
			Blue:  0,
			Green: 0,
			Red:   0,
		}
	}
	backColor, err = s.parseASSColor("&H80000000")
	if err != nil {
		logger.S.Error("Error parsing backcolor:", err)
		backColor = &astisub.Color{
			Alpha: 128,
			Blue:  0,
			Green: 0,
			Red:   0,
		}
	}
	borderStyle := 1
	alignment := 2
	marginLeft, marginRight := 20, 20
	marginVertical := 10
	encoding := 1
	defaultStyle := &astisub.StyleAttributes{
		SSAFontName:        "WenQuanYiMicroHei",
		SSAFontSize:        proto.Float64(19),
		SSAPrimaryColour:   primaryColor,
		SSASecondaryColour: secondaryColor,
		SSAOutlineColour:   outlineColor,
		SSABackColour:      backColor,
		SSABold:            proto.Bool(false),
		SSAItalic:          proto.Bool(false),
		SSAUnderline:       proto.Bool(false),
		SSAStrikeout:       proto.Bool(false),
		SSAScaleX:          proto.Float64(90),
		SSAScaleY:          proto.Float64(100),
		SSASpacing:         proto.Float64(0),
		SSAAngle:           proto.Float64(0),
		SSABorderStyle:     &borderStyle,
		SSAOutline:         proto.Float64(0.5),
		SSAShadow:          proto.Float64(0),
		SSAAlignment:       &alignment,
		SSAMarginLeft:      &marginLeft,
		SSAMarginRight:     &marginRight,
		SSAMarginVertical:  &marginVertical,
		SSAEncoding:        &encoding,
	}

	// Create English style
	engPrimaryColor, err := s.parseASSColor("&H0000A0DC")
	if err != nil {
		logger.S.Error("Error parsing Eng primarycolor:", err)
		engPrimaryColor = &astisub.Color{
			Alpha: 0,
			Blue:  220,
			Green: 160,
			Red:   0,
		}
	}
	engSecondaryColor, err := s.parseASSColor("&H0000FFFF")
	if err != nil {
		logger.S.Error("Error parsing Eng secondarycolor:", err)
		engSecondaryColor = &astisub.Color{
			Alpha: 0,
			Blue:  255,
			Green: 255,
			Red:   0,
		}
	}
	engOutlineColor, err := s.parseASSColor("&H00000000")
	if err != nil {
		logger.S.Error("Error parsing Eng outlinecolor:", err)
		engOutlineColor = &astisub.Color{
			Alpha: 0,
			Blue:  0,
			Green: 0,
			Red:   0,
		}
	}
	engBackColor, err := s.parseASSColor("&H80000000")
	if err != nil {
		logger.S.Error("Error parsing Eng backcolor:", err)
		engBackColor = &astisub.Color{
			Alpha: 128,
			Blue:  0,
			Green: 0,
			Red:   0,
		}
	}

	engBorderStyle := 1
	engAlignment := 2
	engMarginLeft, engMarginRight := 20, 20
	engMarginVertical := 10
	engEncoding := 1

	engStyle := &astisub.StyleAttributes{
		SSAFontName:        "WenQuanYiMicroHei",
		SSAFontSize:        proto.Float64(12),
		SSAPrimaryColour:   engPrimaryColor,
		SSASecondaryColour: engSecondaryColor,
		SSAOutlineColour:   engOutlineColor,
		SSABackColour:      engBackColor,
		SSABold:            proto.Bool(false),
		SSAItalic:          proto.Bool(false),
		SSAUnderline:       proto.Bool(false),
		SSAStrikeout:       proto.Bool(false),
		SSAScaleX:          proto.Float64(100),
		SSAScaleY:          proto.Float64(100),
		SSASpacing:         proto.Float64(0),
		SSAAngle:           proto.Float64(0),
		SSABorderStyle:     &engBorderStyle,
		SSAOutline:         proto.Float64(0.5),
		SSAShadow:          proto.Float64(0),
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
