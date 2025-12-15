package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/fusionn/internal/config"
	"github.com/fusionn/internal/version"
	"github.com/fusionn/pkg/logger"
)

func main() {
	// Initialize logger
	isDev := os.Getenv("ENV") != "production"
	logger.Init(isDev)
	defer logger.Sync()

	version.PrintBanner(nil)

	// Load configuration
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "config/config.yaml"
	}

	logger.Infof("📁 Loading config: %s", configPath)
	cfgMgr, err := config.NewManager(configPath)
	if err != nil {
		logger.Fatalf("❌ Config error: %v", err)
	}
	defer cfgMgr.Stop()
	cfg := cfgMgr.Get()

	// Initialize HTTP server
	if !isDev {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(requestLogger())

	// Register routes
	registerRoutes(router)

	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.Server.Port),
		Handler:      router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatalf("❌ Server error: %v", err)
		}
	}()

	// Print startup info
	logger.Info("")
	logger.Infof("🌐 API server: http://localhost:%d", cfg.Server.Port)
	logger.Infof("   GET  /health              - Health check")
	logger.Infof("   GET  /api/v1/status       - Application status")
	logger.Info("")
	logger.Info("────────────────────────────────────────────────────────────────")
	logger.Info("✅  Ready! Application is running...")
	logger.Info("────────────────────────────────────────────────────────────────")

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("")
	logger.Info("🛑 Shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		logger.Errorf("❌ Shutdown error: %v", err)
	}

	logger.Info("👋 Goodbye!")
}

func registerRoutes(router *gin.Engine) {
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	api := router.Group("/api/v1")
	{
		api.GET("/status", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{
				"version": version.Version,
				"status":  "running",
			})
		})
	}
}

// requestLogger returns a gin middleware for logging HTTP requests
func requestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path

		c.Next()

		status := c.Writer.Status()
		if path != "/health" || status >= 400 {
			latency := time.Since(start)
			logger.Debugf("HTTP %s %s → %d (%v)", c.Request.Method, path, status, latency)
		}
	}
}
