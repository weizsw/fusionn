package processor

import (
	"context"
	"fusionn/errs"
	"fusionn/internal/model"
	"fusionn/internal/service"
	"fusionn/logger"

	"go.uber.org/zap"
)

type SubsetStage struct {
	styleService service.StyleService
}

func NewSubsetStage(styleService service.StyleService) *SubsetStage {
	return &SubsetStage{styleService: styleService}
}

func (s *SubsetStage) Process(ctx context.Context, input any) (any, error) {
	req, ok := input.(*model.ParsedSubtitles)
	if !ok {
		return nil, errs.ErrInvalidInput
	}

	logger.L.Info("[SubsetStage] subsetting subtitles", zap.String("file_path", req.ExportedPath))
	err := s.styleService.FontSubSet(req.ExportedPath)
	if err != nil {
		return nil, err
	}

	return req, nil
}
