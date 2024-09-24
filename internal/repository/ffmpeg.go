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
	ExtractSubtitles(videoPath string) (*entity.ExtractedData, error)
}

type ffmpeg struct{}

func NewFFMPEG() *ffmpeg {
	return &ffmpeg{}
}

func (f *ffmpeg) ExtractSubtitles(videoPath string) (*entity.ExtractedData, error) {
	ffprobeData, err := f.detectStream(videoPath)
	if err != nil {
		return nil, err
	}
	lanIndexMap := make(map[string]int)
	filename := common.ExtractFilenameWithoutExtension(videoPath)
	extractData := &entity.ExtractedData{FileName: filename}
	for _, stream := range ffprobeData.Streams {
		if !f.isRelevantSubtitleStream(stream) {
			continue
		}

		switch {
		case common.IsChs(stream.Tags.Language, stream.Tags.Title):
			if _, ok := lanIndexMap[consts.CHS_LAN]; !ok {
				lanIndexMap[consts.CHS_LAN] = stream.Index
			}
		case common.IsTraditionalChinese(stream.Tags.Language, stream.Tags.Title):
			if _, ok := lanIndexMap[consts.CHT_LAN]; !ok {
				lanIndexMap[consts.CHT_LAN] = stream.Index
			}
		case common.IsCht(stream.Tags.Language, stream.Tags.Title):
			if _, ok := lanIndexMap[consts.CHT_LAN]; !ok {
				lanIndexMap[consts.CHT_LAN] = stream.Index
			}
		case common.IsSdh(stream.Tags.Title):
			if _, ok := lanIndexMap[consts.SDH_LAN]; !ok {
				lanIndexMap[consts.SDH_LAN] = stream.Index
			}
		case common.IsEng(stream.Tags.Language, stream.Tags.Title):
			if _, ok := lanIndexMap[consts.ENG_LAN]; !ok {
				lanIndexMap[consts.ENG_LAN] = stream.Index
			}
		}
	}

	for lan, index := range lanIndexMap {
		subtitlePath, err := common.GetTmpSubtitleFullPath(filename + "." + lan)
		if err != nil {
			return nil, fmt.Errorf("failed to get subtitle path: %w", err)
		}

		err = f.extractStream(videoPath, subtitlePath, index)
		if err != nil {
			return nil, fmt.Errorf("failed to extract subtitle stream: %w", err)
		}

		switch lan {
		case consts.CHS_LAN:
			extractData.ChsSubPath = subtitlePath
		case consts.CHT_LAN:
			extractData.ChtSubPath = subtitlePath
		case consts.ENG_LAN:
			extractData.EngSubPath = subtitlePath
		case consts.SDH_LAN:
			extractData.SdhSubPath = subtitlePath
		}

		log.Infof("Subtitle stream %d extracted successfully: %s\n", index, subtitlePath)
	}

	return extractData, nil
}

func (f *ffmpeg) detectStream(videoPath string) (*entity.FFprobeData, error) {
	ffprobePath, err := exec.LookPath("ffprobe")
	if err != nil {
		return nil, fmt.Errorf("ffprobe not found: %w", err)
	}

	cmd := exec.Command(ffprobePath, "-i", videoPath, "-v", "quiet", "-print_format", "json", "-show_streams")
	cmd.Stderr = os.Stderr

	log.Debug(cmd.String())
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
			common.IsChs(stream.Tags.Language, stream.Tags.Title) ||
			common.IsSdh(stream.Tags.Title))
}

func (f *ffmpeg) extractStream(videoPath, subtitlePath string, streamIndex int) error {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		return fmt.Errorf("ffmpeg not found: %w", err)
	}

	cmd := exec.Command(ffmpegPath, "-y", "-i", videoPath, "-v", "quiet", "-map", fmt.Sprintf("0:%d", streamIndex), subtitlePath)
	cmd.Stderr = os.Stderr
	log.Debug(cmd.String())
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("failed to extract subtitle stream %d: %w", streamIndex, err)
	}

	return nil
}
