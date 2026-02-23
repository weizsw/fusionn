package subtitle

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/fusionn/internal/executor"
	"github.com/fusionn/pkg/logger"
)

// SubtitleTrack represents a detected subtitle track.
type SubtitleTrack struct {
	Index           int
	Language        string
	Title           string
	CodecName       string
	Priority        int    // Lower = higher priority
	NeedsConversion bool   // For Traditional Chinese → Simplified
	ExtractedPath   string // Path to extracted SRT file
}

// AnalysisResult contains the result of subtitle analysis.
type AnalysisResult struct {
	EnglishTrack *SubtitleTrack
	ChineseTrack *SubtitleTrack
	VideoPath    string
}

// Analyzer handles subtitle detection and extraction.
type Analyzer struct {
	englishVariants     []string
	chineseVariants     []string
	simplifiedKeywords  []string
	traditionalKeywords []string
}

// NewAnalyzer creates a new subtitle analyzer.
func NewAnalyzer(englishVariants, chineseVariants, simplifiedKeywords, traditionalKeywords []string) *Analyzer {
	return &Analyzer{
		englishVariants:     englishVariants,
		chineseVariants:     chineseVariants,
		simplifiedKeywords:  simplifiedKeywords,
		traditionalKeywords: traditionalKeywords,
	}
}

// AnalyzeVideo detects subtitle tracks in a video file.
func (a *Analyzer) AnalyzeVideo(ctx context.Context, videoPath string) (*AnalysisResult, error) {
	logger.Infof("Analyzing subtitles in: %s", videoPath)

	// Check if video file exists
	if _, err := os.Stat(videoPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("video file not found: %s", videoPath)
	}

	// Run ffprobe
	probeOutput, err := executor.FFProbe(ctx, videoPath)
	if err != nil {
		return nil, fmt.Errorf("ffprobe failed: %w", err)
	}

	// Filter subtitle streams
	var subtitleStreams []executor.StreamInfo
	for _, stream := range probeOutput.Streams {
		if stream.CodecType == "subtitle" {
			subtitleStreams = append(subtitleStreams, stream)
		}
	}

	logger.Infof("Found %d subtitle track(s)", len(subtitleStreams))

	// Detect English and Chinese subtitles
	englishTrack := a.detectEnglishSubtitle(subtitleStreams)
	chineseTrack := a.detectChineseSubtitle(subtitleStreams)

	if englishTrack != nil {
		logger.Infof("✅ English subtitle detected: index=%d, lang=%s, priority=%d",
			englishTrack.Index, englishTrack.Language, englishTrack.Priority)
	} else {
		logger.Warn("⚠️  No English subtitle found")
	}

	if chineseTrack != nil {
		logger.Infof("✅ Chinese subtitle detected: index=%d, lang=%s, priority=%d, needs_conversion=%v",
			chineseTrack.Index, chineseTrack.Language, chineseTrack.Priority, chineseTrack.NeedsConversion)
	} else {
		logger.Warn("⚠️  No Chinese subtitle found - will queue for translation")
	}

	return &AnalysisResult{
		EnglishTrack: englishTrack,
		ChineseTrack: chineseTrack,
		VideoPath:    videoPath,
	}, nil
}

// detectEnglishSubtitle finds the best English subtitle track.
func (a *Analyzer) detectEnglishSubtitle(streams []executor.StreamInfo) *SubtitleTrack {
	var best *SubtitleTrack

	for _, stream := range streams {
		track := a.matchEnglishTrack(stream)
		if track != nil {
			if best == nil || track.Priority < best.Priority {
				best = track
			}
		}
	}

	return best
}

// matchEnglishTrack checks if a stream is an English subtitle.
func (a *Analyzer) matchEnglishTrack(stream executor.StreamInfo) *SubtitleTrack {
	lang := strings.ToLower(getLanguage(stream))
	title := strings.ToLower(getTitle(stream))

	// Check if language is English first
	isEnglish := false
	for _, variant := range a.englishVariants {
		if lang == strings.ToLower(variant) {
			isEnglish = true
			break
		}
	}

	if !isEnglish {
		return nil
	}

	// Priority 1: Standard English (non-SDH)
	if !strings.Contains(title, "sdh") {
		return &SubtitleTrack{
			Index:     stream.Index,
			Language:  lang,
			Title:     title,
			CodecName: stream.CodecName,
			Priority:  1,
		}
	}

	// Priority 2: English SDH
	return &SubtitleTrack{
		Index:     stream.Index,
		Language:  lang,
		Title:     title,
		CodecName: stream.CodecName,
		Priority:  2,
	}
}

// detectChineseSubtitle finds the best Chinese subtitle track.
func (a *Analyzer) detectChineseSubtitle(streams []executor.StreamInfo) *SubtitleTrack {
	var best *SubtitleTrack

	for _, stream := range streams {
		track := a.matchChineseTrack(stream)
		if track != nil {
			if best == nil || track.Priority < best.Priority {
				best = track
			}
		}
	}

	return best
}

// matchChineseTrack checks if a stream is a Chinese subtitle.
func (a *Analyzer) matchChineseTrack(stream executor.StreamInfo) *SubtitleTrack {
	lang := strings.ToLower(getLanguage(stream))
	title := strings.ToLower(getTitle(stream))

	// Check if language is Chinese-related using configured variants
	isChinese := false
	for _, variant := range a.chineseVariants {
		if lang == strings.ToLower(variant) {
			isChinese = true
			break
		}
	}

	if !isChinese {
		return nil
	}

	// Priority 1: Simplified Chinese (by title keywords)
	for _, keyword := range a.simplifiedKeywords {
		if strings.Contains(title, strings.ToLower(keyword)) {
			return &SubtitleTrack{
				Index:           stream.Index,
				Language:        lang,
				Title:           title,
				CodecName:       stream.CodecName,
				Priority:        1,
				NeedsConversion: false,
			}
		}
	}

	// Priority 2: Traditional Chinese (by title keywords) - needs conversion
	for _, keyword := range a.traditionalKeywords {
		if strings.Contains(title, strings.ToLower(keyword)) {
			return &SubtitleTrack{
				Index:           stream.Index,
				Language:        lang,
				Title:           title,
				CodecName:       stream.CodecName,
				Priority:        2,
				NeedsConversion: true,
			}
		}
	}

	// Priority 3: Fallback - assume Traditional if language is Chinese but title is ambiguous
	// (Most ambiguous Chinese subtitles are Traditional Chinese)
	logger.Warnf("Chinese subtitle detected but type unknown (lang=%s, title=%s), defaulting to Traditional", lang, title)
	return &SubtitleTrack{
		Index:           stream.Index,
		Language:        lang,
		Title:           title,
		CodecName:       stream.CodecName,
		Priority:        3,
		NeedsConversion: true, // Assume Traditional, needs conversion to Simplified
	}
}

// ExtractSubtitles extracts the detected subtitle tracks to SRT files in the media directory.
func (a *Analyzer) ExtractSubtitles(ctx context.Context, result *AnalysisResult) error {
	videoDir := filepath.Dir(result.VideoPath)
	videoBase := filepath.Base(result.VideoPath)
	videoName := strings.TrimSuffix(videoBase, filepath.Ext(videoBase))

	if result.EnglishTrack != nil {
		outputPath := filepath.Join(videoDir, fmt.Sprintf("%s.eng.srt", videoName))
		if err := executor.ExtractSubtitle(ctx, result.VideoPath, result.EnglishTrack.Index, outputPath); err != nil {
			return fmt.Errorf("failed to extract English subtitle: %w", err)
		}
		result.EnglishTrack.ExtractedPath = outputPath
		logger.Infof("Extracted English subtitle: %s", outputPath)
	}

	if result.ChineseTrack != nil {
		outputPath := filepath.Join(videoDir, fmt.Sprintf("%s.chs.srt", videoName))
		if err := executor.ExtractSubtitle(ctx, result.VideoPath, result.ChineseTrack.Index, outputPath); err != nil {
			return fmt.Errorf("failed to extract Chinese subtitle: %w", err)
		}
		result.ChineseTrack.ExtractedPath = outputPath
		logger.Infof("Extracted Chinese subtitle: %s", outputPath)
	}

	return nil
}

// Cleanup removes temporary extracted subtitle files.
func (a *Analyzer) Cleanup(result *AnalysisResult) {
	if result.EnglishTrack != nil && result.EnglishTrack.ExtractedPath != "" {
		if err := os.Remove(result.EnglishTrack.ExtractedPath); err != nil {
			logger.Warnf("Failed to cleanup %s: %v", result.EnglishTrack.ExtractedPath, err)
		}
	}

	if result.ChineseTrack != nil && result.ChineseTrack.ExtractedPath != "" {
		if err := os.Remove(result.ChineseTrack.ExtractedPath); err != nil {
			logger.Warnf("Failed to cleanup %s: %v", result.ChineseTrack.ExtractedPath, err)
		}
	}
}

// Helper functions

func getLanguage(stream executor.StreamInfo) string {
	if stream.Tags != nil {
		if lang, ok := stream.Tags["language"]; ok {
			return lang
		}
	}
	return ""
}

func getTitle(stream executor.StreamInfo) string {
	if stream.Tags != nil {
		if title, ok := stream.Tags["title"]; ok {
			return title
		}
	}
	return ""
}
