package processor

import (
	"context"
	"fusionn/internal/model"
	"fusionn/internal/service"
	"fusionn/logger"
	"fusionn/utils"

	"go.uber.org/zap"
)

type ParseFileStage struct {
	parser service.Parser
}

func NewParseFileStage(parser service.Parser) *ParseFileStage {
	return &ParseFileStage{
		parser: parser,
	}
}

func (p *ParseFileStage) Process(ctx context.Context, input any) (any, error) {
	req, ok := input.(*model.AsyncMergeRequest)
	if !ok {
		return nil, ErrInvalidInput
	}

	filename := utils.ExtractFilenameWithoutExtension(req.EngSubtilePath)
	filename = utils.RemoveLanguageExtensions(filename)
	filePath := utils.ExtractPathWithoutExtension(req.EngSubtilePath)
	filePath = utils.RemoveLanguageExtensions(filePath)

	logger.L.Info("[ParseFileStage] parsing subtitles", zap.String("chs_subtitle_path", req.ChsSubtilePath), zap.String("eng_subtitle_path", req.EngSubtilePath), zap.String("filename", filename), zap.String("file_path", filePath))
	chsSub, err := p.parser.Parse(req.ChsSubtilePath)
	if err != nil {
		return nil, err
	}

	engSub, err := p.parser.Parse(req.EngSubtilePath)
	if err != nil {
		return nil, err
	}

	return &model.ParsedSubtitles{
		FilePath:    filePath,
		FileName:    filename,
		ChsSubtitle: chsSub,
		EngSubtitle: engSub,
	}, nil
}
