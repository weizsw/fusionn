package handler

import (
	"fusionn/internal/model"
	"fusionn/internal/processor"
	"fusionn/internal/service"
	"fusionn/pkg"

	"github.com/gin-gonic/gin"
)

func ProvideMergePipeline(
	extractStage *processor.ExtractStage,
	parseStage *processor.ParseStage,
	cleanStage *processor.CleanStage,
	segMergeStage *processor.SegMergeStage,
	styleStage *processor.StyleStage,
	exportStage *processor.ExportStage,
	subsetStage *processor.SubsetStage,
	notiStage *processor.NotiStage,
	afterStage *processor.AfterStage,
) *MergePipeline {
	stages := []processor.Stage{
		extractStage,
		parseStage,
		segMergeStage,
		styleStage,
		exportStage,
		subsetStage,
		notiStage,
		afterStage,
	}
	return &MergePipeline{
		Pipeline: processor.NewPipeline(stages...),
	}
}

type MergeHandler struct {
	ffmpeg    service.FFMPEG
	parser    service.Parser
	convertor service.Convertor
	algo      service.Algo
	apprise   pkg.Apprise
	pipeline  *MergePipeline
}

func NewMergeHandler(ffmpeg service.FFMPEG, parser service.Parser, convertor service.Convertor, algo service.Algo, apprise pkg.Apprise, pipeline *MergePipeline) *MergeHandler {
	return &MergeHandler{
		ffmpeg:    ffmpeg,
		parser:    parser,
		convertor: convertor,
		algo:      algo,
		apprise:   apprise,
		pipeline:  pipeline,
	}
}

func (h *MergeHandler) Merge(c *gin.Context) error {
	req := &model.ExtractRequest{}
	if err := c.ShouldBindJSON(req); err != nil {
		return err
	}

	ctx := c.Request.Context()

	_, err := h.pipeline.Execute(ctx, req)
	if err != nil {
		return err
	}

	return nil
}
