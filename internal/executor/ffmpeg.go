package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

// FFProbeOutput represents the structure of ffprobe JSON output.
type FFProbeOutput struct {
	Streams []StreamInfo `json:"streams"`
}

// StreamInfo represents a single stream from ffprobe.
type StreamInfo struct {
	Index         int               `json:"index"`
	CodecType     string            `json:"codec_type"`
	CodecName     string            `json:"codec_name"`
	Width         int               `json:"width"`
	Height        int               `json:"height"`
	ChannelLayout string            `json:"channel_layout"`
	Tags          map[string]string `json:"tags"`
	Disposition   map[string]int    `json:"disposition"`
}

// FFProbe executes ffprobe to list all streams in a video file.
func FFProbe(ctx context.Context, videoPath string) (*FFProbeOutput, error) {
	cmd := exec.CommandContext(ctx,
		"ffprobe",
		"-v", "quiet",
		"-print_format", "json",
		"-show_streams",
		videoPath,
	)

	var stdout bytes.Buffer // ffprobe result, parsed below
	tail := &boundedTail{}
	cmd.Stdout = &stdout
	cmd.Stderr = tail

	start := toolStarted(ctx, "ffprobe", cmd)
	err := cmd.Run()
	toolFinished(ctx, "ffprobe", start, err, tail.Diagnostic())
	if err != nil {
		return nil, fmt.Errorf("ffprobe failed: %w", err)
	}

	var output FFProbeOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		return nil, fmt.Errorf("failed to parse ffprobe output: %w", err)
	}

	return &output, nil
}

// ExtractSubtitle extracts a subtitle track to an SRT file.
func ExtractSubtitle(ctx context.Context, videoPath string, streamIndex int, outputPath string) error {
	return extractSubtitle(ctx, videoPath, streamIndex, outputPath, "srt")
}

// ExtractSubtitleNative copies a text subtitle stream without transcoding, so
// native ASS/SSA alignment tags, explicit positions and style tables survive.
func ExtractSubtitleNative(ctx context.Context, videoPath string, streamIndex int, outputPath string) error {
	return extractSubtitle(ctx, videoPath, streamIndex, outputPath, "copy")
}

func extractSubtitle(ctx context.Context, videoPath string, streamIndex int, outputPath, codec string) error {
	cmd := exec.CommandContext(ctx,
		"ffmpeg",
		"-v", "quiet",
		"-i", videoPath,
		"-map", fmt.Sprintf("0:%d", streamIndex),
		"-c:s", codec,
		"-y", // Overwrite output file
		outputPath,
	)

	tail := &boundedTail{}
	cmd.Stderr = tail

	start := toolStarted(ctx, "ffmpeg", cmd)
	err := cmd.Run()
	toolFinished(ctx, "ffmpeg", start, err, tail.Diagnostic())
	if err != nil {
		return fmt.Errorf("ffmpeg extract failed: %w", err)
	}

	return nil
}

// CheckFFmpegAvailable checks if ffmpeg and ffprobe are installed.
func CheckFFmpegAvailable() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Check ffprobe
	if err := exec.CommandContext(ctx, "ffprobe", "-version").Run(); err != nil {
		return fmt.Errorf("ffprobe not found: %w", err)
	}

	// Check ffmpeg
	if err := exec.CommandContext(ctx, "ffmpeg", "-version").Run(); err != nil {
		return fmt.Errorf("ffmpeg not found: %w", err)
	}

	return nil
}
