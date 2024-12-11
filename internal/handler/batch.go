package handler

import (
	"fmt"
	"fusionn/internal/model"
	"fusionn/internal/processor"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"golang.org/x/sync/errgroup"
)

type BatchHandler struct {
	pipeline *BatchPipeline
}

func NewBatchHandler(pipeline *BatchPipeline) *BatchHandler {
	return &BatchHandler{
		pipeline: pipeline,
	}
}

func ProvideBatchPipeline(
	extractStage *processor.ExtractStage,
	parseStage *processor.ParseStage,
	cleanStage *processor.CleanStage,
	segMergeStage *processor.SegMergeStage,
	styleStage *processor.StyleStage,
	exportStage *processor.ExportStage,
	subsetStage *processor.SubsetStage,
	afterStage *processor.AfterStage,
) *BatchPipeline {
	stages := []processor.Stage{
		extractStage,
		parseStage,
		segMergeStage,
		styleStage,
		exportStage,
		subsetStage,
		afterStage,
	}
	return &BatchPipeline{
		Pipeline: processor.NewPipeline(stages...),
	}
}

func (h *BatchHandler) Batch(c *gin.Context) error {
	req := &model.BatchRequest{}
	if err := c.ShouldBindJSON(req); err != nil {
		return err
	}
	ctx := c.Request.Context()

	// Read all files in the directory
	files, err := os.ReadDir(req.Path)
	if err != nil {
		return fmt.Errorf("failed to read directory: %w", err)
	}

	// Common video file extensions
	videoExts := map[string]bool{
		".mp4":  true,
		".mkv":  true,
		".avi":  true,
		".mov":  true,
		".wmv":  true,
		".m4v":  true,
		".webm": true,
	}

	// Process each file in the directory
	g := new(errgroup.Group)
	for _, file := range files {
		file := file
		g.Go(func() error {
			if file.IsDir() {
				return nil
			}

			ext := strings.ToLower(filepath.Ext(file.Name()))
			if !videoExts[ext] {
				return nil
			}

			filePath := filepath.Join(req.Path, file.Name())
			if _, err := h.pipeline.Execute(ctx, &model.ExtractRequest{
				SonarrEpisodefilePath: filePath,
			}); err != nil {
				return fmt.Errorf("failed to process video %s: %w", filePath, err)
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return fmt.Errorf("failed to process videos: %w", err)
	}

	return nil
}
