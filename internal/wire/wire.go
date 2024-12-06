//go:build wireinject
// +build wireinject

package wire

import (
	"fusionn/internal/cache"
	"fusionn/internal/database"
	"fusionn/internal/handler"
	"fusionn/internal/mq"
	"fusionn/internal/processor"
	"fusionn/internal/server"
	"fusionn/internal/service"
	"fusionn/pkg"
	"net/http"

	"github.com/google/wire"
)

// ServerSet is a Wire provider set that includes all server dependencies
var ServerSet = wire.NewSet(
	pkg.Set,
	service.Set,
	database.Set,
	handler.Set,
	server.Set,
	processor.Set,
	cache.Set,
	mq.Set,
)

// NewServer creates a new HTTP server with all its dependencies
func NewServer() (*http.Server, error) {
	wire.Build(ServerSet)
	return nil, nil
}
