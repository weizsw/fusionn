package middleware

import (
	"bytes"
	"io"
	"log"

	"github.com/gin-gonic/gin"
)

// RequestLogger logs the incoming request details
func RequestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Read the body
		body, err := c.GetRawData()
		if err == nil {
			// Log the body - adjust logging method as needed
			// Using standard log for example
			log.Printf("Request: %s %s - Body: %s",
				c.Request.Method,
				c.Request.URL.Path,
				string(body))

			// Restore the body
			c.Request.Body = io.NopCloser(bytes.NewBuffer(body))
		}
		c.Next()
	}
}
