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

	"github.com/fusionn/internal/client/bazarr"
	"github.com/fusionn/internal/config"
	"github.com/fusionn/internal/handler"
	"github.com/fusionn/internal/queue"
	"github.com/fusionn/internal/service/subtitle"
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

	// Initialize Redis client (for translation queue)
	var redisQueueClient *queue.RedisClient
	if cfg.Subtitle.Enabled {
		var err error
		redisQueueClient, err = queue.NewRedisClient(cfg.Redis)
		if err != nil {
			logger.Warnf("⚠️  Redis connection failed: %v (translation queue disabled)", err)
			redisQueueClient = nil
		}
	}

	// Initialize Bazarr client (for subtitle search)
	var bazarrClient *bazarr.Client
	if cfg.Bazarr.Enabled {
		bazarrClient = bazarr.NewClient(cfg.Bazarr.URL, cfg.Bazarr.APIKey, cfg.Bazarr.SearchTimeout)
		logger.Infof("✅ Bazarr integration enabled: %s", cfg.Bazarr.URL)
	}

	// Initialize Merge Queue and Subtitle Service (if enabled)
	var mergeQueue *queue.MergeQueue
	var subtitleService *subtitle.Service
	if cfg.Subtitle.Enabled {
		// Create merge queue (will be started after subtitle service is created)
		mergeQueue = queue.NewMergeQueue(
			queue.Config{
				Workers:    cfg.Queue.Workers,
				QueueSize:  cfg.Queue.QueueSize,
				MaxRetries: cfg.Queue.MaxRetries,
			},
			nil, // Handler will be set after service is created
		)

		// Create subtitle service
		subtitleService, err = subtitle.NewService(cfg, redisQueueClient, mergeQueue, bazarrClient)
		if err != nil {
			logger.Fatalf("❌ Failed to initialize subtitle service: %v", err)
		}

		// Wire the queue handler now that the service exists
		mergeQueue.SetHandler(subtitleService.ProcessMergeJob)

		// Start queue workers
		mergeQueue.Start()

		logger.Infof("✅ Subtitle service initialized (queue: %d worker(s), %d max pending)",
			cfg.Queue.Workers, cfg.Queue.QueueSize)
	}

	// Initialize HTTP server
	if !isDev {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(requestLogger())

	// Register routes
	registerRoutes(router, cfg, subtitleService)

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
	logger.Infof("   GET  /health                       - Health check")
	logger.Infof("   GET  /api/v1/status                - Application status")
	if cfg.Subtitle.Enabled {
		logger.Infof("   POST /api/v1/webhook/sonarr        - Sonarr webhook")
		logger.Infof("   POST /api/v1/webhook/radarr        - Radarr webhook")
		logger.Infof("   POST /api/v1/callback/translation  - Translation callback")
	}
	logger.Info("")
	logger.Info("────────────────────────────────────────────────────────────────")
	if cfg.Subtitle.Enabled {
		logger.Info("✅  Ready! Subtitle automation enabled")
	} else {
		logger.Info("✅  Ready! Application is running...")
	}
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

	// Cleanup resources
	if mergeQueue != nil {
		mergeQueue.Stop()
	}

	if redisQueueClient != nil {
		if err := redisQueueClient.Close(); err != nil {
			logger.Errorf("❌ Redis close error: %v", err)
		}
	}

	logger.Info("👋 Goodbye!")
}

func registerRoutes(router *gin.Engine, cfg *config.Config, subtitleService *subtitle.Service) {
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

		// Webhook endpoints (if subtitle feature enabled)
		if cfg.Subtitle.Enabled && subtitleService != nil {
			webhookHandler := handler.NewWebhookHandler(subtitleService)
			api.POST("/webhook/sonarr", webhookHandler.HandleSonarr)
			api.POST("/webhook/radarr", webhookHandler.HandleRadarr)

			// Callback endpoint for fusionn-subs
			callbackHandler := handler.NewCallbackHandler(subtitleService)
			api.POST("/callback/translation", callbackHandler.HandleTranslationCallback)
		}
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
