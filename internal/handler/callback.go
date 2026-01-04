package handler

import (
	"encoding/json"
	"net/http"

	"github.com/fusionn/internal/service/subtitle"
	"github.com/fusionn/pkg/logger"
)

// TranslationCallbackRequest represents a callback from fusionn-subs after translation.
type TranslationCallbackRequest struct {
	JobID            string `json:"job_id"`
	VideoPath        string `json:"video_path"`
	EnglishSubPath   string `json:"english_subtitle_path"`
	ChineseSubPath   string `json:"chinese_subtitle_path"`
	TranslationError string `json:"translation_error,omitempty"`
}

// TranslationCallbackHandler handles callbacks from fusionn-subs.
type TranslationCallbackHandler struct {
	subtitleService *subtitle.Service
}

// NewTranslationCallbackHandler creates a new callback handler.
func NewTranslationCallbackHandler(subtitleService *subtitle.Service) *TranslationCallbackHandler {
	return &TranslationCallbackHandler{
		subtitleService: subtitleService,
	}
}

// Handle processes translation callback.
func (h *TranslationCallbackHandler) Handle(w http.ResponseWriter, r *http.Request) {
	// Only accept POST
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse request body
	var req TranslationCallbackRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		logger.Errorf("Failed to parse callback request: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	logger.Infof("📥 Translation callback received: job_id=%s", req.JobID)

	// Check for translation error
	if req.TranslationError != "" {
		logger.Errorf("Translation failed (job: %s): %s", req.JobID, req.TranslationError)
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "acknowledged"})
		return
	}

	// Validate required fields
	if req.VideoPath == "" || req.EnglishSubPath == "" || req.ChineseSubPath == "" {
		logger.Errorf("Callback missing required fields: video=%s, eng=%s, zh=%s",
			req.VideoPath, req.EnglishSubPath, req.ChineseSubPath)
		http.Error(w, "Missing required fields", http.StatusBadRequest)
		return
	}

	// Enqueue merge job with the translated subtitle
	if err := h.subtitleService.ProcessWithSubtitles(
		r.Context(),
		req.VideoPath,
		req.EnglishSubPath,
		req.ChineseSubPath,
	); err != nil {
		logger.Errorf("Failed to enqueue merge job: %v", err)
		http.Error(w, "Failed to process callback", http.StatusInternalServerError)
		return
	}

	// Return success
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "success",
		"job_id": req.JobID,
	})

	logger.Infof("✅ Translation callback processed: %s", req.JobID)
}
