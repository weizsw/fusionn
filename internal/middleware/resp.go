package middleware

import (
	"bytes"
	"fusionn/logger"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// ResponseLogger captures and logs the response
func ResponseLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Create a buffer to store the response
		responseBuffer := &responseWriter{
			ResponseWriter: c.Writer,
			body:           &bytes.Buffer{},
		}
		c.Writer = responseBuffer

		// Process request
		c.Next()

		// Use zap logger instead of standard log
		logger.L.Info("[outgoing response]",
			zap.String("method", c.Request.Method),
			zap.String("path", c.Request.URL.Path),
			zap.Int("status", responseBuffer.Status()),
			zap.String("body", responseBuffer.body.String()))
	}
}

// responseWriter captures the response body and status
type responseWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

// Write captures the response body
func (w *responseWriter) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

// WriteString captures the response string
func (w *responseWriter) WriteString(s string) (int, error) {
	w.body.WriteString(s)
	return w.ResponseWriter.WriteString(s)
}
