package db

import (
	"github.com/google/wire"
)

var Set = wire.NewSet(NewDatabase, wire.Bind(new(IDatabase), new(*database)))
