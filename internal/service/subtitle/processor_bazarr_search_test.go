package subtitle

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/fusionn/internal/client/bazarr"
	"github.com/fusionn/pkg/logger"
)

// checkBazarrSelection runs the selection step the Bazarr poll loop uses, with
// no client and no network, and returns the context it filled in.
func checkBazarrSelection(t *testing.T, videoPath string, subtitles []bazarr.SubtitleInfo) *ProcessingContext {
	t.Helper()
	captureLogs(t)
	processor := NewBazarrSearchProcessor(nil, "zh", 1, 1)
	pctx := &ProcessingContext{VideoPath: videoPath, MediaType: MediaTypeEpisode}
	if err := processor.checkAndSetSubtitle(context.Background(), pctx, subtitles, nil, logger.Component("bazarr_search")); err != nil {
		t.Fatalf("checkAndSetSubtitle: %v", err)
	}
	return pctx
}

func writeSubtitleFile(t *testing.T, path string) string {
	t.Helper()
	if err := os.WriteFile(path, []byte("1\n00:00:01,000 --> 00:00:03,000\n你好\n"), 0644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// A sidecar or API subtitle named with a Traditional language tag must ask the
// merge stage for OpenCC conversion; Simplified and untagged names must not.
func TestBazarrSelectionSetsNeedsConversionFromLanguageTag(t *testing.T) {
	cases := []struct {
		name           string
		subtitleName   string
		apiPath        bool
		wantConversion bool
	}{
		{name: "sidecar zh-TW", subtitleName: "Episode.zh-TW.srt", wantConversion: true},
		{name: "sidecar zh-Hant", subtitleName: "Episode.zh-Hant.srt", wantConversion: true},
		{name: "sidecar zh", subtitleName: "Episode.zh.srt"},
		{name: "sidecar chi", subtitleName: "Episode.chi.srt"},
		{name: "sidecar zh-CN", subtitleName: "Episode.zh-CN.srt"},
		{name: "sidecar zh-Hans", subtitleName: "Episode.zh-Hans.srt"},
		{name: "api zh-TW", subtitleName: "downloaded.zh-TW.srt", apiPath: true, wantConversion: true},
		{name: "api zh-Hant mixed case", subtitleName: "downloaded.ZH-hant.srt", apiPath: true, wantConversion: true},
		{name: "api zh", subtitleName: "downloaded.zh.srt", apiPath: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			videoPath := filepath.Join(dir, "Episode.mkv")
			subtitlePath := writeSubtitleFile(t, filepath.Join(dir, tc.subtitleName))

			subtitles := []bazarr.SubtitleInfo{}
			if tc.apiPath {
				subtitles = append(subtitles, bazarr.SubtitleInfo{Code2: "zh", Path: subtitlePath})
			}

			pctx := checkBazarrSelection(t, videoPath, subtitles)

			if pctx.ChineseSubPath != subtitlePath {
				t.Fatalf("ChineseSubPath = %q, want %q", pctx.ChineseSubPath, subtitlePath)
			}
			if pctx.ChineseSubSource != ChineseSourceBazarr {
				t.Errorf("ChineseSubSource = %q, want %q", pctx.ChineseSubSource, ChineseSourceBazarr)
			}
			if pctx.NeedsConversion != tc.wantConversion {
				t.Errorf("NeedsConversion = %v, want %v for %q", pctx.NeedsConversion, tc.wantConversion, tc.subtitleName)
			}
		})
	}
}
