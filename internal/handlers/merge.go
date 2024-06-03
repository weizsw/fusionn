package handlers

import (
	"fusionn/internal/processor"

	"github.com/gofiber/fiber/v2"
)

type MergeHandler struct {
	MergeProcessor processor.Merge
}

func NewMergeHandler(mp processor.Merge) *MergeHandler {
	return &MergeHandler{
		MergeProcessor: mp,
	}
}

func (mh *MergeHandler) Merge(c *fiber.Ctx) error {
	return c.JSON(mh.MergeProcessor.Merge(c))
}
