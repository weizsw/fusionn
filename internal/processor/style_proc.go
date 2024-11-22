package processor

import (
	"context"
	"fusionn/internal/model"
	"fusionn/utils"
)

type StyleStage struct{}

func NewStyleStage() *StyleStage {
	return &StyleStage{}
}

func (s *StyleStage) Process(ctx context.Context, input any) (any, error) {
	req, ok := input.(*model.ParsedSubtitles)
	if !ok {
		return nil, ErrInvalidInput
	}

	req.MergeSubtitle = utils.AddingStyleToAss(req.MergeSubtitle)

	return req, nil
}
