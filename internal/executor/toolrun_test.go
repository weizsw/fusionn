package executor

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"go.uber.org/zap/zapcore"

	"github.com/fusionn/pkg/logger"
)

func TestBoundedTailKeepsLastBytes(t *testing.T) {
	tail := &boundedTail{}
	chunk := bytes.Repeat([]byte("a"), 4096)
	for i := 0; i < 8; i++ {
		if _, err := tail.Write(chunk); err != nil {
			t.Fatal(err)
		}
	}
	if got := len(tail.String()); got != tailLimit {
		t.Errorf("tail length = %d, want %d", got, tailLimit)
	}
	// A partial write without any newline must be kept too.
	partial := &boundedTail{}
	if _, err := partial.Write([]byte("no trailing newline")); err != nil {
		t.Fatal(err)
	}
	if partial.String() != "no trailing newline" {
		t.Errorf("partial line lost: %q", partial.String())
	}
}

func TestSummarizeBounded(t *testing.T) {
	long := strings.Repeat("字", 600)
	got := summarize(long)
	if n := len([]rune(got)); n != summaryCap+1 { // 512 runes plus the ellipsis
		t.Errorf("summarize length = %d runes, want %d", n, summaryCap+1)
	}
	if summarize("  short  ") != "short" {
		t.Errorf("summarize must trim: %q", summarize("  short  "))
	}
	if summarize("") != "" {
		t.Errorf("summarize of empty text must stay empty")
	}
}

func TestExitCode(t *testing.T) {
	if exitCode(nil) != 0 {
		t.Error("nil error must map to exit code 0")
	}
	err := exec.Command("sh", "-c", "exit 7").Run()
	if exitCode(err) != 7 {
		t.Errorf("exit code = %d, want 7", exitCode(err))
	}
	if exitCode(errors.New("start failed")) != -1 {
		t.Error("start failure must map to -1")
	}
}

// The bounded capture must not change failure evidence: the original error is
// returned untouched and the retained tail still carries the text the failure
// detection relies on.
func TestToolFailureKeepsDetectionText(t *testing.T) {
	var buf bytes.Buffer
	logger.Configure(logger.Config{Output: zapcore.AddSync(&buf)})
	t.Cleanup(func() { logger.Configure(logger.Config{Output: zapcore.AddSync(&bytes.Buffer{})}) })

	ctx := logger.WithJobID(context.Background(), "job-1")
	cmd := exec.CommandContext(ctx, "sh", "-c", `printf 'Traceback (most recent call last):\nError: boom\n' >&2; exit 3`)
	tail := &boundedTail{}
	cmd.Stderr = tail
	start := toolStarted(ctx, "failing-tool", cmd)
	err := cmd.Run()
	toolFinished(ctx, "failing-tool", start, err, tail.String())

	if err == nil {
		t.Fatal("expected the command to fail")
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 3 {
		t.Fatalf("original error changed: %v", err)
	}
	if !strings.Contains(tail.String(), "Error:") || !strings.Contains(tail.String(), "Traceback") {
		t.Errorf("failure detection text lost: %q", tail.String())
	}

	out := buf.String()
	for _, want := range []string{
		"event=tool_started", "event=tool_completed", "tool=failing-tool",
		"exit_code=3", "duration_ms=", "job_id=job-1", "WARN",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %q", want, out)
		}
	}
	if strings.Contains(out, "\n\n") {
		t.Errorf("tool events must be single lines: %q", out)
	}
}

// Output that does not look like an error never reaches the WARN line; the
// whole tail appears only as a bounded DEBUG preview, and a trimmed tail drops
// its partial first line.
func TestToolFailureHidesRawTailAtWarn(t *testing.T) {
	var buf bytes.Buffer
	logger.Configure(logger.Config{Output: zapcore.AddSync(&buf)})
	t.Cleanup(func() { logger.Configure(logger.Config{Output: zapcore.AddSync(&bytes.Buffer{})}) })

	ctx := context.Background()
	cmd := exec.CommandContext(ctx, "sh", "-c", `printf 'RAW_THIRD_PARTY_TEXT\n' >&2; exit 1`)
	tail := &boundedTail{}
	cmd.Stderr = tail
	start := toolStarted(ctx, "quiet-tool", cmd)
	err := cmd.Run()
	toolFinished(ctx, "quiet-tool", start, err, tail.Diagnostic())

	if strings.Contains(buf.String(), "RAW_THIRD_PARTY_TEXT") {
		t.Errorf("raw third-party text leaked into the WARN line: %q", buf.String())
	}
	if !strings.Contains(buf.String(), "exit_code=1") {
		t.Errorf("failure line missing exit code: %q", buf.String())
	}

	// At DEBUG the same text is a bounded preview.
	buf.Reset()
	t.Setenv("LOG_LEVEL", "DEBUG")
	logger.Configure(logger.Config{Output: zapcore.AddSync(&buf)})
	toolFinished(ctx, "quiet-tool", start, err, tail.Diagnostic())
	if !strings.Contains(buf.String(), `event=tool_failure_detail`) ||
		!strings.Contains(buf.String(), `preview="RAW_THIRD_PARTY_TEXT`) {
		t.Errorf("DEBUG detail preview missing: %q", buf.String())
	}
}

func TestBoundedTailDiagnosticDropsPartialHeadLine(t *testing.T) {
	tail := &boundedTail{}
	// One write past the limit; the head is cut inside the first line, so the
	// partial "PPP..." line must be dropped and the complete line kept.
	payload := strings.Repeat("P", 60) + "\n" + strings.Repeat("C", tailLimit+40-61)
	if _, err := tail.Write([]byte(payload)); err != nil {
		t.Fatal(err)
	}
	diagnostic := tail.Diagnostic()
	if strings.HasPrefix(diagnostic, "P") {
		t.Errorf("partial head line kept: %q", diagnostic[:40])
	}
	if !strings.HasPrefix(diagnostic, "CCC") {
		t.Errorf("complete line after the cut must remain: %q", diagnostic[:40])
	}
}

func TestToolSuccessIsInfo(t *testing.T) {
	var buf bytes.Buffer
	logger.Configure(logger.Config{Output: zapcore.AddSync(&buf)})
	t.Cleanup(func() { logger.Configure(logger.Config{Output: zapcore.AddSync(&bytes.Buffer{})}) })

	ctx := context.Background()
	cmd := exec.CommandContext(ctx, "sh", "-c", "true")
	start := toolStarted(ctx, "ok-tool", cmd)
	err := cmd.Run()
	toolFinished(ctx, "ok-tool", start, err, "")

	out := buf.String()
	if strings.Contains(out, "WARN") || !strings.Contains(out, "exit_code=0") {
		t.Errorf("successful tool must log INFO with exit_code=0: %q", out)
	}
}

// The error lines of a failed tool reach the WARN line as a bounded detail, so
// the cause shows without DEBUG; other output stays out of it.
func TestToolFailureShowsErrorLinesAtWarn(t *testing.T) {
	var buf bytes.Buffer
	logger.Configure(logger.Config{Output: zapcore.AddSync(&buf)})
	t.Cleanup(func() { logger.Configure(logger.Config{Output: zapcore.AddSync(&bytes.Buffer{})}) })

	ctx := context.Background()
	cmd := exec.CommandContext(ctx, "sh", "-c",
		`printf 'frame=100 progress\nError opening input: Invalid data found when processing input\n' >&2; exit 1`)
	tail := &boundedTail{}
	cmd.Stderr = tail
	start := toolStarted(ctx, "ffmpeg", cmd)
	err := cmd.Run()
	toolFinished(ctx, "ffmpeg", start, err, tail.Diagnostic())

	var warn string
	for _, line := range strings.Split(buf.String(), "\n") {
		if strings.Contains(line, "WARN component=executor event=tool_completed") {
			warn = line
		}
	}
	if !strings.Contains(warn, ` detail="Error opening input: Invalid data found when processing input" `) {
		t.Errorf("WARN line lacks the error detail: %q", warn)
	}
	if strings.Contains(warn, "frame=100") {
		t.Errorf("non-error output reached the WARN line: %q", warn)
	}
}
