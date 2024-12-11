package processor

import (
	"context"
	"fusionn/errs"
	"fusionn/internal/model"
	"fusionn/internal/service"
	"fusionn/logger"
)

type StyleStage struct {
	styleService service.StyleService
}

func NewStyleStage(styleService service.StyleService) *StyleStage {
	return &StyleStage{styleService: styleService}
}

func (s *StyleStage) Process(ctx context.Context, input any) (any, error) {
	req, ok := input.(*model.ParsedSubtitles)
	if !ok {
		return nil, errs.ErrInvalidInput
	}

	logger.L.Info("[StyleStage] adding style to subtitles")
	req.MergeSubtitle = s.styleService.AddStyle(req.MergeSubtitle)
	req.MergeSubtitle = s.styleService.ReplaceSpecialCharacters(req.MergeSubtitle)
	if req.Translated {
		req.MergeSubtitle = s.styleService.RemovePunctuation(req.MergeSubtitle)
	}
	return req, nil
}
