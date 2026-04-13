package bazarr

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/fusionn/pkg/logger"
)

func TestMain(m *testing.M) {
	logger.Init(true)
	os.Exit(m.Run())
}

func TestSearchEpisodeSubtitle(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.Method != http.MethodPatch {
			t.Errorf("Expected PATCH, got %s", r.Method)
		}
		if r.URL.Path != "/api/episodes/subtitles" {
			t.Errorf("Expected /api/episodes/subtitles, got %s", r.URL.Path)
		}
		if r.Header.Get("X-API-KEY") != "test-key" {
			t.Errorf("Expected API key 'test-key', got %s", r.Header.Get("X-API-KEY"))
		}
		q := r.URL.Query()
		if q.Get("seriesid") != "42" {
			t.Errorf("Expected seriesid=42, got %s", q.Get("seriesid"))
		}
		if q.Get("episodeid") != "101" {
			t.Errorf("Expected episodeid=101, got %s", q.Get("episodeid"))
		}
		if q.Get("language") != "zh" {
			t.Errorf("Expected language=zh, got %s", q.Get("language"))
		}
		if q.Get("hi") != "False" {
			t.Errorf("Expected hi=False, got %s", q.Get("hi"))
		}
		if q.Get("forced") != "False" {
			t.Errorf("Expected forced=False, got %s", q.Get("forced"))
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key", 30)
	err := client.SearchEpisodeSubtitle(context.Background(), 42, 101, "zh")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !called {
		t.Error("Server was not called")
	}
}

func TestSearchMovieSubtitle(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.Method != http.MethodPatch {
			t.Errorf("Expected PATCH, got %s", r.Method)
		}
		if r.URL.Path != "/api/movies/subtitles" {
			t.Errorf("Expected /api/movies/subtitles, got %s", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("radarrid") != "99" {
			t.Errorf("Expected radarrid=99, got %s", q.Get("radarrid"))
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key", 30)
	err := client.SearchMovieSubtitle(context.Background(), 99, "zh")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if !called {
		t.Error("Server was not called")
	}
}

func TestGetEpisodeSubtitles(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("Expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/episodes" {
			t.Errorf("Expected /api/episodes, got %s", r.URL.Path)
		}
		if r.URL.Query().Get("episodeid[]") != "101" {
			t.Errorf("Expected episodeid[]=101, got %s", r.URL.Query().Get("episodeid[]"))
		}
		resp := EpisodeListResponse{
			Data: []EpisodeData{
				{
					SonarrEpisodeID: 101,
					Subtitles: []SubtitleInfo{
						{Path: "/tv/Show/S01E01.zh.srt", Code2: "zh"},
					},
					MissingSubtitles: []MissingLanguage{},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key", 30)
	info, err := client.GetEpisodeSubtitles(context.Background(), 101)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(info.Subtitles) != 1 {
		t.Fatalf("Expected 1 subtitle, got %d", len(info.Subtitles))
	}
	if info.Subtitles[0].Code2 != "zh" {
		t.Errorf("Expected code2=zh, got %s", info.Subtitles[0].Code2)
	}
}

func TestGetMovieSubtitles(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("radarrid[]") != "99" {
			t.Errorf("Expected radarrid[]=99, got %s", r.URL.Query().Get("radarrid[]"))
		}
		resp := MovieListResponse{
			Data: []MovieData{
				{
					RadarrID: 99,
					Subtitles: []SubtitleInfo{
						{Path: "/movies/Movie/Movie.zh.srt", Code2: "zh"},
					},
					MissingSubtitles: []MissingLanguage{},
				},
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key", 30)
	info, err := client.GetMovieSubtitles(context.Background(), 99)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if len(info.Subtitles) != 1 {
		t.Fatalf("Expected 1 subtitle, got %d", len(info.Subtitles))
	}
}

func TestSearchEpisodeSubtitle_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key", 30)
	err := client.SearchEpisodeSubtitle(context.Background(), 42, 101, "zh")
	if err == nil {
		t.Error("Expected error for 500 response")
	}
}

func TestSearchEpisodeSubtitle_Unreachable(t *testing.T) {
	client := NewClient("http://127.0.0.1:1", "test-key", 2)
	err := client.SearchEpisodeSubtitle(context.Background(), 42, 101, "zh")
	if err == nil {
		t.Error("Expected error for unreachable server")
	}
}

func TestGetEpisodeSubtitles_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := EpisodeListResponse{Data: []EpisodeData{}}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key", 30)
	_, err := client.GetEpisodeSubtitles(context.Background(), 999)
	if err == nil {
		t.Error("Expected error for empty response")
	}
}
