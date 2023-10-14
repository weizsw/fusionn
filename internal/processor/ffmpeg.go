package processor

import (
	"fmt"
	"fusionn/internal/entity"
	"fusionn/internal/repository/ffmpeg"

	"github.com/gofiber/fiber/v2"
)

func Extract(c *fiber.Ctx) error {
	req := &entity.ExtractRequest{}
	if err := c.BodyParser(req); err != nil {
		return err
	}
	fmt.Println(req.SonarrEpisodefilePath)
	return ffmpeg.ExtractSubtitles(req.SonarrEpisodefilePath)
}
