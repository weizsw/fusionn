package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

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
				EventType: "Download",
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
				EventType: "Upgrade",
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
				EventType: "Test",
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
				EventType:   "Download",
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
				EventType: "Download",
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
				EventType: "Upgrade",
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
				EventType: "Rename",
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
				EventType: "Download",
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
