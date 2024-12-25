package service

import "github.com/google/wire"

var Set = wire.NewSet(
	NewAlgo,
	wire.Bind(new(Algo), new(*algo)),
	NewParser,
	wire.Bind(new(Parser), new(*parser)),
	NewFFMPEG,
	wire.Bind(new(FFMPEG), new(*ffmpeg)),
	NewConvertor,
	wire.Bind(new(Convertor), new(*convertor)),
	NewStyleService,
	wire.Bind(new(StyleService), new(*styleService)),
	NewModifyService,
	wire.Bind(new(ModifyService), new(*modifyService)),
	NewFacade,
	wire.Bind(new(Facade), new(*facade)),
)
