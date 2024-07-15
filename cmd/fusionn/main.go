package main

import (
	server "fusionn/internal"

	"github.com/gofiber/fiber/v2/log"
)

func main() {
	server, _ := server.New()
	log.Fatal(server.Listen(":4664"))
}
