package subtitle

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

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
	log.Debugf("Bazarr search: seriesID=%d, episodeID=%d, language=%s", pctx.SonarrSeriesID, pctx.SonarrEpisodeID, p.languageCode)

	if err := p.client.SearchEpisodeSubtitle(ctx, pctx.SonarrSeriesID, pctx.SonarrEpisodeID, p.languageCode); err != nil {
		return err
	}

	return p.pollEpisodeResult(ctx, pctx, log)
}

func (p *BazarrSearchProcessor) pollEpisodeResult(ctx context.Context, pctx *ProcessingContext, log logger.IndentedLogger) error {
	deadline := time.Now().Add(p.pollTimeout)
	attempt := 0

	for {
		attempt++
		info, err := p.client.GetEpisodeSubtitles(ctx, pctx.SonarrEpisodeID)
		if err != nil {
			return err
		}

		log.Debugf("Bazarr poll attempt %d: path=%s, subtitles=%d, missing=%d",
			attempt, info.Path, len(info.Subtitles), len(info.MissingSubtitles))
		for i, s := range info.Subtitles {
			log.Debugf("  subtitle[%d]: code2=%q, name=%q, path=%q", i, s.Code2, s.Name, s.Path)
		}
		for i, m := range info.MissingSubtitles {
			log.Debugf("  missing[%d]: code2=%q, name=%q", i, m.Code2, m.Name)
		}

		stillMissing := false
		for _, m := range info.MissingSubtitles {
			if m.Code2 == p.languageCode {
				stillMissing = true
				break
			}
		}

		if !stillMissing {
			log.Infof("Bazarr reports %q no longer missing after %d poll(s)", p.languageCode, attempt)
			return p.checkAndSetSubtitle(pctx, info.Subtitles, info.MissingSubtitles, log)
		}

		if time.Now().After(deadline) {
			log.Infof("Bazarr poll timed out after %s (%d attempts) — %q still in missing_subtitles",
				p.pollTimeout, attempt, p.languageCode)
			return p.checkAndSetSubtitle(pctx, info.Subtitles, info.MissingSubtitles, log)
		}

		log.Debugf("Language %q still missing, waiting %s before next poll...", p.languageCode, p.pollInterval)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(p.pollInterval):
		}
	}
}

func (p *BazarrSearchProcessor) searchMovie(ctx context.Context, pctx *ProcessingContext, log logger.IndentedLogger) error {
	log.Debugf("Bazarr search: radarrID=%d, language=%s", pctx.RadarrID, p.languageCode)

	if err := p.client.SearchMovieSubtitle(ctx, pctx.RadarrID, p.languageCode); err != nil {
		return err
	}

	return p.pollMovieResult(ctx, pctx, log)
}

func (p *BazarrSearchProcessor) pollMovieResult(ctx context.Context, pctx *ProcessingContext, log logger.IndentedLogger) error {
	deadline := time.Now().Add(p.pollTimeout)
	attempt := 0

	for {
		attempt++
		info, err := p.client.GetMovieSubtitles(ctx, pctx.RadarrID)
		if err != nil {
			return err
		}

		log.Debugf("Bazarr poll attempt %d: path=%s, subtitles=%d, missing=%d",
			attempt, info.Path, len(info.Subtitles), len(info.MissingSubtitles))
		for i, s := range info.Subtitles {
			log.Debugf("  subtitle[%d]: code2=%q, name=%q, path=%q", i, s.Code2, s.Name, s.Path)
		}
		for i, m := range info.MissingSubtitles {
			log.Debugf("  missing[%d]: code2=%q, name=%q", i, m.Code2, m.Name)
		}

		stillMissing := false
		for _, m := range info.MissingSubtitles {
			if m.Code2 == p.languageCode {
				stillMissing = true
				break
			}
		}

		if !stillMissing {
			log.Infof("Bazarr reports %q no longer missing after %d poll(s)", p.languageCode, attempt)
			return p.checkAndSetSubtitle(pctx, info.Subtitles, info.MissingSubtitles, log)
		}

		if time.Now().After(deadline) {
			log.Infof("Bazarr poll timed out after %s (%d attempts) — %q still in missing_subtitles",
				p.pollTimeout, attempt, p.languageCode)
			return p.checkAndSetSubtitle(pctx, info.Subtitles, info.MissingSubtitles, log)
		}

		log.Debugf("Language %q still missing, waiting %s before next poll...", p.languageCode, p.pollInterval)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(p.pollInterval):
		}
	}
}

func (p *BazarrSearchProcessor) checkAndSetSubtitle(pctx *ProcessingContext, subtitles []bazarr.SubtitleInfo, missing []bazarr.MissingLanguage, log logger.IndentedLogger) error {
	log.Debugf("Checking Bazarr result: languageCode=%q, missing=%d, subtitles=%d", p.languageCode, len(missing), len(subtitles))

	for _, m := range missing {
		if m.Code2 == p.languageCode {
			log.Infof("Bazarr did not find a Chinese subtitle (code2=%q still in missing_subtitles)", m.Code2)
			return nil
		}
	}

	log.Debugf("Language %q not in missing_subtitles, searching for sidecar files...", p.languageCode)
	subPath := p.findChineseSidecar(pctx.VideoPath, log)
	if subPath != "" {
		log.Infof("Found Chinese subtitle from Bazarr: %s", subPath)
		pctx.ChineseSubPath = subPath
		pctx.ChineseSubSource = ChineseSourceBazarr
		return nil
	}

	for _, s := range subtitles {
		if s.Code2 == p.languageCode && s.Path != "" {
			_, statErr := os.Stat(s.Path)
			if statErr == nil {
				log.Infof("Found Chinese subtitle from Bazarr API path: %s", s.Path)
				pctx.ChineseSubPath = s.Path
				pctx.ChineseSubSource = ChineseSourceBazarr
				return nil
			}
			log.Debugf("Bazarr API subtitle path not accessible: %s (err: %v)", s.Path, statErr)
		}
	}

	log.Info("Bazarr search completed but subtitle file not found locally")
	return nil
}

func (p *BazarrSearchProcessor) findChineseSidecar(videoPath string, log logger.IndentedLogger) string {
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
	log.Debugf("No sidecar found, checked patterns: %s", fmt.Sprintf("%v", checked))
	return ""
}
