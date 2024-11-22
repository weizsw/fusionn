package handler

import (
	"fusionn/internal/processor"

	"github.com/google/wire"
)

var Set = wire.NewSet(
	NewHandler,
	ProvidePipeline,
	processor.NewExtractStage,
	processor.NewParseStage,
	processor.NewCleanStage,
	processor.NewMergeStage,
	processor.NewStyleStage,
	processor.NewExportStage,
	processor.NewNotiStage,
)
