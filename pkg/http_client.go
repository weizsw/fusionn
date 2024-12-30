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
	logger *zap.Logger
}

func NewLoggingRoundTripper(logger *zap.Logger) http.RoundTripper {
	return loggingRoundTripper{
		next:   http.DefaultTransport,
		logger: logger,
	}
}

func (l loggingRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	// Create a copy of the body for logging
	var bodyBytes []byte
	if req.Body != nil {
		bodyBytes, _ = io.ReadAll(req.Body)
		req.Body.Close()
		// Create new ReadCloser for both logging and the actual request
		req.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
	}

	l.logger.Info("[HTTP Request]",
		zap.String("url", req.URL.String()),
		zap.String("method", req.Method),
		zap.String("body", string(bodyBytes)),
	)

	// Execute request
	resp, err := l.next.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	// Create a copy of response body for logging
	var respBodyBytes []byte
	if resp.Body != nil {
		respBodyBytes, _ = io.ReadAll(resp.Body)
		resp.Body.Close()
		// Create new ReadCloser for both logging and the client
		resp.Body = io.NopCloser(bytes.NewBuffer(respBodyBytes))
	}

	l.logger.Info("[HTTP Response]",
		zap.String("status", resp.Status),
		zap.String("body", string(respBodyBytes)),
	)

	return resp, nil
}
