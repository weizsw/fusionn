package handler

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/fusionn/internal/service/subtitle"
	"github.com/fusionn/pkg/logger"
)

// WebhookHandler handles Sonarr and Radarr webhooks.
type WebhookHandler struct {
	subtitleService *subtitle.Service
}

const (
	WebhookEventDownload = "Download"
	WebhookEventUpgrade  = "Upgrade"
	WebhookEventTest     = "Test"
	WebhookEventGrab     = "Grab"
	WebhookEventRename   = "Rename"
)

// NewWebhookHandler creates a new webhook handler.
func NewWebhookHandler(subtitleService *subtitle.Service) *WebhookHandler {
	return &WebhookHandler{
		subtitleService: subtitleService,
	}
}

// SonarrPayload represents the Sonarr webhook payload.
type SonarrPayload struct {
	EventType   string        `json:"eventType"`
	Series      SeriesInfo    `json:"series"`
	Episodes    []EpisodeInfo `json:"episodes"`
	EpisodeFile EpisodeFile   `json:"episodeFile"`
}

// SeriesInfo contains series metadata.
type SeriesInfo struct {
	ID     int    `json:"id"`
	Title  string `json:"title"`
	Path   string `json:"path"`
	TVDBID int    `json:"tvdbId"`
	ImdbID string `json:"imdbId"`
}

// EpisodeInfo contains episode metadata.
type EpisodeInfo struct {
	ID            int    `json:"id"`
	Title         string `json:"title"`
	EpisodeNumber int    `json:"episodeNumber"`
	SeasonNumber  int    `json:"seasonNumber"`
}

// EpisodeFile contains episode file information.
type EpisodeFile struct {
	Path         string `json:"path"`
	RelativePath string `json:"relativePath"`
}

// RadarrPayload represents the Radarr webhook payload.
type RadarrPayload struct {
	EventType string    `json:"eventType"`
	Movie     MovieInfo `json:"movie"`
	MovieFile MovieFile `json:"movieFile"`
}

// MovieInfo contains movie metadata.
type MovieInfo struct {
	ID     int    `json:"id"`
	Title  string `json:"title"`
	Year   int    `json:"year"`
	ImdbID string `json:"imdbId"`
	TMDBID int    `json:"tmdbId"`
}

// MovieFile contains movie file information.
type MovieFile struct {
	Path         string `json:"path"`
	RelativePath string `json:"relativePath"`
}

// HandleSonarr processes Sonarr webhook requests.
func (h *WebhookHandler) HandleSonarr(c *gin.Context) {
	var payload SonarrPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		logger.Errorf("Invalid Sonarr payload: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid payload"})
		return
	}

	// Validate event type
	if !shouldProcessEvent(payload.EventType) {
		logger.Infof("Ignoring Sonarr event type: %s", payload.EventType)
		c.JSON(http.StatusOK, gin.H{"message": "Event type ignored"})
		return
	}

	// Validate required fields
	if payload.EpisodeFile.Path == "" {
		logger.Error("Missing episodeFile.path in Sonarr payload")
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing episodeFile.path"})
		return
	}

	logger.Infof("📺 Sonarr webhook received: %s S%02dE%02d",
		payload.Series.Title,
		getSeasonNumber(payload.Episodes),
		getEpisodeNumber(payload.Episodes))

	logger.Debugf("File path: %s", payload.EpisodeFile.Path)

	// Build media title
	mediaTitle := formatEpisodeTitle(payload.Series.Title, payload.Episodes)
	params := sonarrMediaParams(payload)

	// Process subtitle in background (non-blocking)
	if h.subtitleService != nil {
		go func(params subtitle.MediaParams) {
			ctx := context.Background()
			if err := h.subtitleService.ProcessMedia(ctx, params); err != nil {
				logger.Errorf("Subtitle processing failed for %s: %v", mediaTitle, err)
			}
		}(params)
	}

	c.JSON(http.StatusAccepted, gin.H{
		"message": "Processing started",
	})
}

// HandleRadarr processes Radarr webhook requests.
func (h *WebhookHandler) HandleRadarr(c *gin.Context) {
	var payload RadarrPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		logger.Errorf("Invalid Radarr payload: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid payload"})
		return
	}

	// Validate event type
	if !shouldProcessEvent(payload.EventType) {
		logger.Infof("Ignoring Radarr event type: %s", payload.EventType)
		c.JSON(http.StatusOK, gin.H{"message": "Event type ignored"})
		return
	}

	// Validate required fields
	if payload.MovieFile.Path == "" {
		logger.Error("Missing movieFile.path in Radarr payload")
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing movieFile.path"})
		return
	}

	logger.Infof("🎬 Radarr webhook received: %s (%d)",
		payload.Movie.Title,
		payload.Movie.Year)

	logger.Debugf("File path: %s", payload.MovieFile.Path)

	// Build media title
	mediaTitle := formatMovieTitle(payload.Movie.Title, payload.Movie.Year)
	params := radarrMediaParams(payload)

	// Process subtitle in background (non-blocking)
	if h.subtitleService != nil {
		go func(params subtitle.MediaParams) {
			ctx := context.Background()
			if err := h.subtitleService.ProcessMedia(ctx, params); err != nil {
				logger.Errorf("Subtitle processing failed for %s: %v", mediaTitle, err)
			}
		}(params)
	}

	c.JSON(http.StatusAccepted, gin.H{
		"message": "Processing started",
	})
}

// Helper functions
func shouldProcessEvent(eventType string) bool {
	return eventType == WebhookEventDownload || eventType == WebhookEventUpgrade
}

func getSeasonNumber(episodes []EpisodeInfo) int {
	if len(episodes) > 0 {
		return episodes[0].SeasonNumber
	}
	return 0
}

func getEpisodeNumber(episodes []EpisodeInfo) int {
	if len(episodes) > 0 {
		return episodes[0].EpisodeNumber
	}
	return 0
}

func sonarrMediaParams(payload SonarrPayload) subtitle.MediaParams {
	episodeID := 0
	if len(payload.Episodes) > 0 {
		episodeID = payload.Episodes[0].ID
	}

	mediaID := idString(payload.Series.ID)
	return subtitle.MediaParams{
		Path:            payload.EpisodeFile.Path,
		MediaType:       subtitle.MediaTypeEpisode,
		Title:           formatEpisodeTitle(payload.Series.Title, payload.Episodes),
		SonarrSeriesID:  payload.Series.ID,
		SonarrEpisodeID: episodeID,
		SourceSystem:    subtitle.SourceSystemSonarr,
		MediaID:         mediaID,
		ExternalIDs: compactExternalIDs(map[string]string{
			subtitle.ExternalIDSonarr: mediaID,
			subtitle.ExternalIDTVDB:   idString(payload.Series.TVDBID),
			subtitle.ExternalIDIMDB:   payload.Series.ImdbID,
		}),
		Season:  getSeasonNumber(payload.Episodes),
		Episode: getEpisodeNumber(payload.Episodes),
	}
}

func radarrMediaParams(payload RadarrPayload) subtitle.MediaParams {
	mediaID := idString(payload.Movie.ID)
	return subtitle.MediaParams{
		Path:         payload.MovieFile.Path,
		MediaType:    subtitle.MediaTypeMovie,
		Title:        formatMovieTitle(payload.Movie.Title, payload.Movie.Year),
		RadarrID:     payload.Movie.ID,
		SourceSystem: subtitle.SourceSystemRadarr,
		MediaID:      mediaID,
		ExternalIDs: compactExternalIDs(map[string]string{
			subtitle.ExternalIDRadarr: mediaID,
			subtitle.ExternalIDTMDB:   idString(payload.Movie.TMDBID),
			subtitle.ExternalIDIMDB:   payload.Movie.ImdbID,
		}),
	}
}

func idString(id int) string {
	if id <= 0 {
		return ""
	}
	return strconv.Itoa(id)
}

func compactExternalIDs(ids map[string]string) map[string]string {
	compacted := make(map[string]string, len(ids))
	for key, value := range ids {
		value = strings.TrimSpace(value)
		if value != "" {
			compacted[key] = value
		}
	}
	if len(compacted) == 0 {
		return nil
	}
	return compacted
}

func formatEpisodeTitle(seriesTitle string, episodes []EpisodeInfo) string {
	if len(episodes) > 0 {
		return fmt.Sprintf("%s S%02dE%02d",
			seriesTitle,
			episodes[0].SeasonNumber,
			episodes[0].EpisodeNumber)
	}
	return seriesTitle
}

func formatMovieTitle(title string, year int) string {
	if year > 0 {
		return fmt.Sprintf("%s (%d)", title, year)
	}
	return title
}
