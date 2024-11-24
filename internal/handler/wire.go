package handler

import (
	"fusionn/internal/processor"

	"github.com/google/wire"
)

// MergePipeline represents the pipeline for merge operations
type MergePipeline struct {
	*processor.Pipeline
}

// BatchPipeline represents the pipeline for batch operations
type BatchPipeline struct {
	*processor.Pipeline
}

type AsyncMergePipeline struct {
	*processor.Pipeline
}

var Set = wire.NewSet(
	NewMergeHandler,
	NewBatchHandler,
	NewAsyncMergeHandler,
	ProvideMergePipeline,
	ProvideBatchPipeline,
	ProvideAsyncMergePipeline,
)
