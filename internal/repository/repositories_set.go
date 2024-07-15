package repository

import "github.com/google/wire"

var Set = wire.NewSet(
	NewAlgo,
	wire.Bind(new(IAlgo), new(*algo)),
	NewParser,
	wire.Bind(new(IParser), new(*parser)),
	NewFFMPEG,
	wire.Bind(new(IFFMPEG), new(*ffmpeg)),
	NewConvertor,
	wire.Bind(new(IConvertor), new(*convertor)),
)
