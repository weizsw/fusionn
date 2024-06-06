//go:build wireinject
// +build wireinject

package internal

import (
	"github.com/gofiber/fiber/v2"
	"github.com/google/wire"
)

var ServerSet = wire.NewSet(AppSet)

func New() (*fiber.App, error) {
	wire.Build(ServerSet)
	return nil, nil
}
