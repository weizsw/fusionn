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

	filePath := req.ExportedPath
	if config.C.Subset.Enabled {
		filePath = utils.ExtractPathWithoutExtension(filePath)
		filePath = fmt.Sprintf("%s.assfonts.ass", filePath)
	}

	mergedSub, err := a.parser.Parse(filePath)
	if err != nil {
		return nil, err
	}

	logger.L.Info("[AfterStage] reducing margin")
	req.MergeSubtitle = a.styleService.ReduceMargin(mergedSub, "{\\pos(192,278)}", "{\\pos(192,268)}")

	logger.L.Info("[AfterStage] writing subtitles", zap.String("dst_path", filePath))
	err = req.MergeSubtitle.Write(filePath)
	if err != nil {
		return nil, err
	}

	return req, nil
}
