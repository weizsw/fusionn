package pkg

import "github.com/google/wire"

var Set = wire.NewSet(
	wire.Bind(new(DeepL), new(*deepL)), NewDeepL,
	wire.Bind(new(Apprise), new(*apprise)), NewApprise,
	wire.Bind(new(SubTrans), new(*subTrans)), NewSubTrans,
)
