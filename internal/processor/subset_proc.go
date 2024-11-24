package processor

import (
	"context"
	"fusionn/internal/model"
	"fusionn/internal/service"
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
		return nil, ErrInvalidInput
	}

	err := s.styleService.FontSubSet(req.ExportedPath)
	if err != nil {
		return nil, err
	}

	return req, nil
}
