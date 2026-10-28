package duosubs

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/zap/zapcore"

	"github.com/fusionn/pkg/logger"
)

const canary = "CANARY_SUBTITLE_OR_MODEL_BODY"

func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	t.Setenv("LOG_LEVEL", "DEBUG")
	var buf bytes.Buffer
	logger.Configure(logger.Config{Output: zapcore.AddSync(&buf)})
	t.Cleanup(func() { logger.Configure(logger.Config{Output: zapcore.AddSync(&bytes.Buffer{})}) })
	return &buf
}

func newTestClient(t *testing.T, handler http.HandlerFunc) *HTTPClient {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return NewHTTPClient(HTTPConfig{BaseURL: server.URL, ContainerPrefix: "/data", HostPrefix: "/host"})
}

// Non-DEBUG lines are the ones a production operator sees; the remote body
// must never appear there.
func nonDebugLines(out string) string {
	var kept []string
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, " DEBUG ") {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

func TestMergeHTTPErrorBodyNeverLeaksOutsideDebug(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, canary)
	})
	buf := captureLogs(t)

	_, err := client.Merge(logger.WithJobID(context.Background(), "job-1"), "zh.srt", "en.srt", "/out")
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), canary) {
		t.Errorf("error text leaks the remote body: %v", err)
	}
	if strings.Contains(err.Error(), " 500") == false {
		t.Errorf("error should keep the safe status: %v", err)
	}
	if got := nonDebugLines(buf.String()); strings.Contains(got, canary) {
		t.Errorf("non-DEBUG lines leak the remote body: %s", got)
	}
	if !strings.Contains(buf.String(), " DEBUG ") || !strings.Contains(buf.String(), canary) {
		t.Errorf("expected one bounded DEBUG excerpt carrying the body: %s", buf.String())
	}
}

func TestMergeSuccessFalseBodyNeverLeaksOutsideDebug(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":false,"error":"`+canary+`"}`)
	})
	buf := captureLogs(t)

	_, err := client.Merge(context.Background(), "zh.srt", "en.srt", "/out")
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), canary) {
		t.Errorf("error text leaks the service error body: %v", err)
	}
	if got := nonDebugLines(buf.String()); strings.Contains(got, canary) {
		t.Errorf("non-DEBUG lines leak the service error body: %s", got)
	}
	if !strings.Contains(buf.String(), canary) {
		t.Errorf("expected the DEBUG excerpt: %s", buf.String())
	}
}

func TestMergeSendsRequestIDCorrelatedWithJob(t *testing.T) {
	var received string
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		received = r.Header.Get("X-Request-ID")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"success":true,"output_path":"/host/out/x_combined.ass"}`)
	})
	buf := captureLogs(t)

	out, err := client.Merge(logger.WithJobID(context.Background(), "job-7"), "zh.srt", "en.srt", "/out")
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if out != "/host/out/x_combined.ass" {
		t.Errorf("output path = %q", out)
	}
	if !logger.ValidRequestID(received) {
		t.Fatalf("the outbound request id is missing or invalid: %q", received)
	}

	var mapping string
	for _, line := range strings.Split(buf.String(), "\n") {
		if strings.Contains(line, "event=service_request") {
			mapping = line
		}
	}
	if mapping == "" {
		t.Fatalf("no service_request line in %s", buf.String())
	}
	for _, want := range []string{"job_id=job-7", "request_id=" + received} {
		if !strings.Contains(mapping, want) {
			t.Errorf("mapping line missing %q: %s", want, mapping)
		}
	}
}
