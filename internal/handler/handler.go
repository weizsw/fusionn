package handler

import (
	"errors"
	"fmt"
	"fusionn/config"
	"fusionn/internal/model"
	"fusionn/internal/service"
	"fusionn/logger"
	"fusionn/pkg"
	"fusionn/utils"
	"time"

	"github.com/asticode/go-astisub"
	"github.com/gin-gonic/gin"
	"github.com/google/wire"
)

var Set = wire.NewSet(NewHandler)

type Handler struct {
	ffmpeg    service.FFMPEG
	parser    service.Parser
	convertor service.Convertor
	algo      service.Algo
	apprise   pkg.Apprise
}

func NewHandler(ffmpeg service.FFMPEG, parser service.Parser, convertor service.Convertor, algo service.Algo, apprise pkg.Apprise) *Handler {
	return &Handler{
		ffmpeg:    ffmpeg,
		parser:    parser,
		convertor: convertor,
		algo:      algo,
		apprise:   apprise,
	}
}

var msgFormat = `{"title":"Fusionn notification","body":"%s"}`

// Merge now returns an error instead of handling it directly
func (h *Handler) Merge(c *gin.Context) error {
	req := &model.ExtractRequest{}
	if err := c.ShouldBindJSON(req); err != nil {
		return err
	}
	logger.Sugar.Infof("extracting subtitles from -> %s", req.SonarrEpisodefilePath)

	extractedData, err := h.ffmpeg.ExtractSubtitles(req.SonarrEpisodefilePath)
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
		engSub, err = h.parser.Parse(extractedData.EngSubPath)
		if err != nil {
			return err
		}

		chsSub, err = h.convertor.TranslateToSimplified(engSub)
		if err != nil {
			return err
		}
		mode = "translated"
	case extractedData.ChsSubPath == "" && extractedData.ChtSubPath != "" && extractedData.EngSubPath != "":
		engSub, err = h.parser.Parse(extractedData.EngSubPath)
		if err != nil {
			return err
		}

		chtSub, err = h.parser.Parse(extractedData.ChtSubPath)
		if err != nil {
			return err
		}

		chsSub, err = h.convertor.ConvertToSimplified(chtSub)
		if err != nil {
			return err
		}
	default:
		engSub, err = h.parser.Parse(extractedData.EngSubPath)
		if err != nil {
			return err
		}
		chsSub, err = h.parser.Parse(extractedData.ChsSubPath)
		if err != nil {
			return err
		}
	}

	mergedItems := h.algo.MatchSubtitlesCueClustering(chsSub.Items, engSub.Items, 1000*time.Millisecond)
	for i := range mergedItems {
		for j := range mergedItems[i].Lines {
			for k := range mergedItems[i].Lines[j].Items {
				mergedItems[i].Lines[j].Items[k].Text = utils.ReplaceSpecialCharacters(mergedItems[i].Lines[j].Items[k].Text)
			}
		}
	}
	chsSub.Items = mergedItems
	chsSub = utils.AddingStyleToAss(chsSub)
	dstpath := utils.ExtractPathWithoutExtension(req.SonarrEpisodefilePath) + ".chi.ass"
	err = chsSub.Write(dstpath)
	if err != nil {
		return err
	}

	if config.C.GetBool("apprise.enabled") {
		h.apprise.SendBasicMessage(config.C.GetString("apprise.url"), []byte(fmt.Sprintf(msgFormat, fmt.Sprintf("Subtitle for %s %s successfully", extractedData.FileName, mode))))
	}

	return nil
}
