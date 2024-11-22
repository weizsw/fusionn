package processor

import (
	"context"
	"fusionn/internal/model"
	"fusionn/internal/service"
)

type ParseStage struct {
	parser service.Parser
}

func NewParseStage(parser service.Parser) *ParseStage {
	return &ParseStage{
		parser: parser,
	}
}

func (p *ParseStage) Process(ctx context.Context, input any) (any, error) {
	stream := input.(*model.ExtractedStream)
	return p.parser.ParseFromBytes(stream)
}
