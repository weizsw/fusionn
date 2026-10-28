package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap/zapcore"

	"github.com/fusionn/pkg/logger"
)

func setupRouterLogger(t *testing.T) *bytes.Buffer {
	t.Helper()
	t.Setenv("LOG_LEVEL", "")
	var buf bytes.Buffer
	logger.Configure(logger.Config{Output: zapcore.AddSync(&buf)})
	t.Cleanup(func() { logger.Configure(logger.Config{Output: zapcore.AddSync(&bytes.Buffer{})}) })
	gin.SetMode(gin.TestMode)
	gin.DefaultWriter = io.Discard
	gin.DefaultErrorWriter = io.Discard
	return &buf
}

func TestRequestLoggerSummaryAndRequestID(t *testing.T) {
	buf := setupRouterLogger(t)
	router := gin.New()
	router.Use(recoveryLogger())
	router.Use(requestLogger())
	router.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	router.POST("/api/v1/webhook/sonarr", func(c *gin.Context) { c.Status(http.StatusAccepted) })

	// A successful health check stays at DEBUG (default INFO suppresses it).
	health := httptest.NewRecorder()
	router.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/health", nil))
	if strings.Contains(buf.String(), "request_completed") {
		t.Errorf("health summary must be DEBUG: %q", buf.String())
	}
	if !logger.ValidRequestID(health.Header().Get("X-Request-ID")) {
		t.Errorf("generated request ID missing or invalid: %q", health.Header().Get("X-Request-ID"))
	}

	// An invalid incoming ID is replaced, echoed and correlated in the summary.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhook/sonarr", nil)
	req.Header.Set("X-Request-ID", "bad id!!")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	out := buf.String()
	echoed := rec.Header().Get("X-Request-ID")
	for _, want := range []string{
		"event=request_completed",
		`route="/api/v1/webhook/sonarr"`,
		"status=202",
		"method=POST",
		"duration_ms=",
		"request_id=" + echoed,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in %q", want, out)
		}
	}
	if !logger.ValidRequestID(echoed) {
		t.Errorf("echoed request ID invalid: %q", echoed)
	}
}

// Concurrent requests must not cross request IDs in the summary lines.
func TestRequestLoggerConcurrentIsolation(t *testing.T) {
	buf := setupRouterLogger(t)
	router := gin.New()
	router.Use(requestLogger())
	router.POST("/api/v1/webhook/sonarr", func(c *gin.Context) { c.Status(http.StatusAccepted) })

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/webhook/sonarr", nil)
			req.Header.Set("X-Request-ID", id)
			router.ServeHTTP(httptest.NewRecorder(), req)
		}(fmt.Sprintf("req-%d", i))
	}
	wg.Wait()

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 8 {
		t.Fatalf("expected 8 summary lines, got %d: %q", len(lines), buf.String())
	}
	seen := map[string]bool{}
	for _, line := range lines {
		var matched []string
		for i := 0; i < 8; i++ {
			id := fmt.Sprintf("req-%d", i)
			if strings.Contains(line, "request_id="+id) {
				matched = append(matched, id)
			}
		}
		if len(matched) != 1 {
			t.Fatalf("line has %d request IDs (%v): %s", len(matched), matched, line)
		}
		seen[matched[0]] = true
	}
	if len(seen) != 8 {
		t.Errorf("expected 8 distinct request IDs, got %v", seen)
	}
}

func TestRecoveryLogsOneEscapedStack(t *testing.T) {
	buf := setupRouterLogger(t)
	router := gin.New()
	router.Use(recoveryLogger())
	router.GET("/panic", func(c *gin.Context) { panic("boom") })

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/panic", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("panic must produce 500, got %d", rec.Code)
	}

	out := strings.TrimRight(buf.String(), "\n")
	if strings.Count(out, "event=request_panic") != 1 {
		t.Errorf("expected exactly one recovery event: %q", out)
	}
	if !strings.Contains(out, "ERROR") || !strings.Contains(out, `stack="`) || !strings.Contains(out, "recoveryLogger") {
		t.Errorf("recovery line missing level/stack: %q", out)
	}
	if strings.Contains(out, "\n") || strings.Contains(out, "\t") {
		t.Errorf("stack must be escaped onto one physical line: %q", out)
	}
}
