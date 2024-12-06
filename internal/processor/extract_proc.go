package processor

import (
	"context"
	"fusionn/errs"
	"fusionn/internal/model"
	"fusionn/internal/service"
	"fusionn/logger"

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

	logger.L.Info("[ExtractStage] starting subtitle extraction", zap.String("file_path", req.SonarrEpisodefilePath))

	return s.ffmpeg.ExtractStreamToBuffer(req.SonarrEpisodefilePath)
}
