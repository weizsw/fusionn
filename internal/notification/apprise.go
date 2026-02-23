package notification

import (
	"context"
	"fmt"
	"time"

	"github.com/go-resty/resty/v2"

	"github.com/fusionn/pkg/logger"
)

// Type represents the type of notification.
type Type string

const (
	Success Type = "success"
	Info    Type = "info"
	Warning Type = "warning"
	Error   Type = "failure"
)

// Response represents an Apprise API response
type Response struct {
	Error string `json:"error,omitempty"`
}

// AppriseClient wraps Apprise API for sending notifications.
type AppriseClient struct {
	client *resty.Client
	key    string
	tag    string
}

// NewAppriseClient creates a new Apprise client.
func NewAppriseClient(baseURL, key, tag string) *AppriseClient {
	if key == "" {
		key = "apprise"
	}
	if tag == "" {
		tag = "all"
	}

	client := resty.New().
		SetBaseURL(baseURL).
		SetTimeout(30 * time.Second).
		SetRetryCount(2).
		SetRetryWaitTime(1 * time.Second)

	return &AppriseClient{
		client: client,
		key:    key,
		tag:    tag,
	}
}

// Send sends a notification via Apprise.
func (a *AppriseClient) Send(ctx context.Context, title, body string, notificationType Type) error {
	if a.key == "" {
		return fmt.Errorf("apprise client not configured")
	}

	// Build form data
	formData := map[string]string{
		"body": body,
		"tags": a.tag,
	}
	if title != "" {
		formData["title"] = title
	}
	if notificationType != "" {
		formData["type"] = string(notificationType)
	}

	logger.Debugf("Sending Apprise notification to /notify/%s", a.key)

	var apiResp Response
	resp, err := a.client.R().
		SetContext(ctx).
		SetFormData(formData).
		SetResult(&apiResp).
		Post(fmt.Sprintf("/notify/%s", a.key))

	if err != nil {
		return fmt.Errorf("sending request: %w", err)
	}

	if resp.IsError() {
		return fmt.Errorf("apprise returned status %d: %s", resp.StatusCode(), resp.String())
	}

	// Check for error in response body (Apprise returns 200 with error in JSON)
	if apiResp.Error != "" {
		return fmt.Errorf("apprise error: %s", apiResp.Error)
	}

	logger.Debugf("Apprise notification sent successfully")
	return nil
}
