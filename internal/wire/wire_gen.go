// Code generated by Wire. DO NOT EDIT.

//go:generate go run -mod=mod github.com/google/wire/cmd/wire
//go:build !wireinject
// +build !wireinject

package wire

import (
	"fusionn/internal/cache"
	"fusionn/internal/database"
	"fusionn/internal/handler"
	"fusionn/internal/mq"
	"fusionn/internal/processor"
	"fusionn/internal/server"
	"fusionn/internal/service"
	"fusionn/pkg"
	"github.com/google/wire"
	"net/http"
)

// Injectors from wire.go:

// NewServer creates a new HTTP server with all its dependencies
func NewServer() (*http.Server, error) {
	databaseService := database.New()
	ffmpeg := service.NewFFMPEG()
	deepL := pkg.NewDeepL()
	convertor := service.NewConvertor(deepL)
	redisClient, err := cache.NewRedisClient()
	if err != nil {
		return nil, err
	}
	messageQueue := mq.NewMessageQueue(redisClient)
	tvdb := pkg.NewTVDB(redisClient)
	facade := service.NewFacade(redisClient, tvdb)
	parser := service.NewParser(convertor, ffmpeg, messageQueue, facade)
	algo := service.NewAlgo()
	apprise := pkg.NewApprise()
	extractStage := processor.NewExtractStage(ffmpeg)
	parseStage := processor.NewParseStage(parser)
	cleanStage := processor.NewCleanStage(parser)
	segMergeStage := processor.NewSegMergeStage(algo)
	styleService := service.NewStyleService()
	styleStage := processor.NewStyleStage(styleService, ffmpeg)
	exportStage := processor.NewExportStage()
	subsetStage := processor.NewSubsetStage(styleService)
	notiStage := processor.NewNotiStage(apprise)
	afterStage := processor.NewAfterStage(styleService, parser)
	mergePipeline := handler.ProvideMergePipeline(extractStage, parseStage, cleanStage, segMergeStage, styleStage, exportStage, subsetStage, notiStage, afterStage)
	mergeHandler := handler.NewMergeHandler(ffmpeg, parser, convertor, algo, apprise, mergePipeline)
	parseFileStage := processor.NewParseFileStage(parser)
	asyncMergePipeline := handler.ProvideAsyncMergePipeline(parseFileStage, segMergeStage, styleStage, exportStage, subsetStage, notiStage, afterStage)
	asyncMergeHandler := handler.NewAsyncMergeHandler(asyncMergePipeline)
	batchPipeline := handler.ProvideBatchPipeline(extractStage, parseStage, cleanStage, segMergeStage, styleStage, exportStage, subsetStage, afterStage)
	batchHandler := handler.NewBatchHandler(batchPipeline)
	httpServer := server.NewServer(databaseService, mergeHandler, asyncMergeHandler, batchHandler)
	return httpServer, nil
}

// wire.go:

// ServerSet is a Wire provider set that includes all server dependencies
var ServerSet = wire.NewSet(pkg.Set, service.Set, database.Set, handler.Set, server.Set, processor.Set, cache.Set, mq.Set)
