package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/fusionn/internal/service/subtitle"
	"github.com/fusionn/pkg/logger"
)

// CallbackHandler handles translation completion callbacks from fusionn-subs.
type CallbackHandler struct {
	subtitleService *subtitle.Service
}

// NewCallbackHandler creates a new callback handler.
func NewCallbackHandler(service *subtitle.Service) *CallbackHandler {
	return &CallbackHandler{
		subtitleService: service,
	}
}

// TranslationCallbackPayload matches the payload from fusionn-subs.
// See: fusionn-subs/internal/client/callback/client.go
type TranslationCallbackPayload struct {
	ChsSubtitlePath string `json:"chs_subtitle_path" binding:"required"`
	EngSubtitlePath string `json:"eng_subtitle_path" binding:"required"`
	VideoPath       string `json:"video_path" binding:"required"`
}

// HandleTranslationCallback handles POST /api/v1/callback/translation.
func (h *CallbackHandler) HandleTranslationCallback(c *gin.Context) {
	var payload TranslationCallbackPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		logger.Errorf("Invalid translation callback payload: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Invalid payload",
		})
		return
	}

	logger.Infof("🔄 Translation callback received: video=%s", payload.VideoPath)
	logger.Debugf("  English: %s", payload.EngSubtitlePath)
	logger.Debugf("  Chinese: %s", payload.ChsSubtitlePath)

	// Trigger subtitle merge with the translated Chinese subtitle
	// Process in background to avoid blocking callback
	go func() {
		if err := h.subtitleService.ProcessWithSubtitles(
			c.Request.Context(),
			payload.VideoPath,
			payload.EngSubtitlePath,
			payload.ChsSubtitlePath,
		); err != nil {
			logger.Errorf("Failed to merge translated subtitles: %v", err)
		}
	}()

	c.JSON(http.StatusOK, gin.H{
		"status": "processing",
	})
}
