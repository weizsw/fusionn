package version

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"go.uber.org/zap/zapcore"

	"github.com/fusionn/pkg/logger"
)

func TestBanner(t *testing.T) {
	banner := Banner()
	if banner == "" {
		t.Error("Banner should not be empty")
	}
	// The banner is ASCII art, check for key characters
	if len(banner) < 10 {
		t.Error("Banner should be substantial in size")
	}
}

func TestPrintBanner(t *testing.T) {
	var buf bytes.Buffer
	PrintBanner(&buf, "INFO")

	output := buf.String()
	if output == "" {
		t.Error("PrintBanner output should not be empty")
	}
	if !strings.Contains(output, "fusionn") {
		t.Error("Output should contain 'fusionn'")
	}
	if !strings.Contains(output, Version) {
		t.Errorf("Output should contain version %s", Version)
	}
	if !strings.Contains(output, "Log level: INFO") {
		t.Errorf("Output should contain the log level line:\n%s", output)
	}
}

// The banner shows the level the logger is actually running at: the resolved
// LOG_LEVEL, WARNING written as WARN, and INFO after an invalid value falls
// back.
func TestPrintBannerReportsEffectiveLogLevel(t *testing.T) {
	previous := logger.Log
	t.Cleanup(func() { logger.Log = previous })

	cases := []struct {
		name     string
		logLevel string
		want     string
	}{
		{"default", "", "INFO"},
		{"debug lowercase", "debug", "DEBUG"},
		{"info", "INFO", "INFO"},
		{"error", "ERROR", "ERROR"},
		{"warning alias", "WARNING", "WARN"},
		{"invalid falls back to info", "verbose", "INFO"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("LOG_LEVEL", tc.logLevel)
			// Real configuration; the log output itself is discarded.
			logger.Configure(logger.Config{Output: zapcore.AddSync(io.Discard)})

			var buf bytes.Buffer
			PrintBanner(&buf, logger.Log.Level().CapitalString())

			output := buf.String()
			if !strings.Contains(output, "Log level: "+tc.want) {
				t.Errorf("LOG_LEVEL=%q: banner should report %s:\n%s", tc.logLevel, tc.want, output)
			}
		})
	}
}
