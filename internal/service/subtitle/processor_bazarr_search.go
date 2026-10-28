package subtitle

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/fusionn/internal/client/bazarr"
	"github.com/fusionn/pkg/logger"
)

// BazarrSearchProcessor searches Bazarr for Chinese subtitles before falling back to translation.
type BazarrSearchProcessor struct {
	client       *bazarr.Client
	languageCode string
	pollInterval time.Duration
	pollTimeout  time.Duration
}

func NewBazarrSearchProcessor(client *bazarr.Client, languageCode string, pollIntervalSec, pollTimeoutSec int) *BazarrSearchProcessor {
	if pollIntervalSec <= 0 {
		pollIntervalSec = 5
	}
	if pollTimeoutSec <= 0 {
		pollTimeoutSec = 60
	}
	return &BazarrSearchProcessor{
		client:       client,
		languageCode: languageCode,
		pollInterval: time.Duration(pollIntervalSec) * time.Second,
		pollTimeout:  time.Duration(pollTimeoutSec) * time.Second,
	}
}

func (p *BazarrSearchProcessor) Name() string {
	return ProcessorNameBazarrSearch
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
	if pctx.MediaType == MediaTypeEpisode && pctx.SonarrEpisodeID == 0 {
		return false
	}
	if pctx.MediaType == MediaTypeMovie && pctx.RadarrID == 0 {
		return false
	}
	return true
}

func (p *BazarrSearchProcessor) Process(ctx context.Context, pctx *ProcessingContext) error {
	log := logger.Component("bazarr_search")
	log.Info(ctx, "search_started", "Searching Bazarr for Chinese subtitle")

	var err error
	switch pctx.MediaType {
	case MediaTypeEpisode:
		err = p.searchEpisode(ctx, pctx, log)
	case MediaTypeMovie:
		err = p.searchMovie(ctx, pctx, log)
	default:
		log.Warn(ctx, "search_skipped", "Unknown media type, skipping Bazarr search", zap.String("media_type", pctx.MediaType))
		return nil
	}

	if err != nil {
		log.Warn(ctx, "search_failed", "Bazarr search failed, falling back to translation", zap.Error(err))
	}
	return nil
}

func (p *BazarrSearchProcessor) searchEpisode(ctx context.Context, pctx *ProcessingContext, log logger.Logger) error {
	log.Debug(ctx, "search_detail", "Bazarr episode search",
		zap.Int("series_id", pctx.SonarrSeriesID), zap.Int("episode_id", pctx.SonarrEpisodeID),
		zap.String("language", p.languageCode))

	if err := p.client.SearchEpisodeSubtitle(ctx, pctx.SonarrSeriesID, pctx.SonarrEpisodeID, p.languageCode); err != nil {
		return err
	}

	return p.pollEpisodeResult(ctx, pctx, log)
}

func (p *BazarrSearchProcessor) pollEpisodeResult(ctx context.Context, pctx *ProcessingContext, log logger.Logger) error {
	deadline := time.Now().Add(p.pollTimeout)
	attempt := 0

	for {
		attempt++
		info, err := p.client.GetEpisodeSubtitles(ctx, pctx.SonarrEpisodeID)
		if err != nil {
			return err
		}

		log.Debug(ctx, "search_poll", "Bazarr poll attempt",
			zap.Int("attempt", attempt), zap.Int("subtitles", len(info.Subtitles)),
			zap.Int("missing", len(info.MissingSubtitles)), logger.Preview(info.Path))
		for i, s := range info.Subtitles {
			log.Debug(ctx, "search_candidate", "Bazarr subtitle candidate",
				zap.Int("index", i), zap.String("code2", s.Code2), logger.Preview(s.Name))
		}
		for i, m := range info.MissingSubtitles {
			log.Debug(ctx, "search_missing", "Bazarr missing language",
				zap.Int("index", i), zap.String("code2", m.Code2), logger.Preview(m.Name))
		}

		stillMissing := false
		for _, m := range info.MissingSubtitles {
			if m.Code2 == p.languageCode {
				stillMissing = true
				break
			}
		}

		if !stillMissing {
			log.Info(ctx, "search_found", "Bazarr reports language no longer missing",
				zap.String("language", p.languageCode), zap.Int("attempts", attempt))
			return p.checkAndSetSubtitle(ctx, pctx, info.Subtitles, info.MissingSubtitles, log)
		}

		if time.Now().After(deadline) {
			log.Warn(ctx, "search_timeout", "Bazarr poll timed out, language still missing",
				zap.String("language", p.languageCode), zap.Int("attempts", attempt))
			return p.checkAndSetSubtitle(ctx, pctx, info.Subtitles, info.MissingSubtitles, log)
		}

		log.Debug(ctx, "search_waiting", "Language still missing, waiting before next poll",
			zap.String("language", p.languageCode), logger.DurMs(p.pollInterval))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(p.pollInterval):
		}
	}
}

func (p *BazarrSearchProcessor) searchMovie(ctx context.Context, pctx *ProcessingContext, log logger.Logger) error {
	log.Debug(ctx, "search_detail", "Bazarr movie search",
		zap.Int("radarr_id", pctx.RadarrID), zap.String("language", p.languageCode))

	if err := p.client.SearchMovieSubtitle(ctx, pctx.RadarrID, p.languageCode); err != nil {
		return err
	}

	return p.pollMovieResult(ctx, pctx, log)
}

func (p *BazarrSearchProcessor) pollMovieResult(ctx context.Context, pctx *ProcessingContext, log logger.Logger) error {
	deadline := time.Now().Add(p.pollTimeout)
	attempt := 0

	for {
		attempt++
		info, err := p.client.GetMovieSubtitles(ctx, pctx.RadarrID)
		if err != nil {
			return err
		}

		log.Debug(ctx, "search_poll", "Bazarr poll attempt",
			zap.Int("attempt", attempt), zap.Int("subtitles", len(info.Subtitles)),
			zap.Int("missing", len(info.MissingSubtitles)), logger.Preview(info.Path))
		for i, s := range info.Subtitles {
			log.Debug(ctx, "search_candidate", "Bazarr subtitle candidate",
				zap.Int("index", i), zap.String("code2", s.Code2), logger.Preview(s.Name))
		}
		for i, m := range info.MissingSubtitles {
			log.Debug(ctx, "search_missing", "Bazarr missing language",
				zap.Int("index", i), zap.String("code2", m.Code2), logger.Preview(m.Name))
		}

		stillMissing := false
		for _, m := range info.MissingSubtitles {
			if m.Code2 == p.languageCode {
				stillMissing = true
				break
			}
		}

		if !stillMissing {
			log.Info(ctx, "search_found", "Bazarr reports language no longer missing",
				zap.String("language", p.languageCode), zap.Int("attempts", attempt))
			return p.checkAndSetSubtitle(ctx, pctx, info.Subtitles, info.MissingSubtitles, log)
		}

		if time.Now().After(deadline) {
			log.Warn(ctx, "search_timeout", "Bazarr poll timed out, language still missing",
				zap.String("language", p.languageCode), zap.Int("attempts", attempt))
			return p.checkAndSetSubtitle(ctx, pctx, info.Subtitles, info.MissingSubtitles, log)
		}

		log.Debug(ctx, "search_waiting", "Language still missing, waiting before next poll",
			zap.String("language", p.languageCode), logger.DurMs(p.pollInterval))
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(p.pollInterval):
		}
	}
}

func (p *BazarrSearchProcessor) checkAndSetSubtitle(ctx context.Context, pctx *ProcessingContext, subtitles []bazarr.SubtitleInfo, missing []bazarr.MissingLanguage, log logger.Logger) error {
	log.Debug(ctx, "search_result", "Checking Bazarr result",
		zap.String("language", p.languageCode), zap.Int("missing", len(missing)), zap.Int("subtitles", len(subtitles)))

	for _, m := range missing {
		if m.Code2 == p.languageCode {
			log.Info(ctx, "search_not_found", "Bazarr did not find a Chinese subtitle",
				zap.String("code2", m.Code2))
			return nil
		}
	}

	log.Debug(ctx, "search_sidecar", "Language not missing anymore, searching for sidecar files",
		zap.String("language", p.languageCode))
	subPath := p.findChineseSidecar(ctx, pctx.VideoPath, log)
	if subPath != "" {
		log.Info(ctx, "subtitle_found", "Found Chinese subtitle from Bazarr", zap.String("path", subPath))
		pctx.ChineseSubPath = subPath
		pctx.ChineseSubSource = ChineseSourceBazarr
		return nil
	}

	for _, s := range subtitles {
		if s.Code2 == p.languageCode && s.Path != "" {
			_, statErr := os.Stat(s.Path)
			if statErr == nil {
				log.Info(ctx, "subtitle_found", "Found Chinese subtitle from Bazarr API path", zap.String("path", s.Path))
				pctx.ChineseSubPath = s.Path
				pctx.ChineseSubSource = ChineseSourceBazarr
				return nil
			}
			log.Debug(ctx, "subtitle_path_inaccessible", "Bazarr API subtitle path not accessible",
				logger.Preview(s.Path), zap.Error(statErr))
		}
	}

	log.Info(ctx, "search_completed", "Bazarr search completed but subtitle file not found locally")
	return nil
}

func (p *BazarrSearchProcessor) findChineseSidecar(ctx context.Context, videoPath string, log logger.Logger) string {
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

	var checked []string
	for _, pattern := range chinesePatterns {
		candidate := filepath.Join(dir, pattern)
		checked = append(checked, pattern)
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	log.Debug(ctx, "sidecar_not_found", "No Chinese sidecar found", logger.Preview(fmt.Sprintf("%v", checked)))
	return ""
}
