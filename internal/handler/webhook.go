package handler

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/fusionn/internal/service/subtitle"
	"github.com/fusionn/pkg/logger"
)

// WebhookHandler handles Sonarr and Radarr webhooks.
type WebhookHandler struct {
	subtitleService *subtitle.Service
}

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
	ID    int    `json:"id"`
	Title string `json:"title"`
	Path  string `json:"path"`
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
	if payload.EventType != "Download" && payload.EventType != "Upgrade" {
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

	// Process subtitle in background (non-blocking)
	if h.subtitleService != nil {
		go func() {
			// Use background context since this runs after HTTP response
			ctx := context.Background()
			if err := h.subtitleService.ProcessMedia(
				ctx,
				payload.EpisodeFile.Path,
				"episode",
				mediaTitle,
			); err != nil {
				logger.Errorf("Subtitle processing failed for %s: %v", mediaTitle, err)
			}
		}()
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
	if payload.EventType != "Download" && payload.EventType != "Upgrade" {
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

	// Process subtitle in background (non-blocking)
	if h.subtitleService != nil {
		go func() {
			// Use background context since this runs after HTTP response
			ctx := context.Background()
			if err := h.subtitleService.ProcessMedia(
				ctx,
				payload.MovieFile.Path,
				"movie",
				mediaTitle,
			); err != nil {
				logger.Errorf("Subtitle processing failed for %s: %v", mediaTitle, err)
			}
		}()
	}

	c.JSON(http.StatusAccepted, gin.H{
		"message": "Processing started",
	})
}

// Helper functions
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
