package common

import (
	"bufio"
	"fmt"
	"fusionn/internal/consts"
	"log"
	"math"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
)

func GetTmpSubtitleFullPath(filename string) (string, error) {
	currentDir, err := os.Getwd()
	if err != nil {
		log.Println("Error:", err)
		return "", err
	}
	return fmt.Sprintf("%s%s%s.srt", currentDir, consts.TMP_DIR, filename), nil
}
func ExtractFilenameWithoutExtension(path string) string {
	// Get the base filename from the path
	filename := filepath.Base(path)

	// Remove the extension from the filename
	extension := filepath.Ext(filename)
	filenameWithoutExtension := strings.TrimSuffix(filename, extension)

	return filenameWithoutExtension
}

func GetFilenameWithoutExtension(filepath string) string {
	filename := path.Base(filepath)
	extension := path.Ext(filename)
	return filename[:len(filename)-len(extension)]
}

func ExtractPathWithoutExtension(filePath string) string {
	dir, file := filepath.Split(filePath)
	extension := filepath.Ext(file)
	fileWithoutExtension := strings.TrimSuffix(file, extension)
	pathWithoutExtension := filepath.Join(dir, fileWithoutExtension)
	return pathWithoutExtension
}

func IsCHS(lan string, title string) bool {
	return (lan == consts.CHS_LAN || lan == consts.CHI_LAN) && (title == consts.CHS_TITLE || title == consts.CHS_TITLE_II)
}

func IsCHT(lan string, title string) bool {
	return (lan == consts.CHT_LAN || lan == consts.CHI_LAN) && (title == consts.CHT_TITLE || title == consts.CHT_TITLE_II)
}

func IsEng(lan string, title string) bool {
	return (lan == consts.ENG_LAN) && (title == consts.ENG_TITLE || title == consts.ENG_TITLE_II)
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
		log.Println("Error opening file:", err)
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
		log.Println("Error scanning file:", err)
		return nil, err
	}

	return lines, nil
}

func WriteFile(lines []string, filePath string) error {
	// Open the file for writing
	file, err := os.Create(filePath)
	if err != nil {
		log.Println("Error creating file:", err)
		return err
	}
	defer file.Close()

	// Create a buffered writer
	writer := bufio.NewWriter(file)

	// Write each line to the file
	for _, line := range lines {
		_, err := writer.WriteString(line + "\n")
		if err != nil {
			log.Println("Error writing line:", err)
			return err
		}
	}

	// Flush the writer to ensure all data is written to the file
	err = writer.Flush()
	if err != nil {
		log.Println("Error flushing writer:", err)
		return err
	}

	log.Println("File written successfully:", filePath)
	return nil
}
