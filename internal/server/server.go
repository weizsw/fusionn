package server

import (
	"fmt"
	"net/http"
	"time"

	"github.com/google/wire"
	_ "github.com/joho/godotenv/autoload"

	"fusionn/internal/database"
	"fusionn/internal/handler"
	"fusionn/logger"
)

type Server struct {
	port         int
	mergeHandler *handler.MergeHandler
	batchHandler *handler.BatchHandler
	db           database.Service
}

func NewServer(db database.Service, h *handler.MergeHandler, b *handler.BatchHandler) *http.Server {
	NewServer := &Server{
		port:         4664,
		db:           db,
		mergeHandler: h,
		batchHandler: b,
	}

	logger.Sugar.Info("Server initialized")
	// Declare Server config
	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", NewServer.port),
		Handler:      NewServer.RegisterRoutes(),
		IdleTimeout:  time.Minute,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	return server
}

var Set = wire.NewSet(NewServer)
