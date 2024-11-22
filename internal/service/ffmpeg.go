package service

import (
	"encoding/json"
	"fmt"
	"fusionn/internal/consts"
	"fusionn/internal/model"
	"fusionn/logger"
	"fusionn/utils"

	"os"
	"os/exec"
)

type FFMPEG interface {
	ExtractSubtitles(videoPath string) (*model.ExtractedData, error)
	ExtractStreamToBuffer(videoPath string) (*model.ExtractedStream, error)
}

type ffmpeg struct{}

func NewFFMPEG() *ffmpeg {
	return &ffmpeg{}
}

func (f *ffmpeg) ExtractSubtitles(videoPath string) (*model.ExtractedData, error) {
	ffprobeData, err := f.detectStream(videoPath)
	if err != nil {
		return nil, err
	}
	lanIndexMap := make(map[string]int)
	filename := utils.ExtractFilenameWithoutExtension(videoPath)
	extractData := &model.ExtractedData{FileName: filename}
	for _, stream := range ffprobeData.Streams {
		if !f.isRelevantSubtitleStream(stream) {
			continue
		}

		switch {
		case utils.IsChs(stream.Tags.Language, stream.Tags.Title):
			if _, ok := lanIndexMap[consts.CHS_LAN]; !ok {
				lanIndexMap[consts.CHS_LAN] = stream.Index
			}
		case utils.IsTraditionalChinese(stream.Tags.Language, stream.Tags.Title):
			if _, ok := lanIndexMap[consts.CHT_LAN]; !ok {
				lanIndexMap[consts.CHT_LAN] = stream.Index
			}
		case utils.IsCht(stream.Tags.Language, stream.Tags.Title):
			if _, ok := lanIndexMap[consts.CHT_LAN]; !ok {
				lanIndexMap[consts.CHT_LAN] = stream.Index
			}
		case utils.IsSdh(stream.Tags.Title):
			if _, ok := lanIndexMap[consts.SDH_LAN]; !ok {
				lanIndexMap[consts.SDH_LAN] = stream.Index
			}
		case utils.IsEng(stream.Tags.Language, stream.Tags.Title):
			if _, ok := lanIndexMap[consts.ENG_LAN]; !ok {
				lanIndexMap[consts.ENG_LAN] = stream.Index
			}
		}
	}

	for lan, index := range lanIndexMap {
		subtitlePath, err := utils.GetTmpSubtitleFullPath(filename + "." + lan)
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

		logger.Sugar.Infof("Subtitle stream %d extracted successfully: %s\n", index, subtitlePath)
	}

	return extractData, nil
}

func (f *ffmpeg) ExtractStreamToBuffer(videoPath string) (*model.ExtractedStream, error) {
	ffprobeData, err := f.detectStream(videoPath)
	if err != nil {
		return nil, err
	}
	lanIndexMap := make(map[string]int)
	filename := utils.ExtractFilenameWithoutExtension(videoPath)
	extractData := &model.ExtractedStream{FileName: filename, FilePath: videoPath}
	for _, stream := range ffprobeData.Streams {
		if !f.isRelevantSubtitleStream(stream) {
			continue
		}

		switch {
		case utils.IsChs(stream.Tags.Language, stream.Tags.Title):
			if _, ok := lanIndexMap[consts.CHS_LAN]; !ok {
				lanIndexMap[consts.CHS_LAN] = stream.Index
			}
		case utils.IsTraditionalChinese(stream.Tags.Language, stream.Tags.Title):
			if _, ok := lanIndexMap[consts.CHT_LAN]; !ok {
				lanIndexMap[consts.CHT_LAN] = stream.Index
			}
		case utils.IsCht(stream.Tags.Language, stream.Tags.Title):
			if _, ok := lanIndexMap[consts.CHT_LAN]; !ok {
				lanIndexMap[consts.CHT_LAN] = stream.Index
			}
		case utils.IsSdh(stream.Tags.Title):
			if _, ok := lanIndexMap[consts.SDH_LAN]; !ok {
				lanIndexMap[consts.SDH_LAN] = stream.Index
			}
		case utils.IsEng(stream.Tags.Language, stream.Tags.Title):
			if _, ok := lanIndexMap[consts.ENG_LAN]; !ok {
				lanIndexMap[consts.ENG_LAN] = stream.Index
			}
		}
	}

	for lan, index := range lanIndexMap {
		subtitlePath, err := utils.GetTmpSubtitleFullPath(filename + "." + lan)
		if err != nil {
			return nil, fmt.Errorf("failed to get subtitle path: %w", err)
		}

		buffer, err := f.extractStreamToBuffer(videoPath, index)
		if err != nil {
			return nil, fmt.Errorf("failed to extract subtitle stream: %w", err)
		}

		switch lan {
		case consts.CHS_LAN:
			extractData.ChsSubBuffer = buffer
		case consts.CHT_LAN:
			extractData.ChtSubBuffer = buffer
		case consts.ENG_LAN:
			extractData.EngSubBuffer = buffer
		case consts.SDH_LAN:
			extractData.SdhSubBuffer = buffer
		}

		logger.Sugar.Infof("Subtitle stream %d extracted successfully: %s\n", index, subtitlePath)
	}

	return extractData, nil
}

func (f *ffmpeg) detectStream(videoPath string) (*model.FFprobeData, error) {
	ffprobePath, err := exec.LookPath("ffprobe")
	if err != nil {
		return nil, fmt.Errorf("ffprobe not found: %w", err)
	}

	cmd := exec.Command(ffprobePath, "-i", videoPath, "-v", "quiet", "-print_format", "json", "-show_streams")
	cmd.Stderr = os.Stderr

	logger.Sugar.Debug(cmd.String())
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to run ffprobe: %w", err)
	}

	var ffprobeData model.FFprobeData
	err = json.Unmarshal(output, &ffprobeData)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal ffprobe data: %w", err)
	}

	return &ffprobeData, nil
}

func (f *ffmpeg) isRelevantSubtitleStream(stream model.Stream) bool {
	return stream.CodecType == "subtitle" &&
		(utils.IsEng(stream.Tags.Language, stream.Tags.Title) ||
			utils.IsCht(stream.Tags.Language, stream.Tags.Title) ||
			utils.IsChs(stream.Tags.Language, stream.Tags.Title) ||
			utils.IsSdh(stream.Tags.Title))
}

func (f *ffmpeg) extractStream(videoPath, subtitlePath string, streamIndex int) error {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		return fmt.Errorf("ffmpeg not found: %w", err)
	}

	cmd := exec.Command(ffmpegPath, "-y", "-i", videoPath, "-v", "quiet", "-map", fmt.Sprintf("0:%d", streamIndex), subtitlePath)
	cmd.Stderr = os.Stderr
	logger.Sugar.Debug(cmd.String())
	err = cmd.Run()
	if err != nil {
		return fmt.Errorf("failed to extract subtitle stream %d: %w", streamIndex, err)
	}

	return nil
}

func (f *ffmpeg) extractStreamToBuffer(videoPath string, streamIndex int) ([]byte, error) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		return nil, fmt.Errorf("ffmpeg not found: %w", err)
	}

	cmd := exec.Command(ffmpegPath, "-y", "-i", videoPath, "-v", "quiet", "-map", fmt.Sprintf("0:%d", streamIndex), "-f", "srt", "-")
	cmd.Stderr = os.Stderr
	logger.Sugar.Debug(cmd.String())

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to extract subtitle stream %d: %w", streamIndex, err)
	}

	return output, nil
}
