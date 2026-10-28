package handler

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

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

// webhookLog is the handler-level logger; request_id comes from the request context.
var webhookLog = logger.Component("webhook")

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
	ctx := c.Request.Context()

	var payload SonarrPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		webhookLog.Warn(ctx, "webhook_invalid", "Invalid Sonarr payload", zap.String("source", "sonarr"), zap.String("reason", "invalid_payload"), zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid payload"})
		return
	}

	// Validate event type
	if !shouldProcessEvent(payload.EventType) {
		webhookLog.Info(ctx, "webhook_ignored", "Event type ignored", zap.String("source", "sonarr"), zap.String("webhook_event", payload.EventType))
		c.JSON(http.StatusOK, gin.H{"message": "Event type ignored"})
		return
	}

	// Validate required fields
	if payload.EpisodeFile.Path == "" {
		webhookLog.Warn(ctx, "webhook_invalid", "Missing episode file path in Sonarr payload", zap.String("source", "sonarr"), zap.String("reason", "missing_path"))
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing episodeFile.path"})
		return
	}

	// Build media title
	mediaTitle := formatEpisodeTitle(payload.Series.Title, payload.Episodes)
	params := sonarrMediaParams(payload)

	webhookLog.Info(ctx, "webhook_received", "Sonarr webhook received",
		zap.String("source", "sonarr"),
		zap.String("media_title", mediaTitle),
		zap.Int("season", getSeasonNumber(payload.Episodes)),
		zap.Int("episode", getEpisodeNumber(payload.Episodes)))
	webhookLog.Debug(ctx, "webhook_detail", "Webhook detail", logger.Preview(payload.EpisodeFile.Path))

	// Process subtitle in background (non-blocking)
	if h.subtitleService != nil {
		go func(params subtitle.MediaParams) {
			// ProcessMedia is the operation boundary and reports its own
			// ERROR exactly once; this goroutine only owns the trigger. The
			// request's correlation values (request_id) survive, but its
			// cancellation does not: the 202 response must not cancel the job.
			_ = h.subtitleService.ProcessMedia(context.WithoutCancel(ctx), params)
		}(params)
	}

	c.JSON(http.StatusAccepted, gin.H{
		"message": "Processing started",
	})
}

// HandleRadarr processes Radarr webhook requests.
func (h *WebhookHandler) HandleRadarr(c *gin.Context) {
	ctx := c.Request.Context()

	var payload RadarrPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		webhookLog.Warn(ctx, "webhook_invalid", "Invalid Radarr payload", zap.String("source", "radarr"), zap.String("reason", "invalid_payload"), zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid payload"})
		return
	}

	// Validate event type
	if !shouldProcessEvent(payload.EventType) {
		webhookLog.Info(ctx, "webhook_ignored", "Event type ignored", zap.String("source", "radarr"), zap.String("webhook_event", payload.EventType))
		c.JSON(http.StatusOK, gin.H{"message": "Event type ignored"})
		return
	}

	// Validate required fields
	if payload.MovieFile.Path == "" {
		webhookLog.Warn(ctx, "webhook_invalid", "Missing movie file path in Radarr payload", zap.String("source", "radarr"), zap.String("reason", "missing_path"))
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing movieFile.path"})
		return
	}

	// Build media title
	mediaTitle := formatMovieTitle(payload.Movie.Title, payload.Movie.Year)
	params := radarrMediaParams(payload)

	webhookLog.Info(ctx, "webhook_received", "Radarr webhook received",
		zap.String("source", "radarr"),
		zap.String("media_title", mediaTitle),
		zap.Int("year", payload.Movie.Year))
	webhookLog.Debug(ctx, "webhook_detail", "Webhook detail", logger.Preview(payload.MovieFile.Path))

	// Process subtitle in background (non-blocking)
	if h.subtitleService != nil {
		go func(params subtitle.MediaParams) {
			// ProcessMedia is the operation boundary and reports its own
			// ERROR exactly once; this goroutine only owns the trigger. The
			// request's correlation values (request_id) survive, but its
			// cancellation does not: the 202 response must not cancel the job.
			_ = h.subtitleService.ProcessMedia(context.WithoutCancel(ctx), params)
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
