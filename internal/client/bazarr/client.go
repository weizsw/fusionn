package bazarr

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/fusionn/pkg/logger"
)

type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

const (
	querySeriesID  = "seriesid"
	queryEpisodeID = "episodeid"
	queryRadarrID  = "radarrid"
	queryLanguage  = "language"
	queryHI        = "hi"
	queryForced    = "forced"

	bazarrFalse = "False"
)

type SubtitleInfo struct {
	Path  string `json:"path"`
	Code2 string `json:"code2"`
	Name  string `json:"name"`
}

type MissingLanguage struct {
	Code2 string `json:"code2"`
	Name  string `json:"name"`
}

type EpisodeData struct {
	SonarrEpisodeID  int               `json:"sonarrEpisodeId"`
	SonarrSeriesID   int               `json:"sonarrSeriesId"`
	Path             string            `json:"path"`
	Subtitles        []SubtitleInfo    `json:"subtitles"`
	MissingSubtitles []MissingLanguage `json:"missing_subtitles"`
}

type EpisodeListResponse struct {
	Data []EpisodeData `json:"data"`
}

type MovieData struct {
	RadarrID         int               `json:"radarrId"`
	Path             string            `json:"path"`
	Subtitles        []SubtitleInfo    `json:"subtitles"`
	MissingSubtitles []MissingLanguage `json:"missing_subtitles"`
}

type MovieListResponse struct {
	Data  []MovieData `json:"data"`
	Total int         `json:"total"`
}

func NewClient(baseURL, apiKey string, timeoutSeconds int) *Client {
	if timeoutSeconds <= 0 {
		timeoutSeconds = 180
	}
	return &Client{
		baseURL: baseURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: time.Duration(timeoutSeconds) * time.Second,
		},
	}
}

func (c *Client) SearchEpisodeSubtitle(ctx context.Context, seriesID, episodeID int, language string) error {
	params := url.Values{}
	params.Set(querySeriesID, strconv.Itoa(seriesID))
	params.Set(queryEpisodeID, strconv.Itoa(episodeID))
	params.Set(queryLanguage, language)
	params.Set(queryHI, bazarrFalse)
	params.Set(queryForced, bazarrFalse)

	reqURL := c.baseURL + "/api/episodes/subtitles?" + params.Encode()
	logger.Debugf("Bazarr PATCH request: %s", reqURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, reqURL, http.NoBody)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("X-API-KEY", c.apiKey)

	start := time.Now()
	resp, err := c.httpClient.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		return fmt.Errorf("bazarr episode search failed (took %s): %w", elapsed, err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	logger.Debugf("Bazarr PATCH response: status=%d, took=%s, body=%s", resp.StatusCode, elapsed, string(body))

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bazarr episode search returned status %d (took %s): %s", resp.StatusCode, elapsed, string(body))
	}
	return nil
}

func (c *Client) SearchMovieSubtitle(ctx context.Context, radarrID int, language string) error {
	params := url.Values{}
	params.Set(queryRadarrID, strconv.Itoa(radarrID))
	params.Set(queryLanguage, language)
	params.Set(queryHI, bazarrFalse)
	params.Set(queryForced, bazarrFalse)

	reqURL := c.baseURL + "/api/movies/subtitles?" + params.Encode()
	logger.Debugf("Bazarr PATCH request: %s", reqURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, reqURL, http.NoBody)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("X-API-KEY", c.apiKey)

	start := time.Now()
	resp, err := c.httpClient.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		return fmt.Errorf("bazarr movie search failed (took %s): %w", elapsed, err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	logger.Debugf("Bazarr PATCH response: status=%d, took=%s, body=%s", resp.StatusCode, elapsed, string(body))

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("bazarr movie search returned status %d (took %s): %s", resp.StatusCode, elapsed, string(body))
	}
	return nil
}

func (c *Client) GetEpisodeSubtitles(ctx context.Context, episodeID int) (*EpisodeData, error) {
	reqURL := fmt.Sprintf("%s/api/episodes?episodeid[]=%d", c.baseURL, episodeID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("X-API-KEY", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bazarr get episode failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("bazarr get episode returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read episode response: %w", err)
	}
	logger.Debugf("Bazarr GET episodes response (episodeId=%d): %s", episodeID, string(body))

	var listResp EpisodeListResponse
	if err := json.Unmarshal(body, &listResp); err != nil {
		return nil, fmt.Errorf("decode episode response: %w", err)
	}

	if len(listResp.Data) == 0 {
		return nil, fmt.Errorf("episode %d not found in bazarr", episodeID)
	}
	return &listResp.Data[0], nil
}

func (c *Client) GetMovieSubtitles(ctx context.Context, radarrID int) (*MovieData, error) {
	reqURL := fmt.Sprintf("%s/api/movies?radarrid[]=%d", c.baseURL, radarrID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("X-API-KEY", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("bazarr get movie failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("bazarr get movie returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read movie response: %w", err)
	}
	logger.Debugf("Bazarr GET movies response (radarrId=%d): %s", radarrID, string(body))

	var listResp MovieListResponse
	if err := json.Unmarshal(body, &listResp); err != nil {
		return nil, fmt.Errorf("decode movie response: %w", err)
	}

	if len(listResp.Data) == 0 {
		return nil, fmt.Errorf("movie (radarr_id=%d) not found in bazarr", radarrID)
	}
	return &listResp.Data[0], nil
}
