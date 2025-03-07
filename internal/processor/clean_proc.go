package processor

import (
	"context"
	"fusionn/errs"
	"fusionn/internal/model"
	"fusionn/internal/service"
	"fusionn/logger"
)

type CleanStage struct {
	parser service.Parser
}

func NewCleanStage(p service.Parser) *CleanStage {
	return &CleanStage{parser: p}
}

func (c *CleanStage) Process(ctx context.Context, input any) (any, error) {
	req, ok := input.(*model.ParsedSubtitles)
	if !ok {
		return nil, errs.ErrInvalidInput
	}

	logger.L.Info("[CleanStage] cleaning subtitles")

	req.ChsSubtitle = c.parser.Clean(req.ChsSubtitle)
	req.EngSubtitle = c.parser.Clean(req.EngSubtitle)

	return req, nil
}
