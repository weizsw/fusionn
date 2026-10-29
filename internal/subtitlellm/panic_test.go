package subtitlellm

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

	"go.uber.org/zap/zapcore"

	"github.com/fusionn/pkg/logger"
)

// panicTransport makes every request panic, standing in for a bug in the code
// that handles one window or one split-check batch.
type panicTransport struct{}

func (panicTransport) RoundTrip(*http.Request) (*http.Response, error) { panic("window bug") }

type lockedLogBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedLogBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedLogBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func capturePanicLogs(t *testing.T) *lockedLogBuffer {
	t.Helper()
	t.Setenv("LOG_LEVEL", "")
	buf := &lockedLogBuffer{}
	logger.Configure(logger.Config{Output: zapcore.AddSync(buf)})
	t.Cleanup(func() { logger.Configure(logger.Config{Output: zapcore.AddSync(&lockedLogBuffer{})}) })
	return buf
}

func requirePanicLine(t *testing.T, logs string) {
	t.Helper()
	for _, line := range strings.Split(logs, "\n") {
		if strings.Contains(line, "event=panic_recovered job_id=j1") {
			if !strings.Contains(line, `error="panic: window bug"`) || !strings.Contains(line, ` stack="`) {
				t.Errorf("panic line lacks error or stack: %s", line)
			}
			return
		}
	}
	t.Errorf("no panic_recovered line for job j1:\n%s", logs)
}

// A panic in an alignment worker is an unexpected internal failure: Align
// returns it, stack included, instead of ending the process.
func TestAlignRecoversWindowPanic(t *testing.T) {
	en, zh := testCues(1, 1000)
	bundle := testBundle(t, en, zh)
	cfg := testConfig(t, "http://alignment.invalid/v1")
	engine := &Engine{httpClient: &http.Client{Transport: panicTransport{}}}

	_, _, err := engine.Align(context.Background(), bundle, cfg)
	var pe *logger.PanicError
	if !errors.As(err, &pe) {
		t.Fatalf("Align error = %v, want the recovered panic", err)
	}
	if err.Error() != "panic: window bug" || !strings.Contains(pe.Stack, "panicTransport") {
		t.Errorf("error %q, stack lacks the panicking frame:\n%s", err, pe.Stack)
	}
}

// A panic in a split-check batch leaves its questions unanswered, as a failed
// request does.
func TestCheckSplitsRecoversBatchPanic(t *testing.T) {
	buf := capturePanicLogs(t)
	cfg := testConfig(t, "http://alignment.invalid/v1")
	cfg.EnChars, cfg.ZhChars = 40, 20
	bundle, mapping := londonInputs()
	engine := &Engine{httpClient: &http.Client{Transport: panicTransport{}}}

	checks, audit, _, _ := engine.checkSplits(logger.WithJobID(context.Background(), "j1"), bundle, mapping.Groups, cfg, t.TempDir())
	if len(checks) != 0 || audit.Unanswered != 1 || len(audit.Failures) != 1 || !strings.Contains(audit.Failures[0], "panic: window bug") {
		t.Fatalf("checks %+v audit %+v, want one unanswered question", checks, audit)
	}
	requirePanicLine(t, buf.String())
}
