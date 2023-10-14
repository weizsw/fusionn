package common

import (
	"fmt"
	"fusionn/internal/consts"
	"os"
	"path/filepath"
	"strings"
)

func GetTmpSubtitleFullPath(filename string) (string, error) {
	currentDir, err := os.Getwd()
	if err != nil {
		fmt.Println("Error:", err)
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

func IsCHS(lan string, title string) bool {
	return (lan == consts.CHS_LAN) && (title == consts.CHS_TITLE)
}

func IsCHT(lan string, title string) bool {
	return (lan == consts.CHT_LAN || lan == consts.CHI_LAN) && (title == consts.CHT_TITLE)
}

func IsEng(lan string, title string) bool {
	return (lan == consts.ENG_LAN) && (title == consts.ENG_TITLE)
}
