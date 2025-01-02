package processor

import (
	"context"
	"fmt"
	"fusionn/config"
	"fusionn/errs"
	"fusionn/internal/model"
	"fusionn/internal/service"
	"fusionn/logger"
	"fusionn/utils"

	"go.uber.org/zap"
)

type AfterStage struct {
	styleService service.StyleService
	parser       service.Parser
}

func NewAfterStage(styleService service.StyleService, parser service.Parser) *AfterStage {
	return &AfterStage{styleService: styleService, parser: parser}
}

func (a *AfterStage) Process(ctx context.Context, input any) (any, error) {
	req, ok := input.(*model.ParsedSubtitles)
	if !ok {
		return nil, errs.ErrInvalidInput
	}

	if !config.C.After.ReduceMargin {
		return req, nil
	}

	filePath := req.ExportedPath
	if config.C.Subset.Enabled {
		filePath = utils.ExtractPathWithoutExtension(filePath)
		filePath = fmt.Sprintf("%s.assfonts.ass", filePath)
	}

	mergedSub, err := a.parser.ParseFile(filePath)
	if err != nil {
		return nil, err
	}

	logger.L.Info("[AfterStage] reducing margin")
	modifiedSub := a.styleService.ReduceMarginV2(mergedSub, config.C.After.DefaultMargin, config.C.After.EngMargin)

	logger.L.Info("[AfterStage] writing subtitles", zap.String("dst_path", filePath))
	err = a.parser.ExportFile(modifiedSub, filePath)
	if err != nil {
		return nil, err
	}

	return req, nil
}
