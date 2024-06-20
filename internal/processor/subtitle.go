package processor

import (
	"errors"
	"fmt"
	"fusionn/internal/consts"
	"fusionn/internal/entity"
	"fusionn/internal/repository"
	"fusionn/internal/repository/common"
	"fusionn/pkg"
	"time"

	"github.com/asticode/go-astisub"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/log"
)

type ISubtitle interface {
	Merge(c *fiber.Ctx) error
}

type Subtitle struct {
	ffmpeg    repository.IFFMPEG
	parser    repository.IParser
	convertor repository.IConvertor
	algo      repository.IAlgo
	apprise   pkg.IApprise
}

func NewSubtitle(
	ffmpeg repository.IFFMPEG,
	parser repository.IParser,
	convertor repository.IConvertor,
	algo repository.IAlgo,
	apprise pkg.IApprise,
) *Subtitle {
	return &Subtitle{
		ffmpeg:    ffmpeg,
		parser:    parser,
		convertor: convertor,
		algo:      algo,
		apprise:   apprise,
	}
}

var msgFormat = `{"title":"Fusionn notification","body":"%s"}`

func (s *Subtitle) Merge(c *fiber.Ctx) error {
	req := &entity.ExtractRequest{}
	if err := c.BodyParser(req); err != nil {
		return err
	}
	log.Info("extracting subtitles from ->", req.SonarrEpisodefilePath)

	extractedData, err := s.ffmpeg.ExtractSubtitles(req.SonarrEpisodefilePath)
	if err != nil {
		return err
	}

	var (
		chsSub *astisub.Subtitles
		chtSub *astisub.Subtitles
		engSub *astisub.Subtitles
	)
	mode := "generated"

	switch {
	case extractedData.EngSubPath == "":
		return errors.New("no english subtitles found")
	case extractedData.ChsSubPath == "" && extractedData.ChtSubPath == "" && extractedData.EngSubPath == "":
		return errors.New("no subtitles found")
	case extractedData.ChsSubPath == "" && extractedData.ChtSubPath == "" && extractedData.EngSubPath != "":
		engSub, err = s.parser.Parse(extractedData.EngSubPath)
		if err != nil {
			return err
		}

		chsSub, err = s.convertor.TranslateToSimplified(engSub)
		if err != nil {
			return err
		}
		mode = "translated"
	case extractedData.ChsSubPath == "" && extractedData.ChtSubPath != "" && extractedData.EngSubPath != "":
		engSub, err = s.parser.Parse(extractedData.EngSubPath)
		if err != nil {
			return err
		}

		chtSub, err = s.parser.Parse(extractedData.ChtSubPath)
		if err != nil {
			return err
		}

		chsSub, err = s.convertor.ConvertToSimplified(chtSub)
		if err != nil {
			return err
		}
	default:
		engSub, err = s.parser.Parse(extractedData.EngSubPath)
		if err != nil {
			return err
		}
		chsSub, err = s.parser.Parse(extractedData.ChsSubPath)
		if err != nil {
			return err
		}
	}

	mergedItems := s.algo.MatchSubtitlesCueClustering(chsSub.Items, engSub.Items, 500*time.Millisecond)

	chsSub.Items = mergedItems
	dstpath := common.ExtractPathWithoutExtension(req.SonarrEpisodefilePath) + ".chi.ass"
	err = chsSub.Write(dstpath)
	if err != nil {
		return err
	}
	s.apprise.SendBasicMessage(consts.APPRISE, []byte(fmt.Sprintf(msgFormat, fmt.Sprintf("Subtitle for %s %s successfully", extractedData.FileName, mode))))

	return nil
}
