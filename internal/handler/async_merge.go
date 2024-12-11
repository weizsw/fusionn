package handler

import (
	"fusionn/internal/model"
	"fusionn/internal/processor"

	"github.com/gin-gonic/gin"
)

type AsyncMergeHandler struct {
	pipeline *AsyncMergePipeline
}

func NewAsyncMergeHandler(pipeline *AsyncMergePipeline) *AsyncMergeHandler {
	return &AsyncMergeHandler{
		pipeline: pipeline,
	}
}

func ProvideAsyncMergePipeline(
	parseFileStage *processor.ParseFileStage,
	segMergeStage *processor.SegMergeStage,
	styleStage *processor.StyleStage,
	exportStage *processor.ExportStage,
	subsetStage *processor.SubsetStage,
	notiStage *processor.NotiStage,
	afterStage *processor.AfterStage,
) *AsyncMergePipeline {
	stages := []processor.Stage{
		parseFileStage,
		segMergeStage,
		styleStage,
		exportStage,
		subsetStage,
		notiStage,
		afterStage,
	}
	return &AsyncMergePipeline{
		Pipeline: processor.NewPipeline(stages...),
	}
}

func (h *AsyncMergeHandler) AsyncMerge(c *gin.Context) error {
	req := &model.AsyncMergeRequest{}
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
