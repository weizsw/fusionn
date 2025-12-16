package executor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/fusionn/pkg/logger"
)

// ConvertChineseSubtitle converts Traditional Chinese to Simplified Chinese using OpenCC.
// inputPath: Path to subtitle file (SRT or ASS)
// config: OpenCC config (e.g., "t2s.json" for Traditional to Simplified)
//
// Returns the path to the converted subtitle file.
func ConvertChineseSubtitle(ctx context.Context, inputPath, config string) (string, error) {
	// Output path: same directory, with _simplified suffix
	ext := filepath.Ext(inputPath)
	base := strings.TrimSuffix(inputPath, ext)
	outputPath := base + "_simplified" + ext

	// Build opencc command
	// opencc -i <input> -o <output> -c <config>
	args := []string{
		"-i", inputPath,
		"-o", outputPath,
		"-c", config,
	}

	logger.Infof("Executing OpenCC: opencc %v", args)

	cmd := exec.CommandContext(ctx, "opencc", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Execute
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("opencc conversion failed: %w", err)
	}

	// Verify output file exists
	if _, err := os.Stat(outputPath); err != nil {
		return "", fmt.Errorf("converted subtitle file not created: %w", err)
	}

	logger.Infof("OpenCC conversion completed: %s", outputPath)
	return outputPath, nil
}
