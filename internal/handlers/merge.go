package handlers

import (
	"fusionn/internal/processor"

	"github.com/gofiber/fiber/v2"
)

type Handler struct {
	MergeProcessor processor.ISubtitle
}

func NewHandler(p processor.ISubtitle) *Handler {
	return &Handler{
		MergeProcessor: p,
	}
}

func (h *Handler) Merge(c *fiber.Ctx) error {
	return h.MergeProcessor.Merge(c)
}
