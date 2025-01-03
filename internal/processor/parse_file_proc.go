package processor

import (
	"context"
	"fusionn/errs"
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
		return nil, errs.ErrInvalidInput
	}

	filePath := req.VideoPath
	filename := utils.ExtractFilenameWithoutExtension(filePath)

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
		Translated:  true,
	}, nil
}
