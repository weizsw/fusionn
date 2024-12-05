package processor

import (
	"context"
	"fusionn/internal/model"
	"fusionn/internal/service"
	"fusionn/logger"

	"go.uber.org/zap"
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
	stream, ok := input.(*model.ExtractedStream)
	if !ok {
		return nil, ErrInvalidInput
	}

	logger.L.Info("[ParseStage] parsing subtitles", zap.String("file_path", stream.FilePath))
	return p.parser.ParseFromBytes(ctx, stream)
}
