package processor

import (
	"errors"
	"fusionn/internal/entity"
	"fusionn/internal/repository/algo"
	"fusionn/internal/repository/common"
	"fusionn/internal/repository/convertor"
	"fusionn/internal/repository/ffmpeg"
	"fusionn/internal/repository/parser"
	"time"

	"github.com/asticode/go-astisub"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/log"
)

type Merge interface {
	Merge(c *fiber.Ctx) error
}

type merge struct {
	ffmpeg    ffmpeg.FFMPEG
	parser    parser.Parser
	convertor convertor.Convertor
	algo      algo.Algo
}

func (m *merge) Merge(c *fiber.Ctx) error {
	req := &entity.ExtractRequest{}
	if err := c.BodyParser(req); err != nil {
		return err
	}
	log.Info("extracting subtitles from ->", req.SonarrEpisodefilePath)

	extractedData, err := m.ffmpeg.ExtractSubtitles(req.SonarrEpisodefilePath)
	if err != nil {
		return err
	}

	var (
		chsSub *astisub.Subtitles
		chtSub *astisub.Subtitles
		engSub *astisub.Subtitles
	)

	if extractedData.ChsSubPath == "" && extractedData.ChtSubPath == "" && extractedData.EngSubPath == "" {
		return errors.New("no subtitles found")
	}

	if extractedData.ChsSubPath == "" && extractedData.ChtSubPath == "" && extractedData.EngSubPath != "" {
		engSub, err = m.parser.Parse(extractedData.EngSubPath)
		if err != nil {
			return err
		}

		chsSub, err = m.convertor.TranslateToSimplified(engSub)
		if err != nil {
			return err
		}
	}

	if extractedData.ChsSubPath == "" && extractedData.ChtSubPath != "" && extractedData.EngSubPath != "" {
		engSub, err = m.parser.Parse(extractedData.EngSubPath)
		if err != nil {
			return err
		}

		chtSub, err = m.parser.Parse(extractedData.ChtSubPath)
		if err != nil {
			return err
		}

		chsSub, err = m.convertor.ConvertToSimplified(chtSub)
		if err != nil {
			return err
		}
	}

	mergedItems := m.algo.MatchSubtitlesCueClustering(engSub.Items, chsSub.Items, 500*time.Millisecond)

	chsSub.Items = mergedItems
	dstpath := common.ExtractPathWithoutExtension(req.SonarrEpisodefilePath) + ".chi.ass"
	err = chsSub.Write(dstpath)
	if err != nil {
		return err
	}

	return nil
}
