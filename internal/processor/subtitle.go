package processor

import (
	"errors"
	"fmt"
	"fusionn/configs"
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

	mode := "generated"

	chsSub, chtSub, engSub, sdhSub, err := s.parseSubtitles(extractedData)
	if err != nil {
		return err
	}

	engSub, err = s.handleSDHSubtitles(engSub, sdhSub)
	if err != nil {
		return err
	}

	chsSub, err = s.handleChineseSubtitles(chsSub, chtSub, engSub)
	if err != nil {
		return err
	}

	if chsSub == nil {
		return errors.New("no subtitles found")
	}

	mergedItems := s.algo.MatchSubtitlesCueClustering(chsSub.Items, engSub.Items, 1000*time.Millisecond)
	for i := range mergedItems {
		for j := range mergedItems[i].Lines {
			for k := range mergedItems[i].Lines[j].Items {
				mergedItems[i].Lines[j].Items[k].Text = common.ReplaceSpecialCharacters(mergedItems[i].Lines[j].Items[k].Text)
			}
		}
	}
	chsSub.Items = mergedItems
	chsSub = common.AddingStyleToAss(chsSub)
	dstpath := common.ExtractPathWithoutExtension(req.SonarrEpisodefilePath) + ".chi.ass"
	err = chsSub.Write(dstpath)
	if err != nil {
		return err
	}

	if configs.C.GetBool("apprise.enabled") {
		s.apprise.SendBasicMessage(configs.C.GetString("apprise.url"), []byte(fmt.Sprintf(msgFormat, fmt.Sprintf("Subtitle for %s %s successfully", extractedData.FileName, mode))))
	}

	return nil
}

func (s *Subtitle) parseSubtitles(data *entity.ExtractedData) (*astisub.Subtitles, *astisub.Subtitles, *astisub.Subtitles, *astisub.Subtitles, error) {
	chsSub, err := s.parser.Parse(data.ChsSubPath)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	chtSub, err := s.parser.Parse(data.ChtSubPath)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	engSub, err := s.parser.Parse(data.EngSubPath)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	sdhSub, err := s.parser.Parse(data.SdhSubPath)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	return chsSub, chtSub, engSub, sdhSub, nil
}

func (s *Subtitle) handleSDHSubtitles(engSub, sdhSub *astisub.Subtitles) (*astisub.Subtitles, error) {
	if engSub == nil && sdhSub != nil {
		log.Info("removing sdh")
		engSub = s.parser.RemoveSDH(sdhSub)
	}
	return engSub, nil
}

func (s *Subtitle) handleChineseSubtitles(chsSub, chtSub, engSub *astisub.Subtitles) (*astisub.Subtitles, error) {
	var err error
	if chsSub == nil && chtSub != nil {
		log.Info("converting cht to chs")
		chsSub, err = s.convertor.ConvertToSimplified(chtSub)
		if err != nil {
			return nil, err
		}
	}

	if chsSub == nil && engSub != nil {
		log.Info("translating eng to chs")
		chsSub, err = s.convertor.TranslateToSimplified(engSub)
		if err != nil {
			return nil, err
		}
	}

	return chsSub, nil
}
