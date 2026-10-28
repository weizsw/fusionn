package duosubs

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/fusionn/pkg/logger"
)

// HTTPClient calls a remote DuoSubs HTTP service
type HTTPClient struct {
	baseURL         string
	containerPrefix string
	hostPrefix      string
	timeout         time.Duration
	client          *http.Client
}

// HTTPConfig configures the HTTP client
type HTTPConfig struct {
	BaseURL         string
	ContainerPrefix string
	HostPrefix      string
	Timeout         time.Duration
}

// MergeRequest is the HTTP request payload
type mergeRequest struct {
	PrimaryPath     string `json:"primary_path"`
	SecondaryPath   string `json:"secondary_path"`
	OutputDir       string `json:"output_dir"`
	ContainerPrefix string `json:"container_prefix"`
	HostPrefix      string `json:"host_prefix"`
}

// MergeResponse is the HTTP response payload
type mergeResponse struct {
	Success    bool   `json:"success"`
	OutputPath string `json:"output_path,omitempty"`
	Error      string `json:"error,omitempty"`
}

// NewHTTPClient creates a new HTTP client for DuoSubs service
func NewHTTPClient(cfg HTTPConfig) *HTTPClient {
	if cfg.Timeout == 0 {
		cfg.Timeout = 15 * time.Minute // Default 15 min for ML processing
	}

	return &HTTPClient{
		baseURL:         cfg.BaseURL,
		containerPrefix: cfg.ContainerPrefix,
		hostPrefix:      cfg.HostPrefix,
		timeout:         cfg.Timeout,
		client: &http.Client{
			Timeout: cfg.Timeout,
		},
	}
}

// Merge merges subtitles via HTTP service
func (c *HTTPClient) Merge(ctx context.Context, primaryPath, secondaryPath, outputDir string) (string, error) {
	req := mergeRequest{
		PrimaryPath:     primaryPath,
		SecondaryPath:   secondaryPath,
		OutputDir:       outputDir,
		ContainerPrefix: c.containerPrefix,
		HostPrefix:      c.hostPrefix,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/merge", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	// One outbound request id ties this call to the Python service's own
	// request_completed line; the shared logger renders it with the ctx job_id.
	requestID := logger.NewRequestID()
	ctx = logger.WithRequestID(ctx, requestID)
	httpReq = httpReq.WithContext(ctx)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("X-Request-ID", requestID)
	logger.Component("duosubs").Info(ctx, "service_request", "Calling DuoSubs service", zap.String("tool", "duosubs"))

	resp, err := c.client.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		// The remote body is untrusted content and never becomes part of the
		// error; it appears once as a bounded, redacted DEBUG excerpt.
		logger.Component("duosubs").Debug(ctx, "http_error_detail", "DuoSubs HTTP error detail",
			zap.Int("status", resp.StatusCode), logger.Preview(string(respBody)))
		return "", fmt.Errorf("http error %d", resp.StatusCode)
	}

	var mergeResp mergeResponse
	if err := json.Unmarshal(respBody, &mergeResp); err != nil {
		return "", fmt.Errorf("unmarshal response: %w", err)
	}

	if !mergeResp.Success {
		logger.Component("duosubs").Debug(ctx, "merge_error_detail", "DuoSubs merge error detail",
			zap.Int("status", resp.StatusCode), logger.Preview(mergeResp.Error))
		return "", errors.New("duosubs reported merge failure")
	}

	return mergeResp.OutputPath, nil
}

// HealthCheck checks if the service is healthy
func (c *HTTPClient) HealthCheck(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/health", http.NoBody)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unhealthy: status %d", resp.StatusCode)
	}

	return nil
}
