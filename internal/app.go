package internal

import (
	"fusionn/internal/handlers"
	"fusionn/internal/processor"
	"fusionn/internal/repository"
	"fusionn/pkg"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/log"
	"github.com/google/wire"
)

var AppSet = wire.NewSet(NewApp, repository.Set, handlers.Set, processor.Set, pkg.Set)

func NewApp(handler *handlers.Handler) *fiber.App {
	app := fiber.New()

	api := app.Group("/api")
	v1 := api.Group("/v1")
	v1.Post("/merge", handler.Merge)

	log.Info("Application is running...")
	return app
}
