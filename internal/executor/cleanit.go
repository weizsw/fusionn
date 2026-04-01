package executor

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/fusionn/pkg/logger"
)

// RunCleanit runs cleanit with the no-sdh tag on an SRT file (modifies in-place).
func RunCleanit(ctx context.Context, srtPath string) error {
	cmd := exec.CommandContext(ctx,
		"cleanit",
		"-t", "no-sdh",
		srtPath,
	)

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	logger.Debugf("Executing: %s", cmd.String())

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("cleanit failed: %w (stderr: %s)", err, stderr.String())
	}

	return nil
}

// IsCleanitAvailable checks if cleanit binary exists in PATH.
func IsCleanitAvailable() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "cleanit", "--version").Run() == nil
}
