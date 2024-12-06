package errs

import "errors"

var (
	ErrStopPipeline = errors.New("stop pipeline")
	ErrInvalidInput = errors.New("invalid input type")
)
