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
	ffprobeData, err := f.runFFprobe(videoPath)
	if err != nil {
		return nil, err
	}

	filename := common.ExtractFilenameWithoutExtension(videoPath)
	extractData := &entity.ExtractData{FileName: filename}

	for _, stream := range ffprobeData.Streams {
		if !f.isRelevantSubtitleStream(stream) {
			continue
		}

		subtitlePath, err := f.processSubtitleStream(videoPath, filename, stream, extractData)
		if err != nil {
			log.Error(err)
			continue
		}

		log.Infof("Subtitle stream %d extracted successfully: %s\n", stream.Index, subtitlePath)
	}

	f.handleSDHSubtitle(extractData)

	return extractData, nil
}

func (f *ffmpeg) runFFprobe(videoPath string) (*entity.FFprobeData, error) {
	ffprobePath, err := exec.LookPath("ffprobe")
	if err != nil {
		return nil, fmt.Errorf("ffprobe not found: %w", err)
	}

	cmd := exec.Command(ffprobePath, "-i", videoPath, "-v", "quiet", "-print_format", "json", "-show_streams")
	cmd.Stderr = os.Stderr

	log.Info(cmd.String())
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run ffprobe: %w", err)
	}

	var ffprobeData entity.FFprobeData
	err = json.Unmarshal(output, &ffprobeData)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal ffprobe data: %w", err)
	}

	return &ffprobeData, nil
}

func (f *ffmpeg) isRelevantSubtitleStream(stream entity.Stream) bool {
	return stream.CodecType == "subtitle" &&
		(common.IsEng(stream.Tags.Language, stream.Tags.Title) ||
			common.IsCht(stream.Tags.Language, stream.Tags.Title) ||
			common.IsChs(stream.Tags.Language, stream.Tags.Title))
}

func (f *ffmpeg) processSubtitleStream(videoPath, filename string, stream entity.Stream, extractData *entity.ExtractData) (string, error) {
	subtitlePath, err := common.GetTmpSubtitleFullPath(filename + "." + stream.Tags.Language)
	if err != nil {
		return "", fmt.Errorf("failed to get subtitle path: %w", err)
	}

	f.updateExtractData(extractData, stream, subtitlePath)

	err = ExtractSubtitleStream(videoPath, subtitlePath, stream.Index)
	if err != nil {
		return "", fmt.Errorf("failed to extract subtitle stream: %w", err)
	}

	return subtitlePath, nil
}

func (f *ffmpeg) updateExtractData(extractData *entity.ExtractData, stream entity.Stream, subtitlePath string) {
	switch {
	case common.IsEng(stream.Tags.Language, stream.Tags.Title) && extractData.EngSubPath == "":
		f.handleEngSubtitle(extractData, stream, subtitlePath)
	case common.IsChs(stream.Tags.Language, stream.Tags.Title) && extractData.ChsSubPath == "":
		f.handleChsSubtitle(extractData, stream, subtitlePath)
	case common.IsCht(stream.Tags.Language, stream.Tags.Title) && extractData.ChtSubPath == "":
		f.handleChtSubtitle(extractData, stream, subtitlePath)
	}
}

func (f *ffmpeg) handleEngSubtitle(extractData *entity.ExtractData, stream entity.Stream, subtitlePath string) {
	if common.IsSdh(stream.Tags.Title) {
		sdhPath, _ := common.GetTmpSubtitleFullPath(extractData.FileName + "." + consts.SDH_LAN)
		extractData.SdhSubPath = sdhPath
	} else {
		extractData.EngSubPath = subtitlePath
	}
	log.Infof("Eng subtitle: language:(%s) title:(%s) path:(%s)", stream.Tags.Language, stream.Tags.Title, subtitlePath)
}

func (f *ffmpeg) handleChsSubtitle(extractData *entity.ExtractData, stream entity.Stream, subtitlePath string) {
	chsPath, _ := common.GetTmpSubtitleFullPath(extractData.FileName + "." + consts.CHS_LAN)
	extractData.ChsSubPath = chsPath
	log.Infof("Chs subtitle: language:(%s) title:(%s) path:(%s)", stream.Tags.Language, stream.Tags.Title, subtitlePath)
}

func (f *ffmpeg) handleChtSubtitle(extractData *entity.ExtractData, stream entity.Stream, subtitlePath string) {
	extractData.ChtSubPath = subtitlePath
	log.Infof("Cht subtitle: language(%s) title:(%s) path:(%s)", stream.Tags.Language, stream.Tags.Title, subtitlePath)
}

func (f *ffmpeg) handleSDHSubtitle(extractData *entity.ExtractData) {
	if extractData.EngSubPath == "" && extractData.SdhSubPath != "" {
		extractData.EngSubPath = extractData.SdhSubPath
	}
}

func ExtractSubtitleStream(videoPath, subtitlePath string, streamIndex int) error {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		return fmt.Errorf("ffmpeg not found: %w", err)
	}

	cmd := exec.Command(ffmpegPath, "-y", "-i", videoPath, "-v", "quiet", "-map", fmt.Sprintf("0:%d", streamIndex), subtitlePath)
	cmd.Stderr = os.Stderr
	log.Info(cmd.String())
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("failed to extract subtitle stream %d: %w", streamIndex, err)
	}

	return nil
}

func ConvertSubtitleToAss(subtitlePath, outputPath string) error {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		return fmt.Errorf("ffmpeg not found: %w", err)
	}
	cmd := exec.Command(ffmpegPath, "-i", subtitlePath, outputPath, "-v", "quiet")
	cmd.Stderr = os.Stderr

	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("failed to convert ass: %w", err)
	}

	return nil
}
