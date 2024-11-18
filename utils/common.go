package utils

import (
	"bufio"
	"fmt"
	"fusionn/internal/consts"
	"fusionn/logger"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/asticode/go-astisub"
	"google.golang.org/protobuf/proto"
)

func GetTmpSubtitleFullPath(filename string) (string, error) {
	currentDir, err := os.Getwd()
	if err != nil {
		logger.Sugar.Error("Error:", err)
		return "", err
	}
	return fmt.Sprintf("%s%s%s.srt", currentDir, consts.TMP_DIR, filename), nil
}

func GetTmpDirPath() (string, error) {
	currentDir, err := os.Getwd()
	if err != nil {
		logger.Sugar.Error("Error:", err)
		return "", err
	}
	return fmt.Sprintf("%s%s", currentDir, consts.TMP_DIR), nil
}

func ExtractFilenameWithoutExtension(path string) string {
	// Get the base filename from the path
	filename := filepath.Base(path)

	// Remove the extension from the filename
	extension := filepath.Ext(filename)
	filenameWithoutExtension := strings.TrimSuffix(filename, extension)

	return filenameWithoutExtension
}

func ExtractPathWithoutExtension(filePath string) string {
	dir, file := filepath.Split(filePath)
	extension := filepath.Ext(file)
	fileWithoutExtension := strings.TrimSuffix(file, extension)
	pathWithoutExtension := filepath.Join(dir, fileWithoutExtension)
	return pathWithoutExtension
}

func IsChs(lan string, title string) bool {
	var simplifiedRegex = regexp.MustCompile(`(?i)(simplified|简体|简)`)
	isCHSTitle := simplifiedRegex.MatchString(title)
	return (lan == consts.CHS_LAN || lan == consts.CHI_LAN) && isCHSTitle
}

func IsTraditionalChinese(lan string, title string) bool {
	var traditionalRegex = regexp.MustCompile(`(?i)(traditional|繁體|繁|chi)`)
	isCHTTitle := traditionalRegex.MatchString(title)
	return (lan == consts.CHT_LAN || lan == consts.CHI_LAN) && isCHTTitle
}

func IsCht(lan string, title string) bool {
	var traditionalRegex = regexp.MustCompile(`(?i)(traditional|繁體|繁|chi|hong kong)`)
	isCHTTitle := traditionalRegex.MatchString(title)
	return (lan == consts.CHT_LAN || lan == consts.CHI_LAN) && isCHTTitle
}

func IsEng(lan string, title string) bool {
	var englishRegex = regexp.MustCompile(`(?i)^(english|)$`)
	isEngTitle := englishRegex.MatchString(title)
	return (lan == consts.ENG_LAN) && isEngTitle
}

func IsSdh(title string) bool {
	var sdhRegex = regexp.MustCompile(`(?i)english\(sdh\)|sdh|forced`)
	return sdhRegex.MatchString(title)
}

func Floor(num int) int {
	return int(math.Floor(float64(num)/10.0)) * 10
}

func Ceil(num int) int {
	res := int(math.Ceil(float64(num)/10.0)) * 10
	if CountDigits(num) != CountDigits(res) {
		return res - 1
	}
	return res
}

func CountDigits(num int) int {
	numStr := strconv.Itoa(num)
	return len(numStr)
}

func ReadFile(filePath string) ([]string, error) {
	// Open the file
	file, err := os.Open(filePath)
	if err != nil {
		logger.Sugar.Error("Error opening file:", err)
		return nil, err
	}
	defer file.Close()

	// Create a scanner to read the file line by line
	scanner := bufio.NewScanner(file)

	// Read the file line by line and store in a []string slice
	var lines []string
	for scanner.Scan() {
		line := scanner.Text()
		lines = append(lines, line)
	}

	// Check for any scanning errors
	if err := scanner.Err(); err != nil {
		logger.Sugar.Error("Error scanning file:", err)
		return nil, err
	}

	return lines, nil
}

func WriteFile(lines []string, filePath string) error {
	// Open the file for writing
	file, err := os.Create(filePath)
	if err != nil {
		logger.Sugar.Error("Error creating file:", err)
		return err
	}
	defer file.Close()

	// Create a buffered writer
	writer := bufio.NewWriter(file)

	// Write each line to the file
	for _, line := range lines {
		_, err := writer.WriteString(line + "\n")
		if err != nil {
			logger.Sugar.Error("Error writing line:", err)
			return err
		}
	}

	// Flush the writer to ensure all data is written to the file
	err = writer.Flush()
	if err != nil {
		logger.Sugar.Error("Error flushing writer:", err)
		return err
	}

	logger.Sugar.Info("File written successfully:", filePath)
	return nil
}

func GetFullPathWithoutExtension(path string) string {
	// Get the base name of the file
	filename := filepath.Base(path)

	// Remove the extension from the filename
	extension := filepath.Ext(filename)
	base := strings.TrimSuffix(filename, extension)

	// Get the directory path
	dir := filepath.Dir(path)

	// Join the directory path and the base filename without extension
	fullPath := filepath.Join(dir, base)

	return fullPath
}

func DeleteFilesInDirectory(dirPath, fileName string) error {
	files, err := os.ReadDir(dirPath)
	if err != nil {
		return err
	}

	for _, file := range files {
		if !file.IsDir() {
			filePath := filepath.Join(dirPath, file.Name())
			if !strings.Contains(filePath, fileName) {
				continue
			}
			err := os.Remove(filePath)
			if err != nil {
				return err
			}
			logger.Sugar.Info("Deleted file: %s\n", filePath)
		}
	}

	return nil
}

func AddingStyleToAss(assSub *astisub.Subtitles) *astisub.Subtitles {
	if assSub == nil {
		return assSub
	}

	resX := 384
	resY := 288
	assSub.Metadata = &astisub.Metadata{}
	assSub.Metadata.Title = "Default Aegisub file"
	assSub.Metadata.SSAScriptType = "v4.00+"
	assSub.Metadata.SSAWrapStyle = "0"
	assSub.Metadata.SSAPlayResX = &resX
	assSub.Metadata.SSAPlayResY = &resY
	assSub.Metadata.SSATimer = proto.Float64(100)
	var (
		primaryColor   *astisub.Color
		secondaryColor *astisub.Color
		outlineColor   *astisub.Color
		backColor      *astisub.Color
		err            error
	)

	primaryColor, err = ParseASSColor("&H00C5C5C5")
	if err != nil {
		logger.Sugar.Error("Error parsing primarycolor:", err)
		primaryColor = &astisub.Color{
			Alpha: 0,
			Blue:  197,
			Green: 197,
			Red:   197,
		}
	}
	secondaryColor, err = ParseASSColor("&H0000FFFF")
	if err != nil {
		logger.Sugar.Error("Error parsing secondarycolor:", err)
		secondaryColor = &astisub.Color{
			Alpha: 0,
			Blue:  255,
			Green: 255,
			Red:   0,
		}
	}
	outlineColor, err = ParseASSColor("&H00000000")
	if err != nil {
		logger.Sugar.Error("Error parsing outlinecolor:", err)
		outlineColor = &astisub.Color{
			Alpha: 0,
			Blue:  0,
			Green: 0,
			Red:   0,
		}
	}
	backColor, err = ParseASSColor("&H80000000")
	if err != nil {
		logger.Sugar.Error("Error parsing backcolor:", err)
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
		SSAFontName:        "Microsoft YaHei",
		SSAFontSize:        proto.Float64(16),
		SSAPrimaryColour:   primaryColor,
		SSASecondaryColour: secondaryColor,
		SSAOutlineColour:   outlineColor,
		SSABackColour:      backColor,
		SSABold:            proto.Bool(false),
		SSAItalic:          proto.Bool(false),
		SSAUnderline:       proto.Bool(false),
		SSAStrikeout:       proto.Bool(false),
		SSAScaleX:          proto.Float64(100),
		SSAScaleY:          proto.Float64(100),
		SSASpacing:         proto.Float64(0),
		SSAAngle:           proto.Float64(0),
		SSABorderStyle:     &borderStyle,
		SSAOutline:         proto.Float64(2),
		SSAShadow:          proto.Float64(0),
		SSAAlignment:       &alignment,
		SSAMarginLeft:      &marginLeft,
		SSAMarginRight:     &marginRight,
		SSAMarginVertical:  &marginVertical,
		SSAEncoding:        &encoding,
	}

	// Create English style
	engPrimaryColor, err := ParseASSColor("&H0000A0DC")
	if err != nil {
		logger.Sugar.Error("Error parsing Eng primarycolor:", err)
		engPrimaryColor = &astisub.Color{
			Alpha: 0,
			Blue:  220,
			Green: 160,
			Red:   0,
		}
	}
	engSecondaryColor, err := ParseASSColor("&H0000FFFF")
	if err != nil {
		logger.Sugar.Error("Error parsing Eng secondarycolor:", err)
		engSecondaryColor = &astisub.Color{
			Alpha: 0,
			Blue:  255,
			Green: 255,
			Red:   0,
		}
	}
	engOutlineColor, err := ParseASSColor("&H00000000")
	if err != nil {
		logger.Sugar.Error("Error parsing Eng outlinecolor:", err)
		engOutlineColor = &astisub.Color{
			Alpha: 0,
			Blue:  0,
			Green: 0,
			Red:   0,
		}
	}
	engBackColor, err := ParseASSColor("&H80000000")
	if err != nil {
		logger.Sugar.Error("Error parsing Eng backcolor:", err)
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
		SSAFontName:        "Arial",
		SSAFontSize:        proto.Float64(10),
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
		SSAOutline:         proto.Float64(1),
		SSAShadow:          proto.Float64(0),
		SSAAlignment:       &engAlignment,
		SSAMarginLeft:      &engMarginLeft,
		SSAMarginRight:     &engMarginRight,
		SSAMarginVertical:  &engMarginVertical,
		SSAEncoding:        &engEncoding,
	}

	assSub.Styles = map[string]*astisub.Style{
		"Default": {
			ID:          "Default",
			InlineStyle: defaultStyle,
		},
		"Eng": {
			ID:          "Eng",
			InlineStyle: engStyle,
		},
	}

	for _, item := range assSub.Items {
		if item.Style == nil {
			item.Style = &astisub.Style{
				ID: "Default",
			}
		}
	}
	return assSub
}

func ParseASSColor(assColor string) (*astisub.Color, error) {
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
