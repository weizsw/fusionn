package executor

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/fusionn/pkg/logger"
)

// tailLimit bounds subprocess diagnostic capture: enough for a failure
// summary, never unbounded. The pipe stays drained, so filtering can never
// block the child process.
const tailLimit = 8 << 10

// summaryCap caps a diagnostic excerpt in the log, matching the shared
// preview limit (512 Unicode characters).
const summaryCap = 512

func toolLogger() logger.Logger { return logger.Component("executor") }

// boundedTail keeps the last tailLimit bytes written to it. Writes never
// fail and never block.
type boundedTail struct {
	mu      sync.Mutex
	buf     []byte
	trimmed bool
}

func (b *boundedTail) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	if len(b.buf) > tailLimit {
		b.buf = append(b.buf[:0], b.buf[len(b.buf)-tailLimit:]...)
		b.trimmed = true
	}
	return len(p), nil
}

func (b *boundedTail) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf)
}

// Diagnostic returns the retained text for logging. When the buffer dropped
// its head, the first (incomplete) line is discarded: it may begin inside a
// credential, leaving a suffix the redactor cannot recognize.
func (b *boundedTail) Diagnostic() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.trimmed {
		return string(b.buf)
	}
	if idx := bytes.IndexByte(b.buf, '\n'); idx >= 0 {
		return string(b.buf[idx+1:])
	}
	return ""
}

// summarize returns a bounded diagnostic excerpt for the error field.
func summarize(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	n := 0
	for i := range s {
		if n == summaryCap {
			return s[:i] + "…"
		}
		n++
	}
	return s
}

// toolStarted emits the INFO start event and returns the start time. The
// command line is only ever a capped DEBUG preview.
func toolStarted(ctx context.Context, tool string, cmd *exec.Cmd) time.Time {
	toolLogger().Info(ctx, "tool_started", "Tool started", zap.String("tool", tool))
	if cmd != nil {
		toolLogger().Debug(ctx, "tool_invocation", "Tool invocation",
			zap.String("tool", tool), logger.Preview(cmd.String()))
	}
	return time.Now()
}

// toolFinished emits tool_completed with exit code and duration. A non-zero
// exit is a WARN whose error is the tool's own exit error and whose detail
// holds the error lines of the tool output (see logger.FailureDetail), so the
// cause shows without DEBUG. The whole output is only ever a bounded DEBUG
// preview. The returned error value is never altered here, so existing
// failure detection and messages stay intact.
func toolFinished(ctx context.Context, tool string, start time.Time, err error, diag string) {
	fields := []zap.Field{
		zap.String("tool", tool),
		logger.DurMs(time.Since(start)),
		zap.Int("exit_code", exitCode(err)),
	}
	if err == nil {
		toolLogger().Info(ctx, "tool_completed", "Tool completed", fields...)
		return
	}
	fields = append(fields, zap.Error(err), logger.FailureDetail(diag))
	toolLogger().Warn(ctx, "tool_completed", "Tool failed", fields...)
	if d := summarize(diag); d != "" {
		// The whole output is a separate DEBUG event with a bounded preview.
		toolLogger().Debug(ctx, "tool_failure_detail", "Tool failure detail",
			zap.String("tool", tool), logger.Preview(d))
	}
}

// exitCode reports a process exit code, or -1 when the tool could not start.
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}
