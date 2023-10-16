package main

import (
	"fusionn/internal/processor"

	"github.com/gofiber/fiber/v2"
)

func main() {
	app := fiber.New()
	api := app.Group("/api")
	v1 := api.Group("/v1")
	v1.Post("/extract", processor.Extract)
	v1.Post("/merge", processor.Merge)
	app.Listen("0.0.0.0:4664")
}
