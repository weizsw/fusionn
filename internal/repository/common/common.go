package common

import (
	"bufio"
	"fmt"
	"fusionn/internal/consts"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/asticode/go-astisub"
	"github.com/gofiber/fiber/v2/log"
	"google.golang.org/protobuf/proto"
)

func GetTmpSubtitleFullPath(filename string) (string, error) {
	currentDir, err := os.Getwd()
	if err != nil {
		log.Error("Error:", err)
		return "", err
	}
	return fmt.Sprintf("%s%s%s.srt", currentDir, consts.TMP_DIR, filename), nil
}

func GetTmpDirPath() (string, error) {
	currentDir, err := os.Getwd()
	if err != nil {
		log.Error("Error:", err)
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
	var simplifiedRegex = regexp.MustCompile(`(?i)(simplified|简体|简|chi)`)
	isCHSTitle := simplifiedRegex.MatchString(title)
	return (lan == consts.CHS_LAN || lan == consts.CHI_LAN) && isCHSTitle
}

func IsCht(lan string, title string) bool {
	var traditionalRegex = regexp.MustCompile(`(?i)(traditional|繁體|繁|chi|hong kong)`)
	isCHTTitle := traditionalRegex.MatchString(title)
	return (lan == consts.CHT_LAN || lan == consts.CHI_LAN) && isCHTTitle
}

func IsEng(lan string, title string) bool {
	var englishRegex = regexp.MustCompile(`(?i)english\(sdh\)|sdh|english|^$`)
	isEngTitle := englishRegex.MatchString(title)
	return (lan == consts.ENG_LAN) && isEngTitle
}

func IsSdh(title string) bool {
	var sdhRegex = regexp.MustCompile(`(?i)english\(sdh\)|sdh`)
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
		log.Error("Error opening file:", err)
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
		log.Error("Error scanning file:", err)
		return nil, err
	}

	return lines, nil
}

func WriteFile(lines []string, filePath string) error {
	// Open the file for writing
	file, err := os.Create(filePath)
	if err != nil {
		log.Error("Error creating file:", err)
		return err
	}
	defer file.Close()

	// Create a buffered writer
	writer := bufio.NewWriter(file)

	// Write each line to the file
	for _, line := range lines {
		_, err := writer.WriteString(line + "\n")
		if err != nil {
			log.Error("Error writing line:", err)
			return err
		}
	}

	// Flush the writer to ensure all data is written to the file
	err = writer.Flush()
	if err != nil {
		log.Error("Error flushing writer:", err)
		return err
	}

	log.Info("File written successfully:", filePath)
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
			log.Info("Deleted file: %s\n", filePath)
		}
	}

	return nil
}

func AddingStyleToAss(assSub *astisub.Subtitles) *astisub.Subtitles {
	if assSub == nil {
		return assSub
	}

	resX := 3840
	resY := 2160
	assSub.Metadata = &astisub.Metadata{}
	assSub.Metadata.Title = "Default Aegisub file"
	assSub.Metadata.SSAScriptType = "v4.00+"
	assSub.Metadata.SSAWrapStyle = "0"
	assSub.Metadata.SSAPlayResX = &resX
	assSub.Metadata.SSAPlayResY = &resY
	var (
		primaryColor   *astisub.Color
		secondaryColor *astisub.Color
		outlineColor   *astisub.Color
		backColor      *astisub.Color
		err            error
	)

	primaryColor, err = ParseASSColor("&H00FFFFFF")
	if err != nil {
		log.Error("Error parsing primarycolor:", err)
		primaryColor = &astisub.Color{
			Alpha: 0,
			Blue:  255,
			Green: 255,
			Red:   255,
		}
	}
	secondaryColor, err = ParseASSColor("&H000000FF")
	if err != nil {
		log.Error("Error parsing secondarycolor:", err)
		secondaryColor = &astisub.Color{
			Alpha: 0,
			Blue:  0,
			Green: 0,
			Red:   255,
		}
	}
	outlineColor, err = ParseASSColor("&H00000000")
	if err != nil {
		log.Error("Error parsing outlinecolor:", err)
		outlineColor = &astisub.Color{
			Alpha: 0,
			Blue:  0,
			Green: 0,
			Red:   0,
		}
	}
	backColor, err = ParseASSColor("&H00000000")
	if err != nil {
		log.Error("Error parsing backcolor:", err)
		backColor = &astisub.Color{
			Alpha: 0,
			Blue:  0,
			Green: 0,
			Red:   0,
		}
	}
	boarderStyle := 1
	alignment := 2
	marginLeft, marginRight := 15, 15
	marginVertical := 11
	encoding := 1
	defaultStyle := &astisub.StyleAttributes{
		SSAFontName:        "方正黑体简体",
		SSAFontSize:        proto.Float64(65),
		SSAPrimaryColour:   primaryColor,
		SSASecondaryColour: secondaryColor,
		SSAOutlineColour:   outlineColor,
		SSABackColour:      backColor,
		SSABold:            proto.Bool(false),
		SSAItalic:          proto.Bool(false),
		SSAUnderline:       proto.Bool(false),
		SSAStrikeout:       proto.Bool(false),
		SSAScaleX:          proto.Float64(104.046),
		SSAScaleY:          proto.Float64(100),
		SSASpacing:         proto.Float64(0),
		SSAAngle:           proto.Float64(0),
		SSABorderStyle:     &boarderStyle,
		SSAOutline:         proto.Float64(0.9975),
		SSAShadow:          proto.Float64(0.9975),
		SSAAlignment:       &alignment,
		SSAMarginLeft:      &marginLeft,
		SSAMarginRight:     &marginRight,
		SSAMarginVertical:  &marginVertical,
		SSAEncoding:        &encoding,
	}
	assSub.Styles = map[string]*astisub.Style{
		"Default": {
			ID:          "Default",
			InlineStyle: defaultStyle,
		},
	}
	for _, item := range assSub.Items {
		item.Style = &astisub.Style{
			ID: "Default",
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
		return &astisub.Color{}, fmt.Errorf("invalid color format: %v", err)
	}

	// Extract color components
	alpha := uint8((value >> 24) & 0xFF)
	blue := uint8((value >> 16) & 0xFF)
	green := uint8((value >> 8) & 0xFF)
	red := uint8(value & 0xFF)

	// ASS format uses ABGR, but we want ARGB, so we need to swap blue and red
	return &astisub.Color{
		Alpha: alpha,
		Blue:  red,
		Green: green,
		Red:   blue,
	}, nil
}
