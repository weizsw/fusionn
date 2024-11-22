package processor

import (
	"context"
	"fusionn/internal/model"
	"fusionn/utils"
)

type ExportStage struct {
}

func NewExportStage() *ExportStage {
	return &ExportStage{}
}

func (s *ExportStage) Process(ctx context.Context, input any) (any, error) {
	req, ok := input.(*model.ParsedSubtitles)
	if !ok {
		return nil, ErrInvalidInput
	}

	dstpath := utils.ExtractPathWithoutExtension(req.FilePath) + ".chi.ass"
	err := req.MergeSubtitle.Write(dstpath)
	if err != nil {
		return nil, err
	}

	return req, nil
}
