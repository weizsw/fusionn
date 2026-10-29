package subtitle

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"go.uber.org/zap"

	"github.com/fusionn/internal/executor"
	"github.com/fusionn/pkg/logger"
)

const (
	penaltyForcedDisposition = 100
	penaltyForcedTitle       = 100
	penaltyNonTextCodec      = 100
	penaltySDHDisposition    = 10
	penaltySDHTitle          = 10
	penaltyLowFrameCount     = 50
	penaltyLowByteCount      = 50
	heuristicThreshold       = 0.25
)

const (
	codecTypeSubtitle          = "subtitle"
	dispositionForced          = "forced"
	dispositionHearingImpaired = "hearing_impaired"
	tagLanguage                = "language"
	tagTitle                   = "title"
	tagNumberOfFrames          = "NUMBER_OF_FRAMES"
	tagNumberOfBytes           = "NUMBER_OF_BYTES"
)

// Track represents a detected subtitle track.
type Track struct {
	Index           int
	Language        string
	Title           string
	CodecName       string
	Priority        int    // Lower = higher priority
	NeedsConversion bool   // For Traditional Chinese → Simplified
	ExtractedPath   string // Path to extracted SRT file
	IsSDH           bool
}

// AnalysisResult contains the result of subtitle analysis.
type AnalysisResult struct {
	EnglishTrack *Track
	ChineseTrack *Track
	VideoPath    string
}

// Analyzer handles subtitle detection and extraction.
type Analyzer struct {
	englishVariants     []string
	chineseVariants     []string
	simplifiedKeywords  []string
	traditionalKeywords []string
	preserveNativeASS   bool
}

// Extraction hooks are variables so tests can observe mode selection without
// requiring ffmpeg.
var (
	extractSubtitleFunc       = executor.ExtractSubtitle
	extractSubtitleNativeFunc = executor.ExtractSubtitleNative
)

// NewAnalyzer creates a new subtitle analyzer.
func NewAnalyzer(englishVariants, chineseVariants, simplifiedKeywords, traditionalKeywords []string) *Analyzer {
	return &Analyzer{
		englishVariants:     englishVariants,
		chineseVariants:     chineseVariants,
		simplifiedKeywords:  simplifiedKeywords,
		traditionalKeywords: traditionalKeywords,
	}
}

// SetPreserveNativeASS makes extraction copy ASS/SSA subtitle streams without
// transcoding, preserving native alignment tags, explicit positions and style
// tables for the LLM renderer. SRT extraction behavior is unchanged.
func (a *Analyzer) SetPreserveNativeASS(enabled bool) {
	a.preserveNativeASS = enabled
}

// AnalyzeVideo detects subtitle tracks in a video file.
func (a *Analyzer) AnalyzeVideo(ctx context.Context, videoPath string) (*AnalysisResult, error) {
	log := logger.Component("analyzer")
	log.Debug(ctx, "analysis_detail", "Analyzing subtitle tracks", logger.Preview(videoPath))

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
		if stream.CodecType == codecTypeSubtitle {
			subtitleStreams = append(subtitleStreams, stream)
		}
	}

	log.Info(ctx, "tracks_found", "Subtitle tracks found", zap.Int("count", len(subtitleStreams)))

	// Detect English and Chinese subtitles
	englishTrack := a.detectEnglishSubtitle(ctx, subtitleStreams)
	chineseTrack := a.detectChineseSubtitle(ctx, subtitleStreams)

	if englishTrack != nil {
		log.Info(ctx, "track_detected", "English subtitle detected",
			zap.String("language", "eng"), zap.Int("index", englishTrack.Index),
			zap.Int("priority", englishTrack.Priority))
	} else {
		log.Warn(ctx, "track_missing", "No English subtitle found", zap.String("language", "eng"))
	}

	if chineseTrack != nil {
		log.Info(ctx, "track_detected", "Chinese subtitle detected",
			zap.String("language", "chs"), zap.Int("index", chineseTrack.Index),
			zap.Int("priority", chineseTrack.Priority), zap.Bool("needs_conversion", chineseTrack.NeedsConversion))
	} else {
		log.Warn(ctx, "track_missing", "No Chinese subtitle found, will queue for translation", zap.String("language", "chs"))
	}

	return &AnalysisResult{
		EnglishTrack: englishTrack,
		ChineseTrack: chineseTrack,
		VideoPath:    videoPath,
	}, nil
}

// englishCandidate pairs a stream with its parsed metadata for scoring.
type englishCandidate struct {
	stream executor.StreamInfo
	lang   string
	title  string
	frames int
	bytes  int
}

// englishScore stores penalty breakdown by signal layer.
type englishScore struct {
	CodecPenalty       int
	DispositionPenalty int
	TitlePenalty       int
	FramePenalty       int
	BytePenalty        int
	Total              int
}

// detectEnglishSubtitle finds the best English subtitle using layered penalty scoring.
// Pass 1: collect all English candidates and find max frames/bytes.
// Pass 2: score each candidate, pick the lowest penalty (tie-break by frame count, then stream order).
func (a *Analyzer) detectEnglishSubtitle(ctx context.Context, streams []executor.StreamInfo) *Track {
	log := logger.Component("analyzer")

	var candidates []englishCandidate
	maxFrames, maxBytes := 0, 0

	for _, stream := range streams {
		if !isTextSubtitleCodec(stream.CodecName) {
			continue
		}
		lang := strings.ToLower(getLanguage(stream))
		if !a.isEnglishLang(lang) {
			continue
		}
		title := strings.ToLower(getTitle(stream))
		frames, _ := getFrameCount(stream)
		bytes, _ := getByteCount(stream)

		candidates = append(candidates, englishCandidate{
			stream: stream,
			lang:   lang,
			title:  title,
			frames: frames,
			bytes:  bytes,
		})

		if frames > maxFrames {
			maxFrames = frames
		}
		if bytes > maxBytes {
			maxBytes = bytes
		}
	}

	if len(candidates) == 0 {
		return nil
	}

	scores := make([]englishScore, len(candidates))
	bestIdx := -1
	bestScore := englishScore{}

	for i := range candidates {
		c := &candidates[i]
		score := scoreEnglishTrackDetails(c.stream, c.title, c.frames, c.bytes, maxFrames, maxBytes, len(candidates))
		scores[i] = score

		log.Debug(ctx, "candidate_scored", "English candidate scored",
			zap.Int("index", c.stream.Index), zap.Int("total", score.Total),
			zap.Int("codec", score.CodecPenalty), zap.Int("disposition", score.DispositionPenalty),
			zap.Int("title", score.TitlePenalty), zap.Int("frame", score.FramePenalty),
			zap.Int("byte", score.BytePenalty), zap.String("codec_name", c.stream.CodecName),
			zap.Int("frames", c.frames), zap.Int("bytes", c.bytes))

		if bestIdx < 0 || score.Total < bestScore.Total || (score.Total == bestScore.Total && c.frames > candidates[bestIdx].frames) {
			bestIdx = i
			bestScore = score
		}
	}

	if len(candidates) > 1 {
		runnerUpIdx := -1
		runnerUpScore := englishScore{}
		for i := range candidates {
			if i == bestIdx {
				continue
			}
			score := scores[i]
			if runnerUpIdx < 0 || score.Total < runnerUpScore.Total || (score.Total == runnerUpScore.Total && candidates[i].frames > candidates[runnerUpIdx].frames) {
				runnerUpIdx = i
				runnerUpScore = score
			}
		}

		if runnerUpIdx >= 0 {
			log.Debug(ctx, "english_candidate_selected", "Selected English candidate",
				zap.Int("index", candidates[bestIdx].stream.Index), zap.Int("total", bestScore.Total),
				zap.Int("runner_up_index", candidates[runnerUpIdx].stream.Index), zap.Int("runner_up_total", runnerUpScore.Total))
		} else {
			log.Debug(ctx, "english_candidate_selected", "Selected English candidate",
				zap.Int("index", candidates[bestIdx].stream.Index), zap.Int("total", bestScore.Total))
		}
	}

	best := &candidates[bestIdx]
	return &Track{
		Index:     best.stream.Index,
		Language:  best.lang,
		Title:     best.title,
		CodecName: best.stream.CodecName,
		Priority:  bestScore.Total,
		IsSDH:     isHearingImpaired(best.stream) || isSDHByTitle(best.title),
	}
}

// scoreEnglishTrack computes a penalty score across four independent signal layers.
// Lower score = better track. Layers: disposition flags, title keywords, frame heuristic, byte heuristic.
func scoreEnglishTrack(stream executor.StreamInfo, title string, frames, bytes, maxFrames, maxBytes, candidateCount int) int {
	return scoreEnglishTrackDetails(stream, title, frames, bytes, maxFrames, maxBytes, candidateCount).Total
}

func scoreEnglishTrackDetails(stream executor.StreamInfo, title string, frames, bytes, maxFrames, maxBytes, candidateCount int) englishScore {
	score := englishScore{}

	if !isTextSubtitleCodec(stream.CodecName) {
		score.CodecPenalty += penaltyNonTextCodec
	}

	if isForced(stream) {
		score.DispositionPenalty += penaltyForcedDisposition
	}
	if isHearingImpaired(stream) {
		score.DispositionPenalty += penaltySDHDisposition
	}

	if isForcedByTitle(title) {
		score.TitlePenalty += penaltyForcedTitle
	}
	if isSDHByTitle(title) {
		score.TitlePenalty += penaltySDHTitle
	}

	if candidateCount > 1 {
		if maxFrames > 0 && frames > 0 && float64(frames) < float64(maxFrames)*heuristicThreshold {
			score.FramePenalty += penaltyLowFrameCount
		}
		if maxBytes > 0 && bytes > 0 && float64(bytes) < float64(maxBytes)*heuristicThreshold {
			score.BytePenalty += penaltyLowByteCount
		}
	}

	score.Total = score.CodecPenalty + score.DispositionPenalty + score.TitlePenalty + score.FramePenalty + score.BytePenalty
	return score
}

func isTextSubtitleCodec(codecName string) bool {
	switch strings.ToLower(codecName) {
	case "ass", "mov_text", "ssa", "subrip", "text", "webvtt":
		return true
	default:
		return false
	}
}

func (a *Analyzer) isEnglishLang(lang string) bool {
	for _, variant := range a.englishVariants {
		if strings.EqualFold(lang, variant) {
			return true
		}
	}
	return false
}

func isForced(stream executor.StreamInfo) bool {
	if stream.Disposition == nil {
		return false
	}
	return stream.Disposition[dispositionForced] == 1
}

func isHearingImpaired(stream executor.StreamInfo) bool {
	if stream.Disposition == nil {
		return false
	}
	return stream.Disposition[dispositionHearingImpaired] == 1
}

func getFrameCount(stream executor.StreamInfo) (int, bool) {
	if stream.Tags == nil {
		return 0, false
	}
	v, ok := stream.Tags[tagNumberOfFrames]
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, false
	}
	return n, true
}

func getByteCount(stream executor.StreamInfo) (int, bool) {
	if stream.Tags == nil {
		return 0, false
	}
	v, ok := stream.Tags[tagNumberOfBytes]
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, false
	}
	return n, true
}

var forcedTitleKeywords = []string{"forced", "signs & songs", "signs"}

func isForcedByTitle(title string) bool {
	for _, kw := range forcedTitleKeywords {
		if strings.Contains(title, kw) {
			return true
		}
	}
	return false
}

var sdhTitleKeywords = []string{"sdh", "hearing impaired", "cc"}

func isSDHByTitle(title string) bool {
	for _, kw := range sdhTitleKeywords {
		if strings.Contains(title, kw) {
			return true
		}
	}
	return false
}

// detectChineseSubtitle finds the best Chinese subtitle track. Complete
// dialogue tracks come first: the English completeness penalties (forced,
// SDH, far fewer events or bytes than the largest Chinese track) apply here
// too, so a Simplified signs-only track does not beat a full Traditional one.
// Among equally complete tracks Simplified beats Traditional beats unknown,
// then stream order decides.
func (a *Analyzer) detectChineseSubtitle(ctx context.Context, streams []executor.StreamInfo) *Track {
	type candidate struct {
		track         *Track
		stream        executor.StreamInfo
		frames, bytes int
	}
	var candidates []candidate
	maxFrames, maxBytes := 0, 0
	for _, stream := range streams {
		track := a.matchChineseTrack(ctx, stream)
		if track == nil {
			continue
		}
		frames, _ := getFrameCount(stream)
		bytes, _ := getByteCount(stream)
		candidates = append(candidates, candidate{track: track, stream: stream, frames: frames, bytes: bytes})
		maxFrames = max(maxFrames, frames)
		maxBytes = max(maxBytes, bytes)
	}

	var best *Track
	bestPenalty := 0
	for _, c := range candidates {
		penalty := scoreEnglishTrack(c.stream, c.track.Title, c.frames, c.bytes, maxFrames, maxBytes, len(candidates))
		if best == nil || penalty < bestPenalty || (penalty == bestPenalty && c.track.Priority < best.Priority) {
			best, bestPenalty = c.track, penalty
		}
	}
	if len(candidates) > 1 {
		logger.Component("analyzer").Debug(ctx, "chinese_candidate_selected", "Selected Chinese candidate",
			zap.Int("index", best.Index), logger.Preview(best.Title), zap.Int("penalty", bestPenalty),
			zap.Int("priority", best.Priority), zap.Int("candidates", len(candidates)))
	}
	return best
}

// matchChineseTrack checks if a stream is a Chinese subtitle.
func (a *Analyzer) matchChineseTrack(ctx context.Context, stream executor.StreamInfo) *Track {
	if !isTextSubtitleCodec(stream.CodecName) {
		return nil
	}

	lang := strings.ToLower(getLanguage(stream))
	title := strings.ToLower(getTitle(stream))

	// Check if language is Chinese-related using configured variants
	isChinese := false
	for _, variant := range a.chineseVariants {
		if strings.EqualFold(lang, variant) {
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
			return &Track{
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
			return &Track{
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
	log := logger.Component("analyzer")
	log.Warn(ctx, "chinese_type_unknown", "Chinese subtitle type unknown, defaulting to Traditional",
		zap.String("language", lang), zap.String("title", title))
	return &Track{
		Index:           stream.Index,
		Language:        lang,
		Title:           title,
		CodecName:       stream.CodecName,
		Priority:        3,
		NeedsConversion: true, // Assume Traditional, needs conversion to Simplified
	}
}

// ExtractSubtitles extracts the detected subtitle tracks to files in the media
// directory. SRT is the default; see extractTrack for the LLM native ASS mode.
func (a *Analyzer) ExtractSubtitles(ctx context.Context, result *AnalysisResult) error {
	log := logger.Component("analyzer")
	videoBase := filepath.Base(result.VideoPath)
	videoName := strings.TrimSuffix(videoBase, filepath.Ext(videoBase))

	if result.EnglishTrack != nil {
		outputPath, err := a.extractTrack(ctx, result.VideoPath, videoName, "eng", result.EnglishTrack)
		if err != nil {
			return fmt.Errorf("failed to extract English subtitle: %w", err)
		}
		result.EnglishTrack.ExtractedPath = outputPath
		log.Info(ctx, "subtitle_extracted", "English subtitle extracted",
			zap.String("language", "eng"), zap.String("path", outputPath))
	}

	if result.ChineseTrack != nil {
		outputPath, err := a.extractTrack(ctx, result.VideoPath, videoName, "chs", result.ChineseTrack)
		if err != nil {
			// Do not leave the already-extracted English temp file behind when
			// a later track fails.
			if a.preserveNativeASS && result.EnglishTrack != nil && result.EnglishTrack.ExtractedPath != "" {
				os.Remove(result.EnglishTrack.ExtractedPath)
				result.EnglishTrack.ExtractedPath = ""
			}
			return fmt.Errorf("failed to extract Chinese subtitle: %w", err)
		}
		result.ChineseTrack.ExtractedPath = outputPath
		log.Info(ctx, "subtitle_extracted", "Chinese subtitle extracted",
			zap.String("language", "chs"), zap.String("path", outputPath))
	}

	return nil
}

// extractTrack extracts one track. LLM mode copies native ASS/SSA streams
// without transcoding. DuoSubs keeps its deterministic sidecar names; LLM mode
// reserves a unique hidden temp path in the media directory so an existing
// sidecar is never touched and concurrent jobs cannot collide. The path still
// ends in .eng.srt/.chs.srt or .eng.ass/.chs.ass for downstream format
// detection. The extractor owns and overwrites the reserved file.
func (a *Analyzer) extractTrack(ctx context.Context, videoPath, videoName, suffix string, track *Track) (string, error) {
	videoDir := filepath.Dir(videoPath)
	ext := "srt"
	extract := extractSubtitleFunc
	if a.preserveNativeASS && isASSSubtitleCodec(track.CodecName) {
		ext = "ass"
		extract = extractSubtitleNativeFunc
	}

	var outputPath string
	if a.preserveNativeASS {
		reserved, err := reserveExtractionPath(videoDir, videoName, suffix, ext)
		if err != nil {
			return "", err
		}
		outputPath = reserved
	} else {
		outputPath = filepath.Join(videoDir, fmt.Sprintf("%s.%s.%s", videoName, suffix, ext))
	}

	if err := extract(ctx, videoPath, track.Index, outputPath); err != nil {
		if a.preserveNativeASS {
			os.Remove(outputPath)
		}
		return "", err
	}
	return outputPath, nil
}

// reserveExtractionPath creates a unique hidden temp file in the media
// directory and closes it; the extractor then overwrites it with -y. The
// pattern keeps the conventional .<suffix>.<ext> ending while the random
// component is inserted before it, so the result never matches an existing
// sidecar.
func reserveExtractionPath(videoDir, videoName, suffix, ext string) (string, error) {
	pattern := fmt.Sprintf(".fusionn-extract-%s-*.%s.%s", videoName, suffix, ext)
	file, err := os.CreateTemp(videoDir, pattern)
	if err != nil {
		return "", fmt.Errorf("reserve temp extraction path: %w", err)
	}
	name := file.Name()
	if err := file.Close(); err != nil {
		os.Remove(name)
		return "", fmt.Errorf("close reserved extraction path: %w", err)
	}
	return name, nil
}

func isASSSubtitleCodec(codecName string) bool {
	switch strings.ToLower(codecName) {
	case "ass", "ssa":
		return true
	default:
		return false
	}
}

// Cleanup removes temporary extracted subtitle files.
func (a *Analyzer) Cleanup(result *AnalysisResult) {
	log := logger.Component("analyzer")
	if result.EnglishTrack != nil && result.EnglishTrack.ExtractedPath != "" {
		if err := os.Remove(result.EnglishTrack.ExtractedPath); err != nil {
			log.Warn(context.Background(), "cleanup_failed", "Failed to clean up extracted subtitle",
				zap.String("path", result.EnglishTrack.ExtractedPath), zap.Error(err))
		}
	}

	if result.ChineseTrack != nil && result.ChineseTrack.ExtractedPath != "" {
		if err := os.Remove(result.ChineseTrack.ExtractedPath); err != nil {
			log.Warn(context.Background(), "cleanup_failed", "Failed to clean up extracted subtitle",
				zap.String("path", result.ChineseTrack.ExtractedPath), zap.Error(err))
		}
	}
}

// Helper functions

func getLanguage(stream executor.StreamInfo) string {
	if stream.Tags != nil {
		if lang, ok := stream.Tags[tagLanguage]; ok {
			return lang
		}
	}
	return ""
}

func getTitle(stream executor.StreamInfo) string {
	if stream.Tags != nil {
		if title, ok := stream.Tags[tagTitle]; ok {
			return title
		}
	}
	return ""
}
