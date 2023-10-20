package ffmpeg

import (
	"encoding/json"
	"fmt"
	"fusionn/internal/consts"
	"fusionn/internal/entity"
	"fusionn/internal/repository/common"
	"log"
	"os"
	"os/exec"
)

func ExtractSubtitles(videoPath string) (*entity.ExtractData, error) {
	ffprobePath, err := exec.LookPath("ffprobe")
	if err != nil {
		return nil, fmt.Errorf("ffprobe not found: %v", err)
	}

	cmd := exec.Command(ffprobePath, "-i", videoPath, "-v", "quiet", "-print_format", "json", "-show_streams")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run ffprobe: %v", err)
	}

	var ffprobeData entity.FFprobeData
	err = json.Unmarshal(output, &ffprobeData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse ffprobe output: %v", err)
	}

	filename := common.ExtractFilenameWithoutExtension(videoPath)
	extractData := &entity.ExtractData{
		FileName: filename,
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
			log.Printf("Failed to get subtitle path: %v\n", err)
			continue
		}

		if common.IsEng(stream.Tags.Language, stream.Tags.Title) && len(extractData.EngSubPath) == 0 {
			extractData.EngSubPath = subtitlePath
		}
		if common.IsCHS(stream.Tags.Language, stream.Tags.Title) {
			subtitlePath, _ = common.GetTmpSubtitleFullPath(common.ExtractFilenameWithoutExtension(videoPath) + "." + consts.CHS_LAN)
			extractData.CHSSubPath = subtitlePath
		}
		if common.IsCHT(stream.Tags.Language, stream.Tags.Title) {
			extractData.CHTSubPath = subtitlePath
		}
		err = ExtractSubtitleStream(videoPath, subtitlePath, stream.Index)
		if err != nil {
			log.Printf("Failed to extract subtitle stream %d: %v\n", stream.Index, err)
		} else {
			log.Printf("Subtitle stream %d extracted successfully: %s\n", stream.Index, subtitlePath)
		}
	}

	return extractData, nil
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
	cmd := exec.Command(ffmpegPath, "-i", subtitlePath, outputPath, "-v", "quiet")
	cmd.Stderr = os.Stderr

	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("failed to convert ass: %v", err)
	}

	return nil
}
