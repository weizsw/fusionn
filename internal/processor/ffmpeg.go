package processor

import (
	"errors"
	"fmt"
	"fusionn/internal/consts"
	"fusionn/internal/entity"
	"fusionn/internal/repository/common"
	"fusionn/internal/repository/ffmpeg"
	"fusionn/internal/repository/merger"
	"fusionn/pkg/apprise"
	"log"

	"github.com/asticode/go-astisub"
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

	merged := false

	if extractedData.CHSSubPath != "" && extractedData.EngSubPath != "" {
		err = merger.Merge(extractedData.FileName, extractedData.CHSSubPath, extractedData.EngSubPath)
		if err != nil {
			return err
		}
		merged = true
	}

	if extractedData.CHTSubPath != "" && extractedData.EngSubPath != "" && !merged {
		err = merger.Merge(extractedData.FileName, extractedData.CHTSubPath, extractedData.EngSubPath)
		if err != nil {
			return err
		}
		merged = true
	}

	if !merged {
		log.Println("no subtitles found")
		return errors.New("no subtitles found")
	}

	subtitlePath, err := common.GetTmpSubtitleFullPath(common.ExtractFilenameWithoutExtension(req.SonarrEpisodefilePath) + "." + consts.DUAL_LAN)
	if err != nil {
		return err
	}

	outputPath := common.ExtractPathWithoutExtension(subtitlePath) + ".ass"
	err = ffmpeg.ConvertSubtitleToAss(subtitlePath, outputPath)
	if err != nil {
		return err
	}
	originalASS, err := astisub.OpenFile(outputPath)
	if err != nil {
		return err
	}

	dualSubSRT, err := astisub.OpenFile(subtitlePath)
	if err != nil {
		return err
	}
	tmpSubtitlePath := common.ExtractPathWithoutExtension(subtitlePath) + ".tmp.ass"
	dualSubSRT.Write(tmpSubtitlePath)
	dualSubASS, err := astisub.OpenFile(tmpSubtitlePath)
	if err != nil {
		return err
	}

	index := 0
	for {
		if index >= len(originalASS.Items) {
			break
		}
		originalASS.Items[index].StartAt = dualSubASS.Items[index].StartAt
		originalASS.Items[index].EndAt = dualSubASS.Items[index].EndAt
		index++
	}
	dstpath := common.ExtractPathWithoutExtension(req.SonarrEpisodefilePath) + ".chi.ass"
	err = originalASS.Write(dstpath)
	if err != nil {
		return err
	}

	_, err = apprise.SendBasicMessage(consts.APPRISE, []byte(fmt.Sprintf(msgFormat, fmt.Sprintf("Subtitle for %s generated successfully", extractedData.FileName))))
	if err != nil {
		log.Println(err)
	}

	tmpPath, err := common.GetTmpDirPath()
	if err != nil {
		return err
	}

	err = common.DeleteFilesInDirectory(tmpPath)
	if err != nil {
		return err
	}

	return c.SendString("success")

}
