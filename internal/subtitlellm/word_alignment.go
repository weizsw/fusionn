package subtitlellm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/fusionn/internal/config"
	"github.com/fusionn/internal/executor"
	"github.com/fusionn/pkg/logger"
)

// AlignWords uploads short English audio clips and their source transcripts.
// No video paths cross the HTTP boundary. Results use the video's clock.
// A failed request returns completed cues plus an error; callers can keep
// those times and render the remaining cues with proportional timing.
func AlignWords(ctx context.Context, videoPath string, bundle Bundle, cfg config.LLMConfig) (WordTimings, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if cfg.WordAlignURL == "" || videoPath == "" || len(WordAlignmentCues(bundle, cfg)) == 0 {
		return nil, nil
	}
	probe, err := executor.FFProbe(ctx, videoPath)
	if err != nil {
		return nil, fmt.Errorf("probe audio for word alignment: %w", err)
	}
	var audio *executor.StreamInfo
	for i := range probe.Streams {
		s := &probe.Streams[i]
		if s.CodecType != "audio" {
			continue
		}
		if audio == nil {
			audio = s
		}
		lang := strings.ToLower(s.Tags["language"])
		if lang == "eng" || lang == "en" || strings.HasPrefix(lang, "en-") {
			audio = s
			break
		}
	}
	if audio == nil {
		return nil, fmt.Errorf("video has no audio for word alignment")
	}
	return collectWordTimings(ctx, bundle, cfg, func(ctx context.Context, cue Cue) ([]byte, int, error) {
		start := max(0, cue.Start-300)
		duration := cue.End + 300 - start
		args := []string{
			"-nostdin", "-v", "error", "-ss", strconv.FormatFloat(float64(start)/1000, 'f', 3, 64),
			"-i", videoPath, "-t", strconv.FormatFloat(float64(duration)/1000, 'f', 3, 64),
			"-map", fmt.Sprintf("0:%d", audio.Index), "-vn", "-sn", "-dn",
		}
		if hasCenterChannel(audio.ChannelLayout) {
			args = append(args, "-af", "pan=mono|c0=FC")
		}
		args = append(args, "-ac", "1", "-ar", "16000", "-f", "wav", "pipe:1")
		cmd := exec.CommandContext(ctx, "ffmpeg", args...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		data, err := cmd.Output()
		if err != nil {
			logger.Component("word_alignment").Debug(ctx, "extract_failed_detail", "Cue extraction failed",
				zap.Int("cue", cue.ID), logger.Preview(stderr.String()))
			return nil, 0, fmt.Errorf("extract English cue %d: %w", cue.ID, err)
		}
		return data, start, nil
	})
}

func hasCenterChannel(layout string) bool {
	if strings.Contains(layout, "FC") {
		return true
	}
	switch layout {
	case "3.0", "3.1", "4.0", "4.1", "5.0", "5.0(side)", "5.1", "5.1(side)",
		"6.0", "6.1", "6.1(back)", "7.0", "7.0(front)", "7.1", "7.1(wide)", "7.1(wide-side)",
		"hexagonal", "octagonal", "hexadecagonal":
		return true
	}
	return false
}

func collectWordTimings(ctx context.Context, bundle Bundle, cfg config.LLMConfig, extract func(context.Context, Cue) ([]byte, int, error)) (WordTimings, error) {
	result := WordTimings{}
	client := &http.Client{Timeout: 60 * time.Second}
	for _, cue := range WordAlignmentCues(bundle, cfg) {
		clipCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		wav, offset, err := extract(clipCtx, cue)
		if err == nil {
			var words []WordTiming
			words, err = requestWordTimings(clipCtx, client, cfg.WordAlignURL, cue.Display, wav, offset)
			if err == nil && len(words) > 0 {
				result[cue.ID] = words
			}
		}
		cancel()
		if err != nil {
			return result, fmt.Errorf("align English cue %d: %w", cue.ID, err)
		}
	}
	return result, nil
}

func requestWordTimings(ctx context.Context, client *http.Client, baseURL, text string, wav []byte, offset int) ([]WordTiming, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", "clip.wav")
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(wav); err != nil {
		return nil, err
	}
	for key, value := range map[string]string{"text": text, "language": "English"} {
		if err := writer.WriteField(key, value); err != nil {
			return nil, err
		}
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/align", &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	// word-align logs each request under this ID, so its lines can be found
	// from the job that sent them; without one it generates its own.
	if requestID := logger.RequestID(ctx); requestID != "" {
		req.Header.Set("X-Request-ID", requestID)
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("word alignment HTTP status %d", response.StatusCode)
	}
	var payload struct {
		Words []struct {
			Text  string  `json:"text"`
			Start float64 `json:"start_time"`
			End   float64 `json:"end_time"`
		} `json:"words"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode word alignment: %w", err)
	}
	words := make([]WordTiming, len(payload.Words))
	for i, word := range payload.Words {
		if math.IsNaN(word.Start) || math.IsInf(word.Start, 0) || math.IsNaN(word.End) || math.IsInf(word.End, 0) || word.Start < 0 || word.End < word.Start || word.End > 31 {
			return nil, fmt.Errorf("invalid word time at index %d", i)
		}
		words[i] = WordTiming{Text: word.Text, StartMS: offset + int(math.Round(word.Start*1000)), EndMS: offset + int(math.Round(word.End*1000))}
	}
	return words, nil
}
