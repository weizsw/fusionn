package notification

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"go.uber.org/zap"

	"github.com/fusionn/pkg/logger"
)

// defaultAppriseKey is the placeholder used when no key is configured; it is
// never redacted because it is a common word.
const defaultAppriseKey = "apprise"

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
		key = defaultAppriseKey
	}
	if tag == "" {
		tag = "all"
	}

	client := resty.New().
		SetBaseURL(baseURL).
		SetTimeout(30 * time.Second).
		SetRetryCount(2).
		SetRetryWaitTime(1 * time.Second).
		SetDebug(false).
		// Resty's default logger writes raw lines to stderr (bypassing the
		// unified format) and prints the request URL, which embeds the Apprise
		// key at /notify/<key>. The application emits its own safe summary.
		SetLogger(quietRestyLogger{})

	return &AppriseClient{
		client: client,
		key:    key,
		tag:    tag,
	}
}

// quietRestyLogger swallows Resty's internal WARN/ERROR lines: they bypass the
// unified format and repeat the request URL, which contains the Apprise key.
type quietRestyLogger struct{}

func (quietRestyLogger) Errorf(string, ...interface{}) {}
func (quietRestyLogger) Warnf(string, ...interface{})  {}
func (quietRestyLogger) Debugf(string, ...interface{}) {}

// redact replaces the configured key anywhere in text. The key sits in an
// opaque URL path, so no content regex can recognize it; only the known value
// can be replaced reliably.
func (a *AppriseClient) redact(text string) string {
	if a.key == "" || a.key == defaultAppriseKey {
		return text
	}
	return strings.ReplaceAll(text, a.key, "***")
}

// safeCause strips the *url.Error wrapper so the returned error keeps the
// transport cause (timeout, refused, ...) without the full URL and key.
func (a *AppriseClient) safeCause(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Err != nil {
		return urlErr.Err
	}
	return err
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

	logger.Component("apprise").Debug(ctx, "notification_sending", "Sending Apprise notification")

	var apiResp Response
	resp, err := a.client.R().
		SetContext(ctx).
		SetFormData(formData).
		SetResult(&apiResp).
		Post(fmt.Sprintf("/notify/%s", a.key))

	if err != nil {
		// The raw Resty error is a *url.Error carrying the full URL (and the
		// key in its path); only a key-redacted excerpt may reach DEBUG.
		logger.Component("apprise").Debug(ctx, "notification_transport_detail", "Apprise transport detail",
			logger.Preview(a.redact(err.Error())))
		return fmt.Errorf("sending request: %w", a.safeCause(err))
	}

	if resp.IsError() {
		// The remote body is untrusted content; it never becomes part of the
		// error and appears only as a bounded, redacted DEBUG excerpt.
		logger.Component("apprise").Debug(ctx, "notification_error_detail", "Apprise error detail",
			zap.Int("status", resp.StatusCode()), logger.Preview(a.redact(resp.String())))
		return fmt.Errorf("apprise returned status %d", resp.StatusCode())
	}

	// Check for error in response body (Apprise returns 200 with error in JSON)
	if apiResp.Error != "" {
		logger.Component("apprise").Debug(ctx, "notification_error_detail", "Apprise error detail",
			logger.Preview(a.redact(apiResp.Error)))
		return errors.New("apprise error")
	}

	logger.Component("apprise").Debug(ctx, "notification_sent", "Apprise notification sent")
	return nil
}
