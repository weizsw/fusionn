package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fusionn/pkg/logger"
)

func TestLoad(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
server:
  port: 9090
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if cfg.Server.Port != 9090 {
		t.Errorf("Expected port 9090, got %d", cfg.Server.Port)
	}
}

func TestNewManager(t *testing.T) {
	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
server:
  port: 8080
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	// Initialize logger before creating manager (required for logging in NewManager)
	logger.Init(true)
	defer logger.Sync()
	
	mgr, err := NewManager(configPath)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}
	defer mgr.Stop()

	cfg := mgr.Get()
	if cfg.Server.Port != 8080 {
		t.Errorf("Expected port 8080, got %d", cfg.Server.Port)
	}
}

func TestManagerHotReload(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping hot-reload test in short mode")
	}

	// Initialize logger
	logger.Init(true)
	defer logger.Sync()

	// Create a temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	initialConfig := `
server:
  port: 8080
`
	if err := os.WriteFile(configPath, []byte(initialConfig), 0644); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	mgr, err := NewManager(configPath)
	if err != nil {
		t.Fatalf("Failed to create manager: %v", err)
	}
	defer mgr.Stop()

	// Test initial config
	cfg := mgr.Get()
	if cfg.Server.Port != 8080 {
		t.Errorf("Expected initial port 8080, got %d", cfg.Server.Port)
	}

	// Set up change callback
	changed := make(chan bool, 1)
	mgr.OnChange(func(old, updated *Config) {
		changed <- true
	})

	// Modify config file
	updatedConfig := `
server:
  port: 9090
`
	time.Sleep(100 * time.Millisecond) // Ensure file timestamp differs
	if err := os.WriteFile(configPath, []byte(updatedConfig), 0644); err != nil {
		t.Fatalf("Failed to update test config: %v", err)
	}

	// Wait for change detection (with timeout)
	select {
	case <-changed:
		// Change detected
	case <-time.After(15 * time.Second):
		t.Fatal("Config change was not detected within timeout")
	}

	// Verify updated config
	cfg = mgr.Get()
	if cfg.Server.Port != 9090 {
		t.Errorf("Expected updated port 9090, got %d", cfg.Server.Port)
	}
}

