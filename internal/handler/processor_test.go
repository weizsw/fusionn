package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/fusionn/internal/service/subtitle"
)

// withSubtitlesCall records one ProcessWithSubtitles call.
type withSubtitlesCall struct {
	videoPath   string
	engSubPath  string
	chsSubPath  string
	parentJobID string
}

// recordingProcessor is a MediaProcessor fake that records what the handlers
// trigger. The webhook calls ProcessMedia on a background goroutine, so each
// call arrives over a channel.
type recordingProcessor struct {
	mediaCalls chan subtitle.MediaParams
	subCalls   chan withSubtitlesCall
	mediaErr   error
	subErr     error
}

func newRecordingProcessor() *recordingProcessor {
	return &recordingProcessor{
		mediaCalls: make(chan subtitle.MediaParams, 4),
		subCalls:   make(chan withSubtitlesCall, 4),
	}
}

func (p *recordingProcessor) ProcessMedia(_ context.Context, params subtitle.MediaParams) error {
	p.mediaCalls <- params
	return p.mediaErr
}

func (p *recordingProcessor) ProcessWithSubtitles(_ context.Context, videoPath, engSubPath, chsSubPath, parentJobID string) error {
	p.subCalls <- withSubtitlesCall{videoPath: videoPath, engSubPath: engSubPath, chsSubPath: chsSubPath, parentJobID: parentJobID}
	return p.subErr
}

// waitMediaCall returns the recorded ProcessMedia call or fails the test.
func (p *recordingProcessor) waitMediaCall(t *testing.T) subtitle.MediaParams {
	t.Helper()
	select {
	case params := <-p.mediaCalls:
		return params
	case <-time.After(5 * time.Second):
		t.Fatal("ProcessMedia was not called")
		return subtitle.MediaParams{}
	}
}

// waitSubCall returns the recorded ProcessWithSubtitles call or fails the test.
func (p *recordingProcessor) waitSubCall(t *testing.T) withSubtitlesCall {
	t.Helper()
	select {
	case call := <-p.subCalls:
		return call
	case <-time.After(5 * time.Second):
		t.Fatal("ProcessWithSubtitles was not called")
		return withSubtitlesCall{}
	}
}

// assertNoMediaCall fails when a ProcessMedia call shows up. The grace period
// catches a regression that makes an ignored event start work.
func (p *recordingProcessor) assertNoMediaCall(t *testing.T) {
	t.Helper()
	select {
	case params := <-p.mediaCalls:
		t.Fatalf("unexpected ProcessMedia call: %+v", params)
	case <-time.After(100 * time.Millisecond):
	}
}

// serveWebhook posts one JSON payload to a handler and returns the recorder.
func serveWebhook(t *testing.T, handle gin.HandlerFunc, route string, payload interface{}) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, route, bytes.NewBuffer(body))
	c.Request.Header.Set("Content-Type", "application/json")
	handle(c)
	return w
}

// writeSubtitleFiles creates the two subtitle files the callback validates.
func writeSubtitleFiles(t *testing.T) (eng, chs string) {
	t.Helper()
	dir := t.TempDir()
	eng, chs = filepath.Join(dir, "v.en.srt"), filepath.Join(dir, "v.chs.srt")
	for _, path := range []string{eng, chs} {
		if err := os.WriteFile(path, []byte("1\n00:00:01,000 --> 00:00:02,000\nHi\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return eng, chs
}

// A Sonarr Download event must start real work with the payload's media
// identity, not only answer 202.
func TestSonarrDownloadStartsProcessMedia(t *testing.T) {
	processor := newRecordingProcessor()

	rec := serveWebhook(t, NewWebhookHandler(processor).HandleSonarr, "/api/v1/webhook/sonarr", SonarrPayload{
		EventType: WebhookEventDownload,
		Series: SeriesInfo{
			ID:     42,
			Title:  "The Capture",
			Path:   "/tv/The Capture",
			TVDBID: 355620,
			ImdbID: "tt8201186",
		},
		Episodes: []EpisodeInfo{{
			ID:            101,
			Title:         "Correction",
			EpisodeNumber: 1,
			SeasonNumber:  1,
		}},
		EpisodeFile: EpisodeFile{Path: "/tv/The Capture/Season 01/S01E01.mkv"},
	})

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body %s", rec.Code, rec.Body)
	}

	want := subtitle.MediaParams{
		Path:            "/tv/The Capture/Season 01/S01E01.mkv",
		MediaType:       subtitle.MediaTypeEpisode,
		Title:           "The Capture S01E01",
		SonarrSeriesID:  42,
		SonarrEpisodeID: 101,
		SourceSystem:    subtitle.SourceSystemSonarr,
		MediaID:         "42",
		ExternalIDs: map[string]string{
			subtitle.ExternalIDSonarr: "42",
			subtitle.ExternalIDTVDB:   "355620",
			subtitle.ExternalIDIMDB:   "tt8201186",
		},
		Season:  1,
		Episode: 1,
	}
	if got := processor.waitMediaCall(t); !reflect.DeepEqual(got, want) {
		t.Errorf("ProcessMedia params = %+v, want %+v", got, want)
	}
}

// A Radarr Download event must start real work the same way.
func TestRadarrDownloadStartsProcessMedia(t *testing.T) {
	processor := newRecordingProcessor()

	rec := serveWebhook(t, NewWebhookHandler(processor).HandleRadarr, "/api/v1/webhook/radarr", RadarrPayload{
		EventType: WebhookEventDownload,
		Movie: MovieInfo{
			ID:     99,
			Title:  "Test Movie",
			Year:   2024,
			ImdbID: "tt1234567",
			TMDBID: 12345,
		},
		MovieFile: MovieFile{Path: "/movies/Test Movie (2024)/Movie.mkv"},
	})

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body %s", rec.Code, rec.Body)
	}

	want := subtitle.MediaParams{
		Path:         "/movies/Test Movie (2024)/Movie.mkv",
		MediaType:    subtitle.MediaTypeMovie,
		Title:        "Test Movie (2024)",
		RadarrID:     99,
		SourceSystem: subtitle.SourceSystemRadarr,
		MediaID:      "99",
		ExternalIDs: map[string]string{
			subtitle.ExternalIDRadarr: "99",
			subtitle.ExternalIDTMDB:   "12345",
			subtitle.ExternalIDIMDB:   "tt1234567",
		},
	}
	if got := processor.waitMediaCall(t); !reflect.DeepEqual(got, want) {
		t.Errorf("ProcessMedia params = %+v, want %+v", got, want)
	}
}

// An event the handler ignores must not start work.
func TestSonarrIgnoredEventStartsNoProcessMedia(t *testing.T) {
	processor := newRecordingProcessor()

	rec := serveWebhook(t, NewWebhookHandler(processor).HandleSonarr, "/api/v1/webhook/sonarr", SonarrPayload{
		EventType:   WebhookEventTest,
		Series:      SeriesInfo{Title: "Test"},
		Episodes:    []EpisodeInfo{{EpisodeNumber: 1, SeasonNumber: 1}},
		EpisodeFile: EpisodeFile{Path: "/tv/test.mkv"},
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body)
	}
	processor.assertNoMediaCall(t)
}
