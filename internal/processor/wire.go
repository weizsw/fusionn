package processor

import "github.com/google/wire"

var Set = wire.NewSet(NewPipeline, NewExtractStage, NewParseStage)
