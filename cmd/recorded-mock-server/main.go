package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	cacheDir := flag.String("cache-dir", "", "Path to recorded cache directory")
	failWindow := flag.Int("fail-window", -1, "Window index to fail (-1 for none)")
	failCode := flag.Int("fail-code", 503, "HTTP status code on failure")
	port := flag.Int("port", 3425, "Port to listen on (0 for dynamic/ephemeral)")
	statsOut := flag.String("stats-out", "", "File path to save stats JSON on exit")
	flag.Parse()

	if *cacheDir == "" {
		log.Fatalf("Error: -cache-dir is required")
	}

	server, err := NewReplayServer(*cacheDir, *failWindow, *failCode)
	if err != nil {
		log.Fatalf("Error creating replay server: %v", err)
	}

	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	httpServer := &http.Server{
		Addr:    addr,
		Handler: server.Handler(),
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-sigChan
		log.Printf("Shutting down replay server...")
		if *statsOut != "" {
			stats := server.GetStats()
			data, err := json.MarshalIndent(stats, "", "  ")
			if err == nil {
				_ = os.WriteFile(*statsOut, data, 0644)
				log.Printf("Saved stats to %s", *statsOut)
			}
		}
		_ = httpServer.Close()
		os.Exit(0)
	}()

	log.Printf("Recorded mock server listening on http://%s (cache: %s, fail-window: %d)", addr, *cacheDir, *failWindow)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server failed: %v", err)
	}
}
