package repository

import (
	"encoding/json"
	"fmt"
	"fusionn/internal/consts"
	"fusionn/internal/entity"
	"fusionn/internal/repository/common"
	"os"
	"os/exec"

	"github.com/gofiber/fiber/v2/log"
)

type IFFMPEG interface {
	ExtractSubtitles(videoPath string) (*entity.ExtractData, error)
}

type ffmpeg struct{}

func NewFFMPEG() *ffmpeg {
	return &ffmpeg{}
}

func (f *ffmpeg) ExtractSubtitles(videoPath string) (*entity.ExtractData, error) {
	ffprobePath, err := exec.LookPath("ffprobe")
	if err != nil {
		return nil, fmt.Errorf("ffprobe not found: %w", err)
	}

	cmd := exec.Command(ffprobePath, "-i", videoPath, "-v", "quiet", "-print_format", "json", "-show_streams")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run ffprobe: %w", err)
	}

	var ffprobeData entity.FFprobeData
	err = json.Unmarshal(output, &ffprobeData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse ffprobe output: %w", err)
	}

	filename := common.ExtractFilenameWithoutExtension(videoPath)
	extractData := &entity.ExtractData{
		FileName: filename,
	}

	for _, stream := range ffprobeData.Streams {
		if stream.CodecType != "subtitle" {
			continue
		}
		if !common.IsEng(stream.Tags.Language, stream.Tags.Title) && !common.IsCht(stream.Tags.Language, stream.Tags.Title) && !common.IsChs(stream.Tags.Language, stream.Tags.Title) {
			continue
		}

		subtitlePath, err := common.GetTmpSubtitleFullPath(filename + "." + stream.Tags.Language)
		if err != nil {
			log.Error("Failed to get subtitle path: %w", err)
			continue
		}

		if common.IsEng(stream.Tags.Language, stream.Tags.Title) && common.IsSdh(stream.Tags.Title) && len(extractData.EngSubPath) != 0 {
			continue
		}

		if common.IsEng(stream.Tags.Language, stream.Tags.Title) && (len(extractData.EngSubPath) == 0) {
			extractData.EngSubPath = subtitlePath
			log.Infof("Eng subtitle %s %s %s", stream.Tags.Language, stream.Tags.Title, subtitlePath)
		}
		if common.IsChs(stream.Tags.Language, stream.Tags.Title) {
			subtitlePath, _ = common.GetTmpSubtitleFullPath(filename + "." + consts.CHS_LAN)
			extractData.ChsSubPath = subtitlePath
			log.Infof("Chs subtitle %s %s %s", stream.Tags.Language, stream.Tags.Title, subtitlePath)
		}
		if common.IsCht(stream.Tags.Language, stream.Tags.Title) && len(extractData.ChtSubPath) == 0 {
			extractData.ChtSubPath = subtitlePath
			log.Infof("Cht subtitle %s %s %s", stream.Tags.Language, stream.Tags.Title, subtitlePath)
		}

		err = ExtractSubtitleStream(videoPath, subtitlePath, stream.Index)
		if err != nil {
			log.Error(err)
		} else {
			log.Info(fmt.Sprintf("Subtitle stream %d extracted successfully: %s\n", stream.Index, subtitlePath))
		}
	}

	return extractData, nil
}

func ExtractSubtitles(videoPath string) (*entity.ExtractData, error) {
	ffprobePath, err := exec.LookPath("ffprobe")
	if err != nil {
		return nil, fmt.Errorf("ffprobe not found: %w", err)
	}

	cmd := exec.Command(ffprobePath, "-i", videoPath, "-v", "quiet", "-print_format", "json", "-show_streams")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run ffprobe: %w", err)
	}

	var ffprobeData entity.FFprobeData
	err = json.Unmarshal(output, &ffprobeData)
	if err != nil {
		return nil, fmt.Errorf("failed to parse ffprobe output: %w", err)
	}

	filename := common.ExtractFilenameWithoutExtension(videoPath)
	extractData := &entity.ExtractData{
		FileName: filename,
	}

	for _, stream := range ffprobeData.Streams {
		if stream.CodecType != "subtitle" {
			continue
		}
		if !common.IsEng(stream.Tags.Language, stream.Tags.Title) && !common.IsCht(stream.Tags.Language, stream.Tags.Title) && !common.IsChs(stream.Tags.Language, stream.Tags.Title) {
			continue
		}

		subtitlePath, err := common.GetTmpSubtitleFullPath(common.ExtractFilenameWithoutExtension(videoPath) + "." + stream.Tags.Language)
		if err != nil {
			log.Info("Failed to get subtitle path: %v\n", err)
			continue
		}

		if common.IsEng(stream.Tags.Language, stream.Tags.Title) && len(extractData.EngSubPath) == 0 {
			extractData.EngSubPath = subtitlePath
		}
		if common.IsChs(stream.Tags.Language, stream.Tags.Title) {
			subtitlePath, _ = common.GetTmpSubtitleFullPath(common.ExtractFilenameWithoutExtension(videoPath) + "." + consts.CHS_LAN)
			extractData.ChsSubPath = subtitlePath
		}
		if common.IsCht(stream.Tags.Language, stream.Tags.Title) {
			extractData.ChtSubPath = subtitlePath
		}
		err = ExtractSubtitleStream(videoPath, subtitlePath, stream.Index)
		if err != nil {
			log.Error(err)
		} else {
			log.Info(fmt.Sprintf("Subtitle stream %d extracted successfully: %s\n", stream.Index, subtitlePath))
		}
	}

	return extractData, nil
}

func ExtractSubtitleStream(videoPath, subtitlePath string, streamIndex int) error {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		return fmt.Errorf("ffmpeg not found: %v", err)
	}

	_, err = os.Stat(subtitlePath)
	if err != nil {
		if os.IsNotExist(err) {
			// The file does not exist, handle this case if necessary
			log.Warnf("File %s does not exist.\n", subtitlePath)
		} else {
			// Some other error occurred, handle it
			return fmt.Errorf("error checking file existence for %s: %w", subtitlePath, err)
		}
	} else {
		// The file exists
		err = os.Remove(subtitlePath)
		if err != nil {
			return fmt.Errorf("failed to remove file %s: %w", subtitlePath, err)
		}
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
