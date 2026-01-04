package executor

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/fusionn/pkg/logger"
)

// ConvertOpenCC converts Traditional Chinese to Simplified Chinese using OpenCC.
func ConvertOpenCC(ctx context.Context, inputPath, outputPath, configName string) error {
	logger.Infof("Running OpenCC: %s → %s (config: %s)", inputPath, outputPath, configName)

	// opencc -i <input> -o <output> -c <config>
	args := []string{
		"-i", inputPath,
		"-o", outputPath,
		"-c", configName,
	}

	cmd := exec.CommandContext(ctx, "opencc", args...)
	output, err := cmd.CombinedOutput()

	if err != nil {
		return fmt.Errorf("opencc failed: %w\nOutput: %s", err, string(output))
	}

	// Verify output file exists
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		return fmt.Errorf("opencc did not create output file: %s", outputPath)
	}

	logger.Infof("✅ OpenCC conversion completed: %s", outputPath)
	logger.Debugf("OpenCC output: %s", string(output))

	return nil
}
