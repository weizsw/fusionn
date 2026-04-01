package subtitle

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/fusionn/internal/client/bazarr"
	"github.com/fusionn/pkg/logger"
)

// BazarrSearchProcessor searches Bazarr for Chinese subtitles before falling back to translation.
type BazarrSearchProcessor struct {
	client       *bazarr.Client
	languageCode string
}

func NewBazarrSearchProcessor(client *bazarr.Client, languageCode string) *BazarrSearchProcessor {
	return &BazarrSearchProcessor{
		client:       client,
		languageCode: languageCode,
	}
}

func (p *BazarrSearchProcessor) Name() string {
	return "BazarrSearch"
}

func (p *BazarrSearchProcessor) ShouldRun(pctx *ProcessingContext) bool {
	if p.client == nil {
		return false
	}
	if pctx.ChineseSubPath != "" {
		return false
	}
	if pctx.Analysis == nil || pctx.Analysis.EnglishTrack == nil {
		return false
	}
	if pctx.Analysis.ChineseTrack != nil {
		return false
	}
	if pctx.MediaType == "episode" && pctx.SonarrEpisodeID == 0 {
		return false
	}
	if pctx.MediaType == "movie" && pctx.RadarrID == 0 {
		return false
	}
	return true
}

func (p *BazarrSearchProcessor) Process(ctx context.Context, pctx *ProcessingContext) error {
	log := logger.Indent()
	log.Info("Searching Bazarr for Chinese subtitle...")

	var err error
	switch pctx.MediaType {
	case "episode":
		err = p.searchEpisode(ctx, pctx, log)
	case "movie":
		err = p.searchMovie(ctx, pctx, log)
	default:
		log.Warnf("Unknown media type %q, skipping Bazarr search", pctx.MediaType)
		return nil
	}

	if err != nil {
		log.Warnf("Bazarr search failed (will fall back to translation): %v", err)
	}
	return nil
}

func (p *BazarrSearchProcessor) searchEpisode(ctx context.Context, pctx *ProcessingContext, log logger.IndentedLogger) error {
	if err := p.client.SearchEpisodeSubtitle(ctx, pctx.SonarrSeriesID, pctx.SonarrEpisodeID, p.languageCode); err != nil {
		return err
	}

	info, err := p.client.GetEpisodeSubtitles(ctx, pctx.SonarrEpisodeID)
	if err != nil {
		return err
	}

	return p.checkAndSetSubtitle(pctx, info.Subtitles, info.MissingSubtitles, log)
}

func (p *BazarrSearchProcessor) searchMovie(ctx context.Context, pctx *ProcessingContext, log logger.IndentedLogger) error {
	if err := p.client.SearchMovieSubtitle(ctx, pctx.RadarrID, p.languageCode); err != nil {
		return err
	}

	info, err := p.client.GetMovieSubtitles(ctx, pctx.RadarrID)
	if err != nil {
		return err
	}

	return p.checkAndSetSubtitle(pctx, info.Subtitles, info.MissingSubtitles, log)
}

func (p *BazarrSearchProcessor) checkAndSetSubtitle(pctx *ProcessingContext, subtitles []bazarr.SubtitleInfo, missing []bazarr.MissingLanguage, log logger.IndentedLogger) error {
	for _, m := range missing {
		if m.Code2 == p.languageCode {
			log.Info("Bazarr did not find a Chinese subtitle")
			return nil
		}
	}

	subPath := p.findChineseSidecar(pctx.VideoPath)
	if subPath != "" {
		log.Infof("Found Chinese subtitle from Bazarr: %s", subPath)
		pctx.ChineseSubPath = subPath
		return nil
	}

	for _, s := range subtitles {
		if s.Code2 == p.languageCode && s.Path != "" {
			if _, err := os.Stat(s.Path); err == nil {
				log.Infof("Found Chinese subtitle from Bazarr API path: %s", s.Path)
				pctx.ChineseSubPath = s.Path
				return nil
			}
		}
	}

	log.Info("Bazarr search completed but subtitle file not found locally")
	return nil
}

func (p *BazarrSearchProcessor) findChineseSidecar(videoPath string) string {
	dir := filepath.Dir(videoPath)
	base := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))

	chinesePatterns := []string{
		base + ".zh.srt",
		base + ".chi.srt",
		base + ".zho.srt",
		base + ".zh-CN.srt",
		base + ".zh-Hans.srt",
		base + ".zh-TW.srt",
		base + ".zh-Hant.srt",
	}

	for _, pattern := range chinesePatterns {
		candidate := filepath.Join(dir, pattern)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}
