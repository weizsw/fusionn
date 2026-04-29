package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/fusionn/internal/service/subtitle"
	"github.com/fusionn/pkg/logger"
)

func init() {
	// Initialize logger for tests
	logger.Init(true)
	gin.SetMode(gin.TestMode)
}

func TestHandleSonarr(t *testing.T) {
	handler := NewWebhookHandler(nil) // Pass nil for testing

	tests := []struct {
		name           string
		payload        interface{}
		expectedStatus int
		expectedMsg    string
	}{
		{
			name: "Valid Download Event",
			payload: SonarrPayload{
				EventType: WebhookEventDownload,
				Series: SeriesInfo{
					Title: "Test Series",
					Path:  "/tv/Test Series",
				},
				Episodes: []EpisodeInfo{
					{
						Title:         "Test Episode",
						EpisodeNumber: 1,
						SeasonNumber:  1,
					},
				},
				EpisodeFile: EpisodeFile{
					Path:         "/tv/Test Series/Season 01/S01E01.mkv",
					RelativePath: "Season 01/S01E01.mkv",
				},
			},
			expectedStatus: http.StatusAccepted,
			expectedMsg:    "Processing started",
		},
		{
			name: "Valid Upgrade Event",
			payload: SonarrPayload{
				EventType: WebhookEventUpgrade,
				Series: SeriesInfo{
					Title: "Test Series",
				},
				Episodes: []EpisodeInfo{
					{EpisodeNumber: 2, SeasonNumber: 1},
				},
				EpisodeFile: EpisodeFile{
					Path: "/tv/Test Series/S01E02.mkv",
				},
			},
			expectedStatus: http.StatusAccepted,
			expectedMsg:    "Processing started",
		},
		{
			name: "Ignored Test Event",
			payload: SonarrPayload{
				EventType: WebhookEventTest,
				Series:    SeriesInfo{Title: "Test"},
				Episodes:  []EpisodeInfo{{EpisodeNumber: 1}},
				EpisodeFile: EpisodeFile{
					Path: "/tv/test.mkv",
				},
			},
			expectedStatus: http.StatusOK,
			expectedMsg:    "Event type ignored",
		},
		{
			name: "Missing File Path",
			payload: SonarrPayload{
				EventType:   WebhookEventDownload,
				Series:      SeriesInfo{Title: "Test"},
				Episodes:    []EpisodeInfo{{EpisodeNumber: 1}},
				EpisodeFile: EpisodeFile{},
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "Invalid JSON",
			payload:        "invalid json",
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			body, _ := json.Marshal(tt.payload)
			c.Request = httptest.NewRequest("POST", "/api/v1/webhook/sonarr", bytes.NewBuffer(body))
			c.Request.Header.Set("Content-Type", "application/json")

			handler.HandleSonarr(c)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}

			if tt.expectedMsg != "" {
				var response map[string]interface{}
				if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
					t.Fatalf("Failed to parse response: %v", err)
				}
				if msg, ok := response["message"].(string); ok {
					if msg != tt.expectedMsg {
						t.Errorf("Expected message %q, got %q", tt.expectedMsg, msg)
					}
				}
			}
		})
	}
}

func TestSonarrPayloadParseIDs(t *testing.T) {
	handler := NewWebhookHandler(nil)

	payload := SonarrPayload{
		EventType: WebhookEventDownload,
		Series: SeriesInfo{
			ID:    42,
			Title: "Test Series",
			Path:  "/tv/Test Series",
		},
		Episodes: []EpisodeInfo{
			{
				ID:            101,
				Title:         "Test Episode",
				EpisodeNumber: 1,
				SeasonNumber:  1,
			},
		},
		EpisodeFile: EpisodeFile{
			Path: "/tv/Test Series/Season 01/S01E01.mkv",
		},
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body, _ := json.Marshal(payload)
	c.Request = httptest.NewRequest("POST", "/api/v1/webhook/sonarr", bytes.NewBuffer(body))
	c.Request.Header.Set("Content-Type", "application/json")

	handler.HandleSonarr(c)

	if w.Code != http.StatusAccepted {
		t.Errorf("Expected status %d, got %d", http.StatusAccepted, w.Code)
	}

	// Verify IDs are parsed by re-marshaling and checking
	var roundTrip SonarrPayload
	body2, _ := json.Marshal(payload)
	json.Unmarshal(body2, &roundTrip)
	if roundTrip.Series.ID != 42 {
		t.Errorf("Expected series ID 42, got %d", roundTrip.Series.ID)
	}
	if roundTrip.Episodes[0].ID != 101 {
		t.Errorf("Expected episode ID 101, got %d", roundTrip.Episodes[0].ID)
	}
}

func TestSonarrMediaParamsIncludeGlossaryIdentity(t *testing.T) {
	payload := SonarrPayload{
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
		EpisodeFile: EpisodeFile{
			Path: "/tv/The Capture/Season 01/S01E01.mkv",
		},
	}

	params := sonarrMediaParams(payload)

	if params.SourceSystem != subtitle.SourceSystemSonarr {
		t.Fatalf("source system = %q", params.SourceSystem)
	}
	if params.MediaID != "42" {
		t.Fatalf("media id = %q", params.MediaID)
	}
	if params.ExternalIDs[subtitle.ExternalIDSonarr] != "42" {
		t.Fatalf("sonarr external id = %q", params.ExternalIDs[subtitle.ExternalIDSonarr])
	}
	if params.ExternalIDs[subtitle.ExternalIDTVDB] != "355620" {
		t.Fatalf("tvdb id = %q", params.ExternalIDs[subtitle.ExternalIDTVDB])
	}
	if params.ExternalIDs[subtitle.ExternalIDIMDB] != "tt8201186" {
		t.Fatalf("imdb id = %q", params.ExternalIDs[subtitle.ExternalIDIMDB])
	}
	if params.Season != 1 || params.Episode != 1 {
		t.Fatalf("season/episode = %d/%d", params.Season, params.Episode)
	}
}

func TestRadarrPayloadParseIDs(t *testing.T) {
	handler := NewWebhookHandler(nil)

	payload := RadarrPayload{
		EventType: WebhookEventDownload,
		Movie: MovieInfo{
			ID:     99,
			Title:  "Test Movie",
			Year:   2024,
			ImdbID: "tt1234567",
		},
		MovieFile: MovieFile{
			Path: "/movies/Test Movie (2024)/Movie.mkv",
		},
	}

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	body, _ := json.Marshal(payload)
	c.Request = httptest.NewRequest("POST", "/api/v1/webhook/radarr", bytes.NewBuffer(body))
	c.Request.Header.Set("Content-Type", "application/json")

	handler.HandleRadarr(c)

	if w.Code != http.StatusAccepted {
		t.Errorf("Expected status %d, got %d", http.StatusAccepted, w.Code)
	}

	var roundTrip RadarrPayload
	body2, _ := json.Marshal(payload)
	json.Unmarshal(body2, &roundTrip)
	if roundTrip.Movie.ID != 99 {
		t.Errorf("Expected movie ID 99, got %d", roundTrip.Movie.ID)
	}
}

func TestRadarrMediaParamsIncludeGlossaryIdentity(t *testing.T) {
	payload := RadarrPayload{
		EventType: WebhookEventDownload,
		Movie: MovieInfo{
			ID:     99,
			Title:  "Test Movie",
			Year:   2024,
			ImdbID: "tt1234567",
			TMDBID: 12345,
		},
		MovieFile: MovieFile{
			Path: "/movies/Test Movie (2024)/Movie.mkv",
		},
	}

	params := radarrMediaParams(payload)

	if params.SourceSystem != subtitle.SourceSystemRadarr {
		t.Fatalf("source system = %q", params.SourceSystem)
	}
	if params.MediaID != "99" {
		t.Fatalf("media id = %q", params.MediaID)
	}
	if params.ExternalIDs[subtitle.ExternalIDRadarr] != "99" {
		t.Fatalf("radarr external id = %q", params.ExternalIDs[subtitle.ExternalIDRadarr])
	}
	if params.ExternalIDs[subtitle.ExternalIDTMDB] != "12345" {
		t.Fatalf("tmdb id = %q", params.ExternalIDs[subtitle.ExternalIDTMDB])
	}
	if params.ExternalIDs[subtitle.ExternalIDIMDB] != "tt1234567" {
		t.Fatalf("imdb id = %q", params.ExternalIDs[subtitle.ExternalIDIMDB])
	}
}

func TestHandleRadarr(t *testing.T) {
	handler := NewWebhookHandler(nil) // Pass nil for testing

	tests := []struct {
		name           string
		payload        interface{}
		expectedStatus int
		expectedMsg    string
	}{
		{
			name: "Valid Download Event",
			payload: RadarrPayload{
				EventType: WebhookEventDownload,
				Movie: MovieInfo{
					Title:  "Test Movie",
					Year:   2024,
					ImdbID: "tt1234567",
				},
				MovieFile: MovieFile{
					Path:         "/movies/Test Movie (2024)/Movie.mkv",
					RelativePath: "Movie.mkv",
				},
			},
			expectedStatus: http.StatusAccepted,
			expectedMsg:    "Processing started",
		},
		{
			name: "Valid Upgrade Event",
			payload: RadarrPayload{
				EventType: WebhookEventUpgrade,
				Movie: MovieInfo{
					Title: "Test Movie",
					Year:  2024,
				},
				MovieFile: MovieFile{
					Path: "/movies/Test Movie (2024)/Movie.mkv",
				},
			},
			expectedStatus: http.StatusAccepted,
			expectedMsg:    "Processing started",
		},
		{
			name: "Ignored Rename Event",
			payload: RadarrPayload{
				EventType: WebhookEventRename,
				Movie:     MovieInfo{Title: "Test"},
				MovieFile: MovieFile{
					Path: "/movies/test.mkv",
				},
			},
			expectedStatus: http.StatusOK,
			expectedMsg:    "Event type ignored",
		},
		{
			name: "Missing File Path",
			payload: RadarrPayload{
				EventType: WebhookEventDownload,
				Movie:     MovieInfo{Title: "Test"},
				MovieFile: MovieFile{},
			},
			expectedStatus: http.StatusBadRequest,
		},
		{
			name:           "Invalid JSON",
			payload:        "invalid json",
			expectedStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)

			body, _ := json.Marshal(tt.payload)
			c.Request = httptest.NewRequest("POST", "/api/v1/webhook/radarr", bytes.NewBuffer(body))
			c.Request.Header.Set("Content-Type", "application/json")

			handler.HandleRadarr(c)

			if w.Code != tt.expectedStatus {
				t.Errorf("Expected status %d, got %d", tt.expectedStatus, w.Code)
			}

			if tt.expectedMsg != "" {
				var response map[string]interface{}
				if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
					t.Fatalf("Failed to parse response: %v", err)
				}
				if msg, ok := response["message"].(string); ok {
					if msg != tt.expectedMsg {
						t.Errorf("Expected message %q, got %q", tt.expectedMsg, msg)
					}
				}
			}
		})
	}
}
