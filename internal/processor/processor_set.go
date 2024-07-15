package processor

import (
	"github.com/google/wire"
)

var Set = wire.NewSet(wire.Bind(new(ISubtitle), new(*Subtitle)), NewSubtitle)
