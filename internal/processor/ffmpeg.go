package processor

import (
	"errors"
	"fusionn/internal/entity"
	"fusionn/internal/repository/ffmpeg"
	"fusionn/internal/repository/merger"
	"log"

	"github.com/gofiber/fiber/v2"
)

func Extract(c *fiber.Ctx) error {
	req := &entity.ExtractRequest{}
	if err := c.BodyParser(req); err != nil {
		return err
	}
	log.Println("extracting subtitles from ->", req.SonarrEpisodefilePath)
	var (
		err           error
		extractedData *entity.ExtractData
	)
	extractedData, err = ffmpeg.ExtractSubtitles(req.SonarrEpisodefilePath)
	if err != nil {
		return err
	}

	if extractedData.CHSSubPath != "" && extractedData.EngSubPath != "" {
		err = merger.Merge(extractedData.FileName, extractedData.CHSSubPath, extractedData.EngSubPath, req.SonarrEpisodefilePath)
		if err != nil {
			return err
		}
		return nil
	}

	if extractedData.CHTSubPath != "" && extractedData.EngSubPath != "" {
		err = merger.Merge(extractedData.FileName, extractedData.CHTSubPath, extractedData.EngSubPath, req.SonarrEpisodefilePath)
		if err != nil {
			return err
		}
		return nil
	}

	return errors.New("no subtitle found")
}
