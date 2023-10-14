package ffmpeg

import (
	"encoding/json"
	"fmt"
	"fusionn/internal/entity"
	"fusionn/internal/repository/common"
	"os"
	"os/exec"
	"regexp"
	"strings"
)

func ExtractSubtitles(videoPath string) error {
	ffprobePath, err := exec.LookPath("ffprobe")
	if err != nil {
		return fmt.Errorf("ffprobe not found: %v", err)
	}

	cmd := exec.Command(ffprobePath, "-i", videoPath, "-v", "quiet", "-print_format", "json", "-show_streams")
	output, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("failed to run ffprobe: %v", err)
	}

	var ffprobeData entity.FFprobeData
	err = json.Unmarshal(output, &ffprobeData)
	if err != nil {
		return fmt.Errorf("failed to parse ffprobe output: %v", err)
	}

	for _, stream := range ffprobeData.Streams {
		if stream.CodecType != "subtitle" {
			continue
		}
		if !common.IsEng(stream.Tags.Language, stream.Tags.Title) && !common.IsCHT(stream.Tags.Language, stream.Tags.Title) && !common.IsCHS(stream.Tags.Language, stream.Tags.Title) {
			continue
		}

		subtitlePath, err := common.GetTmpSubtitleFullPath(common.ExtractFilenameWithoutExtension(videoPath) + "." + stream.Tags.Language)
		if err != nil {
			fmt.Printf("Failed to get subtitle path: %v\n", err)
			continue
		}
		fmt.Println(subtitlePath)
		err = ExtractSubtitleStream(videoPath, subtitlePath, stream.Index)
		if err != nil {
			fmt.Printf("Failed to extract subtitle stream %d: %v\n", stream.Index, err)
		} else {
			fmt.Printf("Subtitle stream %d extracted successfully: %s\n", stream.Index, subtitlePath)
		}
		fmt.Println(stream.Index, stream.Tags.Language, stream.Tags.Title)

	}

	return nil
}

func ExtractSubtitleStream(videoPath, subtitlePath string, streamIndex int) error {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		return fmt.Errorf("ffmpeg not found: %v", err)
	}

	cmd := exec.Command(ffmpegPath, "-i", videoPath, "-v", "quiet", "-map", fmt.Sprintf("0:%d", streamIndex), subtitlePath)
	cmd.Stderr = os.Stderr

	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("failed to extract subtitle stream %d: %v", streamIndex, err)
	}

	return nil
}

func Re() {
	curWorkDir, _ := os.Getwd()
	originalSubPath := fmt.Sprintf("%s/merged.srt", curWorkDir)
	modifiedSubPath := fmt.Sprintf("%s/red.srt", curWorkDir)

	// Read the original subtitle file
	originalSubContent, err := os.ReadFile(originalSubPath)
	if err != nil {
		fmt.Println("Error reading original subtitle file:", err)
		return
	}

	// Modify the subtitle content (change color and size)
	modifiedSubContent := modifySubtitles(originalSubContent)

	// Write the modified subtitle content to a new file
	err = os.WriteFile(modifiedSubPath, modifiedSubContent, 0644)
	if err != nil {
		fmt.Println("Error writing modified subtitle file:", err)
		return
	}

	fmt.Println("Modified subtitle file created successfully.")
}

// Modify the subtitles (change color and size)
func modifySubtitles(subContent []byte) []byte {
	lines := strings.Split(string(subContent), "\n")
	var modifiedSubContent strings.Builder

	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])

		// Check if the line is an index number
		if isIndexLine(line) {
			// Preserve index lines without modifications
			modifiedSubContent.WriteString(line)
		} else if len(line) > 0 && !isTimecode(line) {
			// Modify the subtitle text (change color and size)
			modifiedLine := modifySubtitleLine(line)
			modifiedSubContent.WriteString(modifiedLine)
		} else {
			// Keep the timecode line as is
			modifiedSubContent.WriteString(line)
		}

		modifiedSubContent.WriteString("\n")
	}

	return []byte(modifiedSubContent.String())
}

// Check if a line is an index number
func isIndexLine(line string) bool {
	indexRegex := regexp.MustCompile(`^\d+$`)
	return indexRegex.MatchString(line)
}

// Check if a line is a timecode (HH:MM:SS,sss --> HH:MM:SS,sss)
func isTimecode(line string) bool {
	_, err := fmt.Sscanf(line, "%d:%d:%d,%d --> %d:%d:%d,%d", new(int), new(int), new(int), new(int), new(int), new(int), new(int), new(int))
	return err == nil
}

// Modify a subtitle line (change color and size)
func modifySubtitleLine(line string) string {
	if strings.HasPrefix(line, "<font ") {
		return line // Skip lines that are already modified
	}
	return fmt.Sprintf("<font size:\"12px\">%s</font>", line)
}
