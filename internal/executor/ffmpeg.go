package executor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"

	"github.com/fusionn/pkg/logger"
)

// FFProbeOutput represents the structure of ffprobe JSON output.
type FFProbeOutput struct {
	Streams []StreamInfo `json:"streams"`
}

// StreamInfo represents a single stream from ffprobe.
type StreamInfo struct {
	Index       int               `json:"index"`
	CodecType   string            `json:"codec_type"`
	CodecName   string            `json:"codec_name"`
	Width       int               `json:"width"`
	Height      int               `json:"height"`
	Tags        map[string]string `json:"tags"`
	Disposition map[string]int    `json:"disposition"`
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

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	logger.Debugf("Executing: %s", cmd.String())

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("ffprobe failed: %w (stderr: %s)", err, stderr.String())
	}

	var output FFProbeOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		return nil, fmt.Errorf("failed to parse ffprobe output: %w", err)
	}

	return &output, nil
}

// ExtractSubtitle extracts a subtitle track to an SRT file.
func ExtractSubtitle(ctx context.Context, videoPath string, streamIndex int, outputPath string) error {
	cmd := exec.CommandContext(ctx,
		"ffmpeg",
		"-v", "quiet",
		"-i", videoPath,
		"-map", fmt.Sprintf("0:%d", streamIndex),
		"-c:s", "srt",
		"-y", // Overwrite output file
		outputPath,
	)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	logger.Debugf("Executing: %s", cmd.String())

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg extract failed: %w (stderr: %s)", err, stderr.String())
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
