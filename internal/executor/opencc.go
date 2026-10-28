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
	args := []string{
		"-i", inputPath,
		"-o", outputPath,
		"-c", configName,
	}

	cmd := exec.CommandContext(ctx, "opencc", args...)
	stdout, stderr := &boundedTail{}, &boundedTail{}
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	start := toolStarted(ctx, "opencc", cmd)
	err := cmd.Run()
	diag := stderr.Diagnostic() + stdout.Diagnostic()
	toolFinished(ctx, "opencc", start, err, diag)

	toolLogger().Debug(ctx, "tool_output", "Tool output", logger.Preview(summarize(diag)))

	if err != nil {
		return fmt.Errorf("opencc failed: %w", err)
	}

	// Verify output file exists
	if _, err := os.Stat(outputPath); os.IsNotExist(err) {
		return fmt.Errorf("opencc did not create output file: %s", outputPath)
	}

	return nil
}
