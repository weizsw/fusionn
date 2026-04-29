package subtitle

import (
	"context"
	"fmt"
	"path/filepath"

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
	return ProcessorNameNotification
}

// ShouldRun runs if Apprise is enabled.
func (p *NotificationProcessor) ShouldRun(pctx *ProcessingContext) bool {
	return p.enabled && p.appriseClient != nil
}

// Process sends a notification based on processing result.
func (p *NotificationProcessor) Process(ctx context.Context, pctx *ProcessingContext) error {
	log := logger.Indent()
	var title, body string
	var notificationType notification.Type

	// Extract media filename from VideoPath
	mediaFile := "Unknown"
	if pctx.VideoPath != "" {
		mediaFile = filepath.Base(pctx.VideoPath)
	}

	// Determine notification type based on processing result
	if pctx.MergedSubPath != "" {
		title = "✅ Subtitle Merge Complete"

		chineseSub := "Extracted"
		switch pctx.ChineseSubSource {
		case ChineseSourceBazarr:
			chineseSub = "Downloaded by Bazarr"
		case ChineseSourceTranslated:
			chineseSub = "Translated"
		}

		body = fmt.Sprintf(
			"Media: %s\nType: %s\nChinese Sub: %s",
			mediaFile,
			pctx.MediaType,
			chineseSub,
		)

		// Add conversion status if Traditional → Simplified conversion occurred
		if pctx.NeedsConversion {
			body += "\nConversion: Traditional → Simplified Chinese"
		}

		notificationType = notification.Success
	} else if pctx.NeedsTranslation {
		// Info: translation queued
		title = "📋 Translation Queued"
		body = fmt.Sprintf(
			"Media: %s\nType: %s\nChinese Sub: Queued for Translation\nReason: Chinese subtitle missing",
			mediaFile,
			pctx.MediaType,
		)
		notificationType = notification.Info
	} else {
		// Warning: no subtitles processed
		title = "⚠️ No Subtitles Processed"
		body = fmt.Sprintf(
			"Media: %s\nType: %s\nChinese Sub: Not Available\nReason: Required subtitles not found",
			mediaFile,
			pctx.MediaType,
		)
		notificationType = notification.Warning
	}

	log.Infof("Sending notification: %s", title)

	if err := p.appriseClient.Send(ctx, title, body, notificationType); err != nil {
		// Log error but don't fail the pipeline
		log.Errorf("Failed to send notification: %v", err)
	}

	return nil
}
