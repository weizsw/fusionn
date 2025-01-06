package server

import (
	"fusionn/config"
	"fusionn/internal/middleware"
	"fusionn/logger"
	"net/http"
	"time"

	"github.com/getsentry/sentry-go"
	sentrygin "github.com/getsentry/sentry-go/gin"
	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func (s *Server) RegisterRoutes() http.Handler {
	if err := sentry.Init(sentry.ClientOptions{
		Dsn:              config.C.Sentry.Dsn,
		EnableTracing:    config.C.Sentry.Enabled,
		TracesSampleRate: config.C.Sentry.SampleRate,
	}); err != nil {
		logger.L.Error("[Sentry] initialization failed", zap.Error(err))
	}
	r := gin.New()

	r.Use(gin.Recovery())
	r.Use(sentrygin.New(sentrygin.Options{
		Repanic:         true,
		WaitForDelivery: true,
		Timeout:         5 * time.Second,
	}))
	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"}, // Add your frontend URL
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH"},
		AllowHeaders:     []string{"Origin", "Accept", "Authorization", "Content-Type"},
		AllowCredentials: true, // Enable cookies/auth
	}))

	r.Use(gin.LoggerWithConfig(gin.LoggerConfig{
		SkipPaths: []string{"/health"}, // Skip logging for health checks
	}))
	r.Use(middleware.RequestLogger())
	r.Use(middleware.ResponseLogger())
	r.Use(RecoverWithSentry())
	r.GET("/", s.HelloWorldHandler)

	r.GET("/health", s.healthHandler)

	r.POST("/api/v1/merge", wrapHandler(s.mergeHandler.Merge))
	r.POST("/api/v1/batch", wrapHandler(s.batchHandler.Batch))
	r.POST("/api/v1/async_merge", wrapHandler(s.asyncMergeHandler.AsyncMerge))
	return r
}

// HandlerFunc is a custom type that returns an error
type HandlerFunc func(*gin.Context) error

// wrapHandler converts our HandlerFunc to gin.HandlerFunc
func wrapHandler(h HandlerFunc) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := h(c); err != nil {
			// Capture error in Sentry with request context
			if hub := sentrygin.GetHubFromContext(c); hub != nil {
				hub.CaptureException(err)
			}

			statusCode := http.StatusBadRequest
			if serr, ok := err.(interface{ StatusCode() int }); ok {
				statusCode = serr.StatusCode()
			}

			c.JSON(statusCode, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "success"})
	}
}

func (s *Server) HelloWorldHandler(c *gin.Context) {
	resp := make(map[string]string)
	resp["message"] = "Hello World"

	c.JSON(http.StatusOK, resp)
}

func (s *Server) healthHandler(c *gin.Context) {
	c.JSON(http.StatusOK, s.db.Health())
}

// Add this helper function to capture panics in your handlers
func RecoverWithSentry() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				if hub := sentrygin.GetHubFromContext(c); hub != nil {
					hub.Recover(err)
				}
				c.AbortWithStatus(http.StatusInternalServerError)
			}
		}()
		c.Next()
	}
}
