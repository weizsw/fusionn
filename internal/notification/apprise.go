package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/fusionn/pkg/logger"
)

// NotificationType represents the type of notification.
type NotificationType string

const (
	Success NotificationType = "success"
	Info    NotificationType = "info"
	Warning NotificationType = "warning"
	Error   NotificationType = "failure"
)

// AppriseClient wraps Apprise API for sending notifications.
type AppriseClient struct {
	baseURL string
	key     string
	tag     string
	client  *http.Client
}

// NewAppriseClient creates a new Apprise client.
func NewAppriseClient(baseURL, key, tag string) *AppriseClient {
	return &AppriseClient{
		baseURL: baseURL,
		key:     key,
		tag:     tag,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// appriseRequest represents the Apprise notification request payload.
type appriseRequest struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Type  string `json:"type,omitempty"`
	Tag   string `json:"tag,omitempty"`
}

// Send sends a notification via Apprise.
func (a *AppriseClient) Send(ctx context.Context, title, body string, notificationType NotificationType) error {
	if a.baseURL == "" || a.key == "" {
		return fmt.Errorf("apprise client not configured")
	}

	url := fmt.Sprintf("%s/notify/%s", a.baseURL, a.key)

	payload := appriseRequest{
		Title: title,
		Body:  body,
		Type:  string(notificationType),
		Tag:   a.tag,
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal apprise request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create apprise request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	logger.Debugf("Sending Apprise notification to %s", url)

	resp, err := a.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send apprise request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("apprise returned status %d", resp.StatusCode)
	}

	logger.Debugf("Apprise notification sent successfully")
	return nil
}
