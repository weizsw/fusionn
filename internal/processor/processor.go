package processor

import (
	"context"
	"errors"
)

var (
	ErrInvalidInput = errors.New("invalid input type")
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
				return nil, err
			}
		}
	}

	return result, nil
}
