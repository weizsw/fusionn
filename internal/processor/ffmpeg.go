package processor

import (
	"errors"
	"fmt"
	"fusionn/internal/consts"
	"fusionn/internal/entity"
	"fusionn/internal/repository/ffmpeg"
	"fusionn/internal/repository/merger"
	"fusionn/pkg/apprise"
	"log"

	"github.com/gofiber/fiber/v2"
)

var msgFormat = `{"title":"Fusionn notification","body":"%s"}`

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
		_, err = apprise.SendBasicMessage(consts.APPRISE, []byte(fmt.Sprintf(msgFormat, fmt.Sprintf("Subtitle for %s generated successfully", extractedData.FileName))))
		if err != nil {
			log.Println(err)
		}
		return c.SendString("success")
	}

	if extractedData.CHTSubPath != "" && extractedData.EngSubPath != "" {
		err = merger.Merge(extractedData.FileName, extractedData.CHTSubPath, extractedData.EngSubPath, req.SonarrEpisodefilePath)
		if err != nil {
			return err
		}
		_, err = apprise.SendBasicMessage(consts.APPRISE, []byte(fmt.Sprintf(msgFormat, fmt.Sprintf("Subtitle for %s generated successfully", extractedData.FileName))))
		if err != nil {
			log.Println(err)
		}
		return c.SendString("success")
	}
	log.Println("no subtitles found")
	return errors.New("no subtitles found")
}
