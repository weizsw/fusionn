package handler

import (
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/fusionn/internal/service/subtitle"
	"github.com/fusionn/pkg/logger"
)

// CallbackHandler handles translation completion callbacks.
type CallbackHandler struct {
	subtitleService *subtitle.Service
}

// callbackLog is the handler-level logger; request_id/job_id come from the request context.
var callbackLog = logger.Component("callback")

// NewCallbackHandler creates a new callback handler.
func NewCallbackHandler(subtitleService *subtitle.Service) *CallbackHandler {
	return &CallbackHandler{
		subtitleService: subtitleService,
	}
}

// TranslationCallbackPayload represents the payload from fusionn-subs.
type TranslationCallbackPayload struct {
	JobID           string `json:"job_id" binding:"required"`
	VideoPath       string `json:"video_path" binding:"required"`
	EngSubtitlePath string `json:"eng_subtitle_path" binding:"required"`
	ChsSubtitlePath string `json:"chs_subtitle_path" binding:"required"`
}

// HandleTranslationCallback processes translation completion callbacks from fusionn-subs.
func (h *CallbackHandler) HandleTranslationCallback(c *gin.Context) {
	ctx := c.Request.Context()

	var payload TranslationCallbackPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		callbackLog.Warn(ctx, "callback_invalid", "Invalid callback payload", zap.String("reason", "invalid_payload"), zap.Error(err))
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid payload"})
		return
	}

	ctx = logger.WithJobID(ctx, payload.JobID)
	callbackLog.Info(ctx, "callback_received", "Translation callback received")

	// Validate file existence
	if _, err := os.Stat(payload.EngSubtitlePath); os.IsNotExist(err) {
		callbackLog.Warn(ctx, "callback_invalid", "English subtitle file not found",
			zap.String("reason", "missing_english_subtitle"), zap.String("path", payload.EngSubtitlePath))
		c.JSON(http.StatusBadRequest, gin.H{"error": "English subtitle file not found"})
		return
	}

	if _, err := os.Stat(payload.ChsSubtitlePath); os.IsNotExist(err) {
		callbackLog.Warn(ctx, "callback_invalid", "Chinese subtitle file not found",
			zap.String("reason", "missing_chinese_subtitle"), zap.String("path", payload.ChsSubtitlePath))
		c.JSON(http.StatusBadRequest, gin.H{"error": "Chinese subtitle file not found"})
		return
	}

	// Enqueue merge job; the new merge job keeps its own Job ID and records
	// the translation Job ID as parent_job_id.
	if err := h.subtitleService.ProcessWithSubtitles(
		ctx,
		payload.VideoPath,
		payload.EngSubtitlePath,
		payload.ChsSubtitlePath,
		payload.JobID,
	); err != nil {
		callbackLog.Error(ctx, "callback_enqueue_failed", "Failed to enqueue merge job", zap.Error(err))
		MarkErrorReported(c)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Queue full or unavailable"})
		return
	}

	c.JSON(http.StatusAccepted, gin.H{"message": "Callback received, merge job enqueued"})
}
