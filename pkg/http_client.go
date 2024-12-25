package pkg

import (
	"bytes"
	"io"
	"net/http"
	"time"

	"github.com/avast/retry-go"
	"go.uber.org/zap"
)

var retryOptions = []retry.Option{
	retry.Attempts(3),
	retry.Delay(1 * time.Second),
	retry.MaxJitter(1 * time.Second),
}

type loggingRoundTripper struct {
	next   http.RoundTripper
	logger *zap.SugaredLogger
}

func NewLoggingRoundTripper(logger *zap.SugaredLogger) http.RoundTripper {
	return loggingRoundTripper{
		next:   http.DefaultTransport,
		logger: logger,
	}
}

func (l loggingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// Log request
	body, _ := io.ReadAll(req.Body)
	req.Body = io.NopCloser(bytes.NewBuffer(body))
	l.logger.Infow("HTTP Request",
		"url", req.URL,
		"method", req.Method,
		"body", string(body),
	)

	// Execute request
	resp, err := l.next.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	// Log response
	respBody, _ := io.ReadAll(resp.Body)
	resp.Body = io.NopCloser(bytes.NewBuffer(respBody))
	l.logger.Infow("HTTP Response",
		"status", resp.Status,
		"body", string(respBody),
	)

	return resp, nil
}
