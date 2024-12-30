package middleware

import (
	"bytes"
	"fusionn/logger"
	"io"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// RequestLogger logs the incoming request details
func RequestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		body, err := c.GetRawData()
		if err == nil {
			// Use zap logger instead of standard log
			logger.L.Info("[incoming request]",
				zap.String("method", c.Request.Method),
				zap.String("path", c.Request.URL.Path),
				zap.String("body", string(body)))

			// Restore the body
			c.Request.Body = io.NopCloser(bytes.NewBuffer(body))
		}
		c.Next()
	}
}
