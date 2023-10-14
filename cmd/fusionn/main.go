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

	app.Listen("localhost:4664")
}
