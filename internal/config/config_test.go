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

func TestSubtitleConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
server:
  port: 8080

subtitle:
  enabled: true
  duosubs:
    model: "sentence-transformers/LaBSE"
    device: "auto"
    timeout_minutes: 10
  output_same_dir: true
  english_variants: ["eng", "eng-sdh", "en"]
  chinese_variants: ["zh-Hans", "zh-CN", "chi", "zho"]
  opencc:
    enabled: true
    config: "t2s.json"

redis:
  host: "redis"
  port: 6379
  password: ""
  database: 0
  queue_key: "fusionn:translation:queue"

apprise:
  enabled: true
  base_url: "http://apprise:8000"
  key: "apprise"
  tag: "fusionn-subtitle"
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Test subtitle config
	if !cfg.Subtitle.Enabled {
		t.Error("Expected subtitle.enabled to be true")
	}
	if cfg.Subtitle.DuoSubs.Model != "sentence-transformers/LaBSE" {
		t.Errorf("Expected model 'sentence-transformers/LaBSE', got %s", cfg.Subtitle.DuoSubs.Model)
	}
	if cfg.Subtitle.DuoSubs.Device != "auto" {
		t.Errorf("Expected device 'auto', got %s", cfg.Subtitle.DuoSubs.Device)
	}
	if cfg.Subtitle.DuoSubs.TimeoutMinutes != 10 {
		t.Errorf("Expected timeout 10, got %d", cfg.Subtitle.DuoSubs.TimeoutMinutes)
	}
	if !cfg.Subtitle.OutputSameDir {
		t.Error("Expected output_same_dir to be true")
	}
	if len(cfg.Subtitle.EnglishVariants) != 3 {
		t.Errorf("Expected 3 english variants, got %d", len(cfg.Subtitle.EnglishVariants))
	}
	if len(cfg.Subtitle.ChineseVariants) != 4 {
		t.Errorf("Expected 4 chinese variants, got %d", len(cfg.Subtitle.ChineseVariants))
	}
	if !cfg.Subtitle.OpenCC.Enabled {
		t.Error("Expected opencc.enabled to be true")
	}
	if cfg.Subtitle.OpenCC.Config != "t2s.json" {
		t.Errorf("Expected opencc config 't2s.json', got %s", cfg.Subtitle.OpenCC.Config)
	}

	// Test Redis config
	if cfg.Redis.Host != "redis" {
		t.Errorf("Expected redis host 'redis', got %s", cfg.Redis.Host)
	}
	if cfg.Redis.Port != 6379 {
		t.Errorf("Expected redis port 6379, got %d", cfg.Redis.Port)
	}
	if cfg.Redis.QueueKey != "fusionn:translation:queue" {
		t.Errorf("Expected queue_key 'fusionn:translation:queue', got %s", cfg.Redis.QueueKey)
	}

	// Test Apprise config
	if !cfg.Apprise.Enabled {
		t.Error("Expected apprise.enabled to be true")
	}
	if cfg.Apprise.BaseURL != "http://apprise:8000" {
		t.Errorf("Expected base_url 'http://apprise:8000', got %s", cfg.Apprise.BaseURL)
	}
	if cfg.Apprise.Key != "apprise" {
		t.Errorf("Expected key 'apprise', got %s", cfg.Apprise.Key)
	}
	if cfg.Apprise.Tag != "fusionn-subtitle" {
		t.Errorf("Expected tag 'fusionn-subtitle', got %s", cfg.Apprise.Tag)
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

func TestFontEmbeddingConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	fontsDir := filepath.Join(tmpDir, "fonts")

	// Create fonts directory
	if err := os.MkdirAll(fontsDir, 0755); err != nil {
		t.Fatalf("Failed to create fonts directory: %v", err)
	}

	tests := []struct {
		name        string
		config      string
		expectError bool
		validate    func(*testing.T, *Config)
	}{
		{
			name: "font embedding disabled by default",
			config: `
server:
  port: 8080
subtitle:
  enabled: true
  duosubs:
    timeout_minutes: 10
`,
			expectError: false,
			validate: func(t *testing.T, cfg *Config) {
				if cfg.Subtitle.FontEmbedding.Enabled {
					t.Error("Expected font_embedding.enabled to be false by default")
				}
			},
		},
		{
			name: "font embedding enabled with valid config",
			config: `
server:
  port: 8080
subtitle:
  enabled: true
  duosubs:
    timeout_minutes: 10
  font_embedding:
    enabled: true
    fonts_dir: "` + fontsDir + `"
    timeout_seconds: 300
`,
			expectError: false,
			validate: func(t *testing.T, cfg *Config) {
				if !cfg.Subtitle.FontEmbedding.Enabled {
					t.Error("Expected font_embedding.enabled to be true")
				}
				if cfg.Subtitle.FontEmbedding.FontsDir != fontsDir {
					t.Errorf("Expected fonts_dir %s, got %s", fontsDir, cfg.Subtitle.FontEmbedding.FontsDir)
				}
				if cfg.Subtitle.FontEmbedding.TimeoutSeconds != 300 {
					t.Errorf("Expected timeout_seconds 300, got %d", cfg.Subtitle.FontEmbedding.TimeoutSeconds)
				}
			},
		},
		{
			name: "font embedding enabled with default timeout",
			config: `
server:
  port: 8080
subtitle:
  enabled: true
  duosubs:
    timeout_minutes: 10
  font_embedding:
    enabled: true
    fonts_dir: "` + fontsDir + `"
`,
			expectError: false,
			validate: func(t *testing.T, cfg *Config) {
				if cfg.Subtitle.FontEmbedding.TimeoutSeconds != 300 {
					t.Errorf("Expected default timeout_seconds 300, got %d", cfg.Subtitle.FontEmbedding.TimeoutSeconds)
				}
			},
		},
		{
			name: "font embedding enabled without fonts_dir",
			config: `
server:
  port: 8080
subtitle:
  enabled: true
  duosubs:
    timeout_minutes: 10
  font_embedding:
    enabled: true
`,
			expectError: true,
		},
		{
			name: "font embedding enabled with non-existent fonts_dir",
			config: `
server:
  port: 8080
subtitle:
  enabled: true
  duosubs:
    timeout_minutes: 10
  font_embedding:
    enabled: true
    fonts_dir: "/nonexistent/path"
`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := os.WriteFile(configPath, []byte(tt.config), 0644); err != nil {
				t.Fatalf("Failed to create test config: %v", err)
			}

			cfg, err := Load(configPath)
			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Fatalf("Failed to load config: %v", err)
			}

			if tt.validate != nil {
				tt.validate(t, cfg)
			}
		})
	}
}

func TestBazarrConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
server:
  port: 8080
bazarr:
  enabled: true
  url: "http://bazarr:6767"
  api_key: "test-api-key"
  search_timeout: 180
  language_code: "zh"
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if !cfg.Bazarr.Enabled {
		t.Error("Expected bazarr.enabled to be true")
	}
	if cfg.Bazarr.URL != "http://bazarr:6767" {
		t.Errorf("Expected url 'http://bazarr:6767', got %s", cfg.Bazarr.URL)
	}
	if cfg.Bazarr.APIKey != "test-api-key" {
		t.Errorf("Expected api_key 'test-api-key', got %s", cfg.Bazarr.APIKey)
	}
	if cfg.Bazarr.SearchTimeout != 180 {
		t.Errorf("Expected search_timeout 180, got %d", cfg.Bazarr.SearchTimeout)
	}
	if cfg.Bazarr.LanguageCode != "zh" {
		t.Errorf("Expected language_code 'zh', got %s", cfg.Bazarr.LanguageCode)
	}
}

func TestBazarrConfigDisabledByDefault(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	configContent := `
server:
  port: 8080
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	if cfg.Bazarr.Enabled {
		t.Error("Expected bazarr.enabled to be false by default")
	}
}
