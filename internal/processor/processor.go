package processor

import (
	"context"
	"errors"

	"fusionn/errs"
)

// Stage represents a single processing stage
type Stage interface {
	Process(ctx context.Context, input any) (any, error)
}

// Pipeline represents a series of processing stages
type Pipeline struct {
	stages []Stage
}

// NewPipeline creates a new processing pipeline with the given stages
func NewPipeline(stages ...Stage) *Pipeline {
	return &Pipeline{
		stages: stages,
	}
}

// Execute runs the input through all stages in the pipeline
func (p *Pipeline) Execute(ctx context.Context, input any) (any, error) {
	var result any = input

	for _, stage := range p.stages {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
			var err error
			result, err = stage.Process(ctx, result)
			if err != nil {
				if errors.Is(err, errs.ErrStopPipeline) {
					// Return the current result without error if it's a stop signal
					return result, nil
				}
				return nil, err
			}
		}
	}

	return result, nil
}
