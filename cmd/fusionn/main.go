package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/fusionn/internal/config"
	"github.com/fusionn/internal/handler"
	"github.com/fusionn/internal/queue"
	"github.com/fusionn/internal/service/subtitle"
	"github.com/fusionn/internal/version"
	"github.com/fusionn/pkg/logger"
)

func main() {
	os.Exit(run())
}

func run() int {
	isDev := os.Getenv("ENV") != "production"
	logger.Configure(logger.Config{})
	defer logger.Sync()

	// The banner is the one deliberate exception to the single-line rule: it
	// prints once per startup, after the logger is configured, and shows the
	// level actually in effect.
	version.PrintBanner(nil, logger.Log.Level().CapitalString())

	log := logger.Component("main")
	log.Info(context.Background(), "service_started", "Service started", zap.String("version", version.Version))

	// Load configuration
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "config/config.yaml"
	}

	log.Info(context.Background(), "config_loading", "Loading configuration", zap.String("path", configPath))
	cfgMgr, err := config.NewManager(configPath)
	if err != nil {
		log.Error(context.Background(), "service_failed", "Config error", zap.String("stage", "config"), zap.Error(err))
		return 1
	}
	defer cfgMgr.Stop()
	cfg := cfgMgr.Get()
	logger.SetConfigLevel(cfg.LogLevel)
	cfgMgr.OnChange(func(old, updated *config.Config) {
		logger.SetConfigLevel(updated.LogLevel)
		if old.Server != updated.Server || old.Redis != updated.Redis || old.Queue != updated.Queue ||
			old.Subtitle.Enabled != updated.Subtitle.Enabled {
			log.Warn(context.Background(), "config_restart_required", "Configuration change requires a restart")
		}
	})

	// Initialize Redis client (for translation queue)
	var redisQueueClient *queue.RedisClient
	if cfg.Subtitle.Enabled {
		var err error
		redisQueueClient, err = queue.NewRedisClient(cfg.Redis)
		if err != nil {
			log.Warn(context.Background(), "redis_unavailable", "Redis connection failed, translation queue disabled", zap.Error(err))
			redisQueueClient = nil
		}
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
		subtitleService, err = subtitle.NewReloadableService(cfgMgr, redisQueueClient, mergeQueue)
		if err != nil {
			log.Error(context.Background(), "service_failed", "Failed to initialize subtitle service", zap.String("stage", "init"), zap.Error(err))
			return 1
		}

		// Wire the queue handler now that the service exists
		mergeQueue.SetHandler(subtitleService.ProcessMergeJob)

		// Start queue workers
		mergeQueue.Start()

		log.Info(context.Background(), "service_ready", "Subtitle service initialized",
			zap.Int("workers", cfg.Queue.Workers), zap.Int("queue_size", cfg.Queue.QueueSize))
	}

	// Initialize HTTP server
	if !isDev {
		gin.SetMode(gin.ReleaseMode)
	}

	// Gin must not print its own route/debug/panic output: every line is
	// rendered by the unified logger (routes_registered and request_panicked
	// below).
	gin.DefaultWriter = io.Discard
	gin.DefaultErrorWriter = io.Discard

	router := newRouter()

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
			log.Error(context.Background(), "service_failed", "Server error", zap.String("stage", "listen"), zap.Error(err))
			os.Exit(1) //nolint:gocritic // best-effort exit from the listener goroutine
		}
	}()

	log.Info(context.Background(), "server_listening", "HTTP server listening",
		zap.Int("port", cfg.Server.Port), zap.Bool("subtitle_enabled", cfg.Subtitle.Enabled))
	log.Debug(context.Background(), "routes_registered", "Routes registered",
		logger.Preview("/health /api/v1/status /api/v1/webhook/sonarr /api/v1/webhook/radarr /api/v1/callback/translation"))

	// Wait for interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info(context.Background(), "shutdown_started", "Shutting down")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Error(context.Background(), "shutdown_failed", "Shutdown error", zap.Error(err))
	}

	// Cleanup resources
	if mergeQueue != nil {
		mergeQueue.Stop()
	}

	if redisQueueClient != nil {
		if err := redisQueueClient.Close(); err != nil {
			log.Error(context.Background(), "redis_close_failed", "Redis close error", zap.Error(err))
		}
	}

	log.Info(context.Background(), "service_stopped", "Service stopped")
	return 0
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

// requestLogger emits one end-of-request summary with method, normalized
// route, status, request_id and duration_ms. Successful health checks drop to
// DEBUG; 4xx are WARN and 5xx ERROR.
func requestLogger() gin.HandlerFunc {
	log := logger.Component("http")
	return func(c *gin.Context) {
		start := time.Now()

		// Validate or generate the request ID and carry it in the request
		// context so handler logs can correlate.
		requestID := c.GetHeader("X-Request-ID")
		if !logger.ValidRequestID(requestID) {
			requestID = logger.NewRequestID()
		}
		c.Header("X-Request-ID", requestID)
		c.Request = c.Request.WithContext(logger.WithRequestID(c.Request.Context(), requestID))

		c.Next()

		route := c.FullPath()
		if route == "" {
			// An unmatched request has no route pattern; its raw path is not
			// logged, because it can carry anything a client sends.
			route = "unmatched"
		}
		fields := []zap.Field{
			zap.String("method", c.Request.Method),
			zap.String("route", route),
			zap.Int("status", c.Writer.Status()),
			logger.DurMs(time.Since(start)),
		}
		status := c.Writer.Status()
		ctx := c.Request.Context()
		switch {
		case route == "/health" && status < 400:
			log.Debug(ctx, "request_completed", "Health check completed", fields...)
		case status >= 500 && handler.ErrorReported(c):
			// The failure is already logged once as ERROR; this line adds the
			// status and duration only.
			log.Warn(ctx, "request_completed", "Request failed", fields...)
		case status >= 500:
			log.Error(ctx, "request_completed", "Request failed", fields...)
		case status >= 400:
			log.Warn(ctx, "request_completed", "Request rejected", fields...)
		default:
			log.Info(ctx, "request_completed", "Request completed", fields...)
		}
	}
}

// newRouter builds the engine with the request middleware. The summary wraps
// the recovery, so a request that panics still leaves its one summary line.
func newRouter() *gin.Engine {
	router := gin.New()
	router.Use(requestLogger())
	router.Use(recoveryLogger())
	return router
}

// recoveryLogger replaces gin's default recovery (which writes raw text that
// bypasses the unified format): one ERROR event with an escaped, bounded
// stack, then the usual 500 abort, marked as reported for the summary.
func recoveryLogger() gin.HandlerFunc {
	log := logger.Component("http")
	return gin.CustomRecovery(func(c *gin.Context, recovered any) {
		log.Error(c.Request.Context(), "request_panicked", "Panic recovered",
			zap.Any("recovered", recovered), zap.String("stack", string(debug.Stack())))
		c.AbortWithStatus(http.StatusInternalServerError)
		handler.MarkErrorReported(c)
	})
}
