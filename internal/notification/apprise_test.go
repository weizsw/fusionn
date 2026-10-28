package notification

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"syscall"
	"testing"

	"go.uber.org/zap/zapcore"

	"github.com/fusionn/pkg/logger"
)

const appriseCanary = "CANARY_APPRISE_RESPONSE_BODY"

func captureAppriseLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	t.Setenv("LOG_LEVEL", "DEBUG")
	var buf bytes.Buffer
	logger.Configure(logger.Config{Output: zapcore.AddSync(&buf)})
	t.Cleanup(func() { logger.Configure(logger.Config{Output: zapcore.AddSync(&bytes.Buffer{})}) })
	return &buf
}

func nonDebug(out string) string {
	var kept []string
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, " DEBUG ") {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

// A transport failure must not leak the configured key (which Resty embeds in
// the /notify/<key> path) and must not let Resty's own logger write raw lines.
func TestSendTransportFailureLeaksNeitherKeyNorRestyLines(t *testing.T) {
	const key = "REVIEW_ONLY_APPRISE_KEY_SECRET"

	// An abrupt disconnect after the request line: Resty retries and, before
	// the fix, printed WARN/ERROR RESTY lines carrying the request URL.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Errorf("hijack: %v", err)
			return
		}
		_ = conn.Close()
	}))
	defer server.Close()

	buf := captureAppriseLogs(t)
	stderr := captureProcessStderr(t)

	err := NewAppriseClient(server.URL, key, "tag").Send(
		logger.WithJobID(context.Background(), "job-9"), "t", "b", Success)
	if err == nil {
		t.Fatal("expected a transport error")
	}
	if strings.Contains(err.Error(), key) || strings.Contains(err.Error(), "/notify/") {
		t.Errorf("returned error leaks the key or request URL: %v", err)
	}
	// The transport cause stays classifiable even though the URL is stripped.
	if !errors.Is(err, io.EOF) && !errors.Is(err, syscall.ECONNRESET) {
		t.Errorf("transport cause lost: %v", err)
	}

	raw := stderr()
	if strings.Contains(raw, "RESTY") || strings.Contains(raw, key) {
		t.Errorf("Resty wrote an unformatted or key-bearing line: %q", raw)
	}

	logged := buf.String()
	if strings.Contains(logged, key) {
		t.Errorf("structured logs leak the key: %s", logged)
	}
	if !strings.Contains(logged, "job_id=job-9") {
		t.Errorf("transport detail lost the ctx job_id: %s", logged)
	}
	if !strings.Contains(logged, "notification_transport_detail") {
		t.Errorf("expected one DEBUG transport detail: %s", logged)
	}
}

// captureProcessStderr redirects os.Stderr for the duration of the test.
func captureProcessStderr(t *testing.T) func() string {
	t.Helper()
	original := os.Stderr
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stderr = writer
	t.Cleanup(func() { os.Stderr = original })
	return func() string {
		_ = writer.Close()
		data, _ := io.ReadAll(reader)
		os.Stderr = original
		return string(data)
	}
}

func TestSendStatusErrorSanitizesRemoteBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, appriseCanary)
	}))
	defer server.Close()
	buf := captureAppriseLogs(t)

	err := NewAppriseClient(server.URL, "key", "tag").Send(context.Background(), "t", "b", Success)
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), appriseCanary) {
		t.Errorf("error text leaks the remote body: %v", err)
	}
	if strings.Contains(err.Error(), "400") == false {
		t.Errorf("error should keep the safe status: %v", err)
	}
	if got := nonDebug(buf.String()); strings.Contains(got, appriseCanary) {
		t.Errorf("non-DEBUG lines leak the remote body: %s", got)
	}
	if !strings.Contains(buf.String(), appriseCanary) {
		t.Errorf("expected the DEBUG excerpt: %s", buf.String())
	}
}

func TestSendErrorFieldSanitized(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"error":"`+appriseCanary+`"}`)
	}))
	defer server.Close()
	buf := captureAppriseLogs(t)

	err := NewAppriseClient(server.URL, "key", "tag").Send(context.Background(), "t", "b", Success)
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), appriseCanary) {
		t.Errorf("error text leaks the Apprise error field: %v", err)
	}
	if got := nonDebug(buf.String()); strings.Contains(got, appriseCanary) {
		t.Errorf("non-DEBUG lines leak the Apprise error field: %s", got)
	}
	if !strings.Contains(buf.String(), appriseCanary) {
		t.Errorf("expected the DEBUG excerpt: %s", buf.String())
	}
}
