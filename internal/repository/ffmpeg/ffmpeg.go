package ffmpeg

import (
	"encoding/json"
	"fmt"
	"fusionn/internal/entity"
	"fusionn/internal/repository/common"
	"os"
	"os/exec"
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

func ConvertSubtitleToAss(subtitlePath, outputPath string) error {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		return fmt.Errorf("ffmpeg not found: %v", err)
	}
	cmd := exec.Command(ffmpegPath, "-i", subtitlePath, outputPath)
	cmd.Stderr = os.Stderr

	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("failed to convert ass: %v", err)
	}

	return nil
}
