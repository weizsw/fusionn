package pkg

import "github.com/google/wire"

var Set = wire.NewSet(wire.Bind(new(IDeepL), new(*deepL)), NewDeepL)
