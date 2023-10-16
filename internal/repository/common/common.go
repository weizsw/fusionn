package common

import (
	"fmt"
	"fusionn/internal/consts"
	"log"
	"os"
	"path"
	"path/filepath"
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
	return (lan == consts.CHS_LAN || lan == consts.CHI_LAN) && (title == consts.CHS_TITLE)
}

func IsCHT(lan string, title string) bool {
	return (lan == consts.CHT_LAN || lan == consts.CHI_LAN) && (title == consts.CHT_TITLE)
}

func IsEng(lan string, title string) bool {
	return (lan == consts.ENG_LAN) && (title == consts.ENG_TITLE)
}
