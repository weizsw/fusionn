package server

import (
	"fusionn/internal/middleware"
	"net/http"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func (s *Server) RegisterRoutes() http.Handler {
	r := gin.New()

	r.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"}, // Add your frontend URL
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "OPTIONS", "PATCH"},
		AllowHeaders:     []string{"Origin", "Accept", "Authorization", "Content-Type"},
		AllowCredentials: true, // Enable cookies/auth
	}))

	r.Use(gin.LoggerWithConfig(gin.LoggerConfig{
		SkipPaths: []string{"/health"}, // Skip logging for health checks
	}))
	r.Use(gin.Recovery())
	r.Use(middleware.RequestLogger())
	r.Use(middleware.ResponseLogger())
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
			statusCode := http.StatusBadRequest
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
