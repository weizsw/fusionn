package handler

import (
	"net/http"
	"os"

	"github.com/gin-gonic/gin"

	"github.com/fusionn/internal/service/subtitle"
	"github.com/fusionn/pkg/logger"
)

// CallbackHandler handles translation completion callbacks.
type CallbackHandler struct {
	subtitleService *subtitle.Service
}

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
	var payload TranslationCallbackPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		logger.Errorf("Invalid callback payload: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid payload"})
		return
	}

	logger.Infof("📥 Translation callback received: job_id=%s", payload.JobID)

	// Validate file existence
	if _, err := os.Stat(payload.EngSubtitlePath); os.IsNotExist(err) {
		logger.Errorf("English subtitle file not found: %s", payload.EngSubtitlePath)
		c.JSON(http.StatusBadRequest, gin.H{"error": "English subtitle file not found"})
		return
	}

	if _, err := os.Stat(payload.ChsSubtitlePath); os.IsNotExist(err) {
		logger.Errorf("Chinese subtitle file not found: %s", payload.ChsSubtitlePath)
		c.JSON(http.StatusBadRequest, gin.H{"error": "Chinese subtitle file not found"})
		return
	}

	// Enqueue merge job
	if err := h.subtitleService.ProcessWithSubtitles(
		c.Request.Context(),
		payload.VideoPath,
		payload.EngSubtitlePath,
		payload.ChsSubtitlePath,
	); err != nil {
		logger.Errorf("Failed to enqueue merge job: %v", err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Queue full or unavailable"})
		return
	}

	logger.Infof("✅ Merge job enqueued for translation callback: job_id=%s", payload.JobID)
	c.JSON(http.StatusAccepted, gin.H{"message": "Callback received, merge job enqueued"})
}
