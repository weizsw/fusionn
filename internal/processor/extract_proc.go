package processor

import (
	"context"
	"fusionn/errs"
	"fusionn/internal/model"
	"fusionn/internal/service"
	"fusionn/logger"

	"github.com/spf13/cast"
	"go.uber.org/zap"
)

type ExtractStage struct {
	ffmpeg service.FFMPEG
}

func NewExtractStage(ffmpeg service.FFMPEG) *ExtractStage {
	return &ExtractStage{ffmpeg: ffmpeg}
}

func (s *ExtractStage) Process(ctx context.Context, input any) (any, error) {
	req, ok := input.(*model.ExtractRequest)
	if !ok {
		return nil, errs.ErrInvalidInput
	}

	logger.L.Info("[ExtractStage] starting subtitle extraction", zap.String("file_path", req.FilePath), zap.String("tvdb_series_id", req.SeriesTVDBID), zap.String("season", req.SeasonNumber), zap.String("episode", req.EpisodeNumbers))

	res, err := s.ffmpeg.ExtractStreamToBuffer(req.FilePath)
	if err != nil {
		return nil, err
	}
	res.TVDBSeriesID = cast.ToInt(req.SeriesTVDBID)
	res.Season = cast.ToInt(req.SeasonNumber)
	res.Episode = cast.ToInt(req.EpisodeNumbers)

	return res, nil
}
