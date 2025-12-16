package subtitle

import (
	"context"
	"fmt"

	"github.com/fusionn/internal/notification"
	"github.com/fusionn/pkg/logger"
)

// NotificationProcessor sends success/failure notifications via Apprise.
type NotificationProcessor struct {
	appriseClient *notification.AppriseClient
	enabled       bool
}

// NewNotificationProcessor creates a new notification processor.
func NewNotificationProcessor(client *notification.AppriseClient, enabled bool) *NotificationProcessor {
	return &NotificationProcessor{
		appriseClient: client,
		enabled:       enabled,
	}
}

// Name returns the processor name.
func (p *NotificationProcessor) Name() string {
	return "NotificationProcessor"
}

// ShouldRun runs if Apprise is enabled.
func (p *NotificationProcessor) ShouldRun(pctx *ProcessingContext) bool {
	return p.enabled && p.appriseClient != nil
}

// Process sends a notification based on processing result.
func (p *NotificationProcessor) Process(ctx context.Context, pctx *ProcessingContext) error {
	var title, body string
	var notificationType notification.NotificationType

	// Determine notification type based on processing result
	if pctx.MergedSubPath != "" {
		// Success: subtitle merged
		title = "✅ Subtitle Merge Complete"
		body = fmt.Sprintf(
			"Media: %s\nType: %s\nOutput: %s",
			pctx.MediaTitle,
			pctx.MediaType,
			pctx.MergedSubPath,
		)
		notificationType = notification.Success
	} else if pctx.NeedsTranslation {
		// Info: translation queued
		title = "📋 Translation Queued"
		body = fmt.Sprintf(
			"Media: %s\nType: %s\nReason: Chinese subtitle missing",
			pctx.MediaTitle,
			pctx.MediaType,
		)
		notificationType = notification.Info
	} else {
		// Warning: no subtitles processed
		title = "⚠️ No Subtitles Processed"
		body = fmt.Sprintf(
			"Media: %s\nType: %s\nReason: Required subtitles not found",
			pctx.MediaTitle,
			pctx.MediaType,
		)
		notificationType = notification.Warning
	}

	logger.Infof("Sending notification: %s", title)

	if err := p.appriseClient.Send(ctx, title, body, notificationType); err != nil {
		// Log error but don't fail the pipeline
		logger.Errorf("Failed to send notification: %v", err)
	}

	return nil
}
