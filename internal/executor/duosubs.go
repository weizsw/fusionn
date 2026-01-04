package executor

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/fusionn/pkg/logger"
)

// DuoSubsConfig holds DuoSubs execution parameters.
type DuoSubsConfig struct {
	Model   string
	Device  string
	Timeout time.Duration
}

// MergeDuoSubs runs DuoSubs to merge English and Chinese subtitles into bilingual ASS.
func MergeDuoSubs(ctx context.Context, englishPath, chinesePath, outputPath string, cfg DuoSubsConfig) error {
	logger.Infof("Running DuoSubs: eng=%s, zh=%s, out=%s", englishPath, chinesePath, outputPath)

	// Build DuoSubs command
	// duosubs -e <english> -s <chinese> -o <output> --model <model> --device <device>
	args := []string{
		"-e", englishPath,
		"-s", chinesePath,
		"-o", outputPath,
		"--model", cfg.Model,
		"--device", cfg.Device,
	}

	// Create context with timeout
	timeoutCtx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()

	// Execute DuoSubs
	cmd := exec.CommandContext(timeoutCtx, "duosubs", args...)
	output, err := cmd.CombinedOutput()

	if err != nil {
		// Check if it was a timeout
		if timeoutCtx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("duosubs timed out after %v", cfg.Timeout)
		}
		return fmt.Errorf("duosubs failed: %w\nOutput: %s", err, string(output))
	}

	logger.Infof("✅ DuoSubs completed: %s", outputPath)
	logger.Debugf("DuoSubs output: %s", string(output))

	return nil
}

// GetMergedOutputPath returns the expected output path for merged subtitle.
// DuoSubs creates an ASS file in the output directory.
func GetMergedOutputPath(outputDir, baseName string) string {
	return filepath.Join(outputDir, baseName+".ass")
}
