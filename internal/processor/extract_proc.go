package processor

import (
	"context"
	"fusionn/internal/model"
	"fusionn/internal/service"
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
		return nil, ErrInvalidInput
	}

	return s.ffmpeg.ExtractStreamToBuffer(req.SonarrEpisodefilePath)
}
