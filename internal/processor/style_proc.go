package processor

import (
	"context"
	"fusionn/internal/model"
	"fusionn/internal/service"
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
		return nil, ErrInvalidInput
	}

	req.MergeSubtitle = s.styleService.AddStyle(req.MergeSubtitle)
	req.MergeSubtitle = s.styleService.ReduceMargin(req.MergeSubtitle, "{\\org(-2000000,0)\\fr-0.00005}", "{\\org(-2000000,0)\\fr0.00015}")
	req.MergeSubtitle = s.styleService.ReplaceSpecialCharacters(req.MergeSubtitle)
	return req, nil
}
