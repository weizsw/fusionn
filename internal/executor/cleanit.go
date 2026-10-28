package executor

import (
	"context"
	"fmt"
	"os/exec"
)

// RunCleanit runs cleanit with the no-sdh tag on an SRT file (modifies in-place).
func RunCleanit(ctx context.Context, srtPath string) error {
	cmd := exec.CommandContext(ctx,
		"cleanit",
		"-t", "no-sdh",
		srtPath,
	)

	tail := &boundedTail{}
	cmd.Stderr = tail

	start := toolStarted(ctx, "cleanit", cmd)
	err := cmd.Run()
	toolFinished(ctx, "cleanit", start, err, tail.Diagnostic())
	if err != nil {
		return fmt.Errorf("cleanit failed: %w", err)
	}

	return nil
}

// IsCleanitAvailable checks if cleanit binary exists in PATH.
func IsCleanitAvailable() bool {
	_, err := exec.LookPath("cleanit")
	return err == nil
}
