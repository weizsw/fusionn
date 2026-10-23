package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/fusionn/pkg/logger"
)

// writeConfig writes a temporary config file and returns its path.
func writeConfig(t *testing.T, content string) string {
	t.Helper()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to create test config: %v", err)
	}
	return configPath
}

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

func TestEngineDefaultsAndLegacyConfig(t *testing.T) {
	tests := []struct {
		name       string
		config     string
		wantEngine string
	}{
		{
			name: "new config without engine defaults to llm",
			config: `
server:
  port: 8080
subtitle:
  enabled: true
  llm:
    base_url: "http://127.0.0.1:3425/v1"
    model: "group/flash-models"
    cache_dir: "/tmp/fusionn-cache"
`,
			wantEngine: EngineLLM,
		},
		{
			name: "legacy config with duosubs section stays duosubs",
			config: `
server:
  port: 8080
subtitle:
  enabled: true
  duosubs:
    timeout_minutes: 10
`,
			wantEngine: EngineDuoSubs,
		},
		{
			name: "explicit duosubs wins even with llm section",
			config: `
server:
  port: 8080
subtitle:
  enabled: true
  engine: "duosubs"
  llm:
    base_url: "http://127.0.0.1:3425/v1"
    model: "group/flash-models"
  duosubs:
    timeout_minutes: 10
`,
			wantEngine: EngineDuoSubs,
		},
		{
			name: "explicit llm with legacy duosubs section",
			config: `
server:
  port: 8080
subtitle:
  enabled: true
  engine: "llm"
  llm:
    base_url: "http://127.0.0.1:3425/v1"
    model: "group/flash-models"
    cache_dir: "/tmp/fusionn-cache"
  duosubs:
    timeout_minutes: 10
`,
			wantEngine: EngineLLM,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := Load(writeConfig(t, tt.config))
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			if cfg.Subtitle.Engine != tt.wantEngine {
				t.Fatalf("Engine = %q, want %q", cfg.Subtitle.Engine, tt.wantEngine)
			}
		})
	}
}

func TestEngineEnvOverride(t *testing.T) {
	t.Setenv("FUSIONN_SUBTITLE_ENGINE", "duosubs")

	cfg, err := Load(writeConfig(t, `
server:
  port: 8080
subtitle:
  enabled: true
  duosubs:
    timeout_minutes: 10
`))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Subtitle.Engine != EngineDuoSubs {
		t.Fatalf("Engine = %q, want %q", cfg.Subtitle.Engine, EngineDuoSubs)
	}
}

func TestEngineInvalidValue(t *testing.T) {
	_, err := Load(writeConfig(t, `
server:
  port: 8080
subtitle:
  enabled: true
  engine: "bogus"
`))
	if err == nil {
		t.Fatal("Expected error for unknown engine")
	}
}

func TestLLMConfigDefaults(t *testing.T) {
	cfg, err := Load(writeConfig(t, `
server:
  port: 8080
subtitle:
  enabled: true
  llm:
    base_url: "http://127.0.0.1:3425/v1"
    model: "group/flash-models"
    cache_dir: "/tmp/fusionn-cache"
`))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	llm := cfg.Subtitle.LLM
	if llm.ResponseFormat != ResponseFormatJSONObject {
		t.Errorf("response_format = %q, want %q", llm.ResponseFormat, ResponseFormatJSONObject)
	}
	if llm.TimeoutSeconds != 300 {
		t.Errorf("timeout_seconds = %d, want 300", llm.TimeoutSeconds)
	}
	if llm.MaxTokens != 0 {
		t.Errorf("max_tokens = %d, want 0 (omitted)", llm.MaxTokens)
	}
	if llm.ReasoningEffort != "low" {
		t.Errorf("reasoning_effort = %q, want low", llm.ReasoningEffort)
	}
	if llm.Workers != 3 {
		t.Errorf("workers = %d, want 3", llm.Workers)
	}
	if llm.Retries != 1 {
		t.Errorf("retries = %d, want 1", llm.Retries)
	}
	if llm.WindowSeconds != 210 {
		t.Errorf("window_seconds = %d, want 210", llm.WindowSeconds)
	}
	if llm.OverlapSeconds != 15 {
		t.Errorf("overlap_seconds = %d, want 15", llm.OverlapSeconds)
	}
	if !llm.CacheEnabled {
		t.Error("cache_enabled = false, want true")
	}
	if llm.EnChars != 65 || llm.ZhChars != 25 {
		t.Errorf("chars = %d/%d, want 65/25", llm.EnChars, llm.ZhChars)
	}
	if llm.EnGapMS != 400 || llm.ZhGapMS != 500 {
		t.Errorf("gaps = %d/%d, want 400/500", llm.EnGapMS, llm.ZhGapMS)
	}
	if llm.SpanMS != 5500 {
		t.Errorf("span_ms = %d, want 5500", llm.SpanMS)
	}
	if llm.FillGapMS != 500 {
		t.Errorf("fill_gap_ms = %d, want 500", llm.FillGapMS)
	}
	if llm.APIKey != "" || llm.APIKeyEnv != "" || llm.MappingPath != "" || llm.WordAlignURL != "" {
		t.Errorf("optional LLM fields should default to empty")
	}
}

func TestLLMConfigEnvBindings(t *testing.T) {
	t.Setenv("FUSIONN_SUBTITLE_LLM_API_KEY", "env-api-key")
	t.Setenv("FUSIONN_SUBTITLE_LLM_BASE_URL", "http://llm.example/v1")
	t.Setenv("FUSIONN_SUBTITLE_LLM_MODEL", "env-model")
	t.Setenv("FUSIONN_SUBTITLE_LLM_REASONING_EFFORT", "medium")
	t.Setenv("FUSIONN_SUBTITLE_LLM_WORKERS", "7")
	t.Setenv("FUSIONN_SUBTITLE_LLM_WORD_ALIGN_URL", "http://word-align:8767")

	cfg, err := Load(writeConfig(t, `
server:
  port: 8080
subtitle:
  enabled: true
  llm:
    cache_dir: "/tmp/fusionn-cache"
`))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	llm := cfg.Subtitle.LLM
	if llm.APIKey != "env-api-key" {
		t.Errorf("api_key = %q, want env-api-key", llm.APIKey)
	}
	if llm.BaseURL != "http://llm.example/v1" {
		t.Errorf("base_url = %q", llm.BaseURL)
	}
	if llm.Model != "env-model" {
		t.Errorf("model = %q", llm.Model)
	}
	if llm.ReasoningEffort != "medium" {
		t.Errorf("reasoning_effort = %q", llm.ReasoningEffort)
	}
	if llm.Workers != 7 {
		t.Errorf("workers = %d, want 7", llm.Workers)
	}
	if llm.WordAlignURL != "http://word-align:8767" {
		t.Errorf("word_align_url = %q", llm.WordAlignURL)
	}
}

func TestLLMConfigExplicitZeroIsPreserved(t *testing.T) {
	_, err := Load(writeConfig(t, `
server:
  port: 8080
subtitle:
  enabled: true
  llm:
    base_url: "http://127.0.0.1:3425/v1"
    model: "group/flash-models"
    cache_dir: "/tmp/fusionn-cache"
    workers: 0
`))
	if err == nil {
		t.Fatal("Expected explicit workers: 0 to be rejected instead of replaced by the default")
	}
}

func TestValidationOnlyForSelectedEngine(t *testing.T) {
	t.Run("duosubs selected ignores invalid llm", func(t *testing.T) {
		cfg, err := Load(writeConfig(t, `
server:
  port: 8080
subtitle:
  enabled: true
  engine: "duosubs"
  duosubs:
    timeout_minutes: 10
  llm:
    base_url: ""
    model: ""
`))
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
		if cfg.Subtitle.Engine != EngineDuoSubs {
			t.Fatalf("Engine = %q", cfg.Subtitle.Engine)
		}
	})

	t.Run("llm selected ignores missing duosubs timeout", func(t *testing.T) {
		_, err := Load(writeConfig(t, `
server:
  port: 8080
subtitle:
  enabled: true
  engine: "llm"
  llm:
    base_url: "http://127.0.0.1:3425/v1"
    model: "group/flash-models"
    cache_dir: "/tmp/fusionn-cache"
`))
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
	})

	t.Run("llm selected rejects missing base_url", func(t *testing.T) {
		_, err := Load(writeConfig(t, `
server:
  port: 8080
subtitle:
  enabled: true
  engine: "llm"
  llm:
    model: "group/flash-models"
    cache_dir: "/tmp/fusionn-cache"
`))
		if err == nil {
			t.Fatal("Expected error for missing base_url")
		}
	})

	t.Run("llm selected rejects non-http base_url", func(t *testing.T) {
		_, err := Load(writeConfig(t, `
server:
  port: 8080
subtitle:
  enabled: true
  engine: "llm"
  llm:
    base_url: "127.0.0.1:3425"
    model: "group/flash-models"
    cache_dir: "/tmp/fusionn-cache"
`))
		if err == nil {
			t.Fatal("Expected error for non-http base_url")
		}
	})

	t.Run("llm selected rejects bad response format", func(t *testing.T) {
		_, err := Load(writeConfig(t, `
server:
  port: 8080
subtitle:
  enabled: true
  engine: "llm"
  llm:
    base_url: "http://127.0.0.1:3425/v1"
    model: "group/flash-models"
    response_format: "xml"
    cache_dir: "/tmp/fusionn-cache"
`))
		if err == nil {
			t.Fatal("Expected error for unknown response_format")
		}
	})

	t.Run("llm selected requires cache_dir when cache enabled", func(t *testing.T) {
		_, err := Load(writeConfig(t, `
server:
  port: 8080
subtitle:
  enabled: true
  engine: "llm"
  llm:
    base_url: "http://127.0.0.1:3425/v1"
    model: "group/flash-models"
`))
		if err == nil {
			t.Fatal("Expected error for missing cache_dir")
		}
	})

	t.Run("llm accepts disabled cache without cache_dir", func(t *testing.T) {
		_, err := Load(writeConfig(t, `
server:
  port: 8080
subtitle:
  enabled: true
  engine: "llm"
  llm:
    base_url: "http://127.0.0.1:3425/v1"
    model: "group/flash-models"
    cache_enabled: false
`))
		if err != nil {
			t.Fatalf("Load() error = %v", err)
		}
	})

	t.Run("llm rejects overlap not smaller than window", func(t *testing.T) {
		_, err := Load(writeConfig(t, `
server:
  port: 8080
subtitle:
  enabled: true
  engine: "llm"
  llm:
    base_url: "http://127.0.0.1:3425/v1"
    model: "group/flash-models"
    cache_dir: "/tmp/fusionn-cache"
    window_seconds: 60
    overlap_seconds: 60
`))
		if err == nil {
			t.Fatal("Expected error for overlap >= window")
		}
	})
}

func TestASSStyleDefaults(t *testing.T) {
	cfg, err := Load(writeConfig(t, `
server:
  port: 8080
subtitle:
  enabled: true
  engine: "duosubs"
  duosubs:
    timeout_minutes: 10
  ass_style:
    enabled: true
`))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	style := cfg.Subtitle.ASSStyle
	checks := []struct {
		name string
		got  any
		want any
	}{
		{"play_res_x", style.PlayResX, 1920},
		{"play_res_y", style.PlayResY, 1080},
		{"primary_font", style.PrimaryFont, "PingFang SC"},
		{"primary_size", style.PrimarySize, 52},
		{"primary_color", style.PrimaryColor, "&H08EFECEA"},
		{"secondary_font", style.SecondaryFont, "Helvetica Neue"},
		{"secondary_size", style.SecondarySize, 36},
		{"secondary_color", style.SecondaryColor, "&H087BC0E5"},
		{"bold", style.Bold, false},
		{"outline", style.Outline, 1.4},
		{"outline_color", style.OutlineColor, "&H30141414"},
		{"shadow", style.Shadow, 1.5},
		{"back_color", style.BackColor, "&H80000000"},
		{"margin_v", style.MarginV, 32},
		{"margin_left", style.MarginLeft, 80},
		{"margin_right", style.MarginRight, 80},
		{"scale_x", style.ScaleX, 100},
		{"spacing", style.Spacing, 0.0},
		{"line_height_ratio", style.LineHeightRatio, 1.23},
		{"block_padding", style.BlockPadding, 16},
		{"collision_gap", style.CollisionGap, 12},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
}

func TestLegacyStyleMigration(t *testing.T) {
	// A legacy config has no play_res_y; its explicit numeric values are in
	// 384x288 script coordinates and must scale by 1080/288 = 3.75.
	cfg, err := Load(writeConfig(t, `
server:
  port: 8080
subtitle:
  enabled: true
  engine: "duosubs"
  duosubs:
    timeout_minutes: 10
  ass_style:
    enabled: true
    primary_font: "WenQuanYi Micro Hei"
    primary_size: 20
    secondary_font: "WenQuanYi Micro Hei"
    secondary_size: 13
    outline: 0.5
    shadow: 0.5
    margin_v: 5
    margin_left: 10
    margin_right: 10
`))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	style := cfg.Subtitle.ASSStyle
	if style.PrimarySize != 75 {
		t.Errorf("primary_size = %d, want 75 (20 * 3.75)", style.PrimarySize)
	}
	if style.SecondarySize != 49 {
		t.Errorf("secondary_size = %d, want 49 (13 * 3.75 rounded)", style.SecondarySize)
	}
	if style.MarginV != 19 {
		t.Errorf("margin_v = %d, want 19 (5 * 3.75 rounded)", style.MarginV)
	}
	if style.MarginLeft != 50 {
		t.Errorf("margin_left = %d, want 50 (10 * 5 horizontal)", style.MarginLeft)
	}
	if style.MarginRight != 50 {
		t.Errorf("margin_right = %d, want 50 (10 * 5 horizontal)", style.MarginRight)
	}
	if style.Outline != 1.875 {
		t.Errorf("outline = %v, want 1.875", style.Outline)
	}
	if style.Shadow != 1.875 {
		t.Errorf("shadow = %v, want 1.875", style.Shadow)
	}
	// Fields not present in the legacy file keep the new defaults.
	if style.MarginV == 0 || style.PrimaryFont != "WenQuanYi Micro Hei" {
		t.Errorf("unexpected style: %+v", style)
	}
	if style.LineHeightRatio != 1.23 || style.BlockPadding != 16 || style.CollisionGap != 12 {
		t.Errorf("new layout defaults were not applied: %+v", style)
	}
}

func TestLegacyStyleMigrationPreservesExplicitZeroAndFalse(t *testing.T) {
	cfg, err := Load(writeConfig(t, `
server:
  port: 8080
subtitle:
  enabled: true
  engine: "duosubs"
  duosubs:
    timeout_minutes: 10
  ass_style:
    enabled: true
    primary_size: 20
    margin_v: 0
    margin_left: 0
    margin_right: 0
    outline: 0
    shadow: 0
    bold: false
`))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	style := cfg.Subtitle.ASSStyle
	if style.PrimarySize != 75 {
		t.Errorf("primary_size = %d, want 75", style.PrimarySize)
	}
	if style.MarginV != 0 || style.MarginLeft != 0 || style.MarginRight != 0 {
		t.Errorf("explicit zero margins must stay zero: %+v", style)
	}
	if style.Outline != 0 || style.Shadow != 0 {
		t.Errorf("explicit zero outline/shadow must stay zero: %+v", style)
	}
	if style.Bold {
		t.Error("explicit bold: false must stay false")
	}
}

func TestExplicitPlayResYOptsOutOfMigration(t *testing.T) {
	cfg, err := Load(writeConfig(t, `
server:
  port: 8080
subtitle:
  enabled: true
  engine: "duosubs"
  duosubs:
    timeout_minutes: 10
  ass_style:
    enabled: true
    play_res_y: 1080
    primary_size: 20
    margin_v: 5
`))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	style := cfg.Subtitle.ASSStyle
	if style.PrimarySize != 20 {
		t.Errorf("primary_size = %d, want 20 with explicit play_res_y", style.PrimarySize)
	}
	if style.MarginV != 5 {
		t.Errorf("margin_v = %d, want 5 with explicit play_res_y", style.MarginV)
	}
}

func TestStyleValidationWhenEnabled(t *testing.T) {
	tests := []struct {
		name    string
		style   string
		wantErr bool
	}{
		{name: "defaults valid", style: "    enabled: true"},
		{name: "zero play_res_y rejected", style: "    enabled: true\n    play_res_y: -1", wantErr: true},
		{name: "empty font rejected", style: "    enabled: true\n    primary_font: \"\"", wantErr: true},
		{name: "zero size rejected", style: "    enabled: true\n    primary_size: 0", wantErr: true},
		{name: "zero scale rejected", style: "    enabled: true\n    scale_x: 0", wantErr: true},
		{name: "zero line height rejected", style: "    enabled: true\n    line_height_ratio: 0", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(writeConfig(t, `
server:
  port: 8080
subtitle:
  enabled: true
  engine: "duosubs"
  duosubs:
    timeout_minutes: 10
  ass_style:
`+tt.style+"\n"))
			if tt.wantErr && err == nil {
				t.Fatal("Expected validation error")
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("Load() error = %v", err)
			}
		})
	}
}

func TestReloadUsesFreshViperInstance(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping hot-reload test in short mode")
	}

	logger.Init(true)
	defer logger.Sync()

	configPath := writeConfig(t, `
server:
  port: 8080
subtitle:
  enabled: true
  llm:
    base_url: "http://127.0.0.1:3425/v1"
    model: "group/flash-models"
    cache_dir: "/tmp/fusionn-cache"
  ass_style:
    enabled: true
    primary_font: "Old Font"
`)

	mgr, err := NewManager(configPath)
	if err != nil {
		t.Fatalf("NewManager() error = %v", err)
	}
	defer mgr.Stop()

	if got := mgr.Get().Subtitle.Engine; got != EngineLLM {
		t.Fatalf("initial engine = %q, want %q", got, EngineLLM)
	}
	if got := mgr.Get().Subtitle.ASSStyle.PrimaryFont; got != "Old Font" {
		t.Fatalf("initial primary_font = %q, want Old Font", got)
	}

	changed := make(chan *Config, 1)
	mgr.OnChange(func(old, updated *Config) {
		changed <- updated
	})

	// Rewriting without the llm section must not retain the old LLM values;
	// the legacy duosubs section now selects the duosubs engine.
	time.Sleep(100 * time.Millisecond)
	updated := `
server:
  port: 9090
subtitle:
  enabled: true
  duosubs:
    timeout_minutes: 10
`
	if err := os.WriteFile(configPath, []byte(updated), 0644); err != nil {
		t.Fatalf("Failed to update config: %v", err)
	}

	select {
	case cfg := <-changed:
		if cfg.Subtitle.Engine != EngineDuoSubs {
			t.Errorf("reloaded engine = %q, want %q", cfg.Subtitle.Engine, EngineDuoSubs)
		}
		if cfg.Subtitle.LLM.BaseURL != "" || cfg.Subtitle.LLM.Model != "" || cfg.Subtitle.LLM.CacheDir != "" {
			t.Errorf("reloaded config retained stale LLM values: %+v", cfg.Subtitle.LLM)
		}
		if cfg.Subtitle.ASSStyle.PrimaryFont != "PingFang SC" {
			t.Errorf("reloaded config retained stale primary_font %q, want default", cfg.Subtitle.ASSStyle.PrimaryFont)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("Config change was not detected within timeout")
	}
}

func TestLogChangesRedactsSecrets(t *testing.T) {
	logger.Init(true)
	defer logger.Sync()

	var buf bytes.Buffer
	core := zapcore.NewCore(
		zapcore.NewConsoleEncoder(zapcore.EncoderConfig{MessageKey: "msg", LevelKey: "level"}),
		zapcore.AddSync(&buf),
		zapcore.DebugLevel,
	)
	prev := logger.Log
	logger.Log = zap.New(core).Sugar()
	defer func() { logger.Log = prev }()

	oldCfg := &Config{
		Redis:   RedisConfig{Password: "old-redis-secret"},
		Apprise: AppriseConfig{Key: "old-apprise-key"},
		Bazarr:  BazarrConfig{APIKey: "old-bazarr-key"},
		Subtitle: SubtitleConfig{
			LLM: LLMConfig{APIKey: "old-llm-key", APIKeyEnv: "OLD_ENV"},
		},
	}
	newCfg := &Config{
		Redis:   RedisConfig{Password: "new-redis-secret"},
		Apprise: AppriseConfig{Key: "new-apprise-key"},
		Bazarr:  BazarrConfig{APIKey: "new-bazarr-key"},
		Subtitle: SubtitleConfig{
			LLM: LLMConfig{APIKey: "new-llm-key", APIKeyEnv: "NEW_ENV"},
		},
	}

	logChanges(oldCfg, newCfg, "")

	logged := buf.String()
	for _, secret := range []string{
		"old-redis-secret", "new-redis-secret",
		"old-apprise-key", "new-apprise-key",
		"old-bazarr-key", "new-bazarr-key",
		"old-llm-key", "new-llm-key",
	} {
		if strings.Contains(logged, secret) {
			t.Errorf("log output leaked secret %q:\n%s", secret, logged)
		}
	}
	if !strings.Contains(logged, "Subtitle.LLM.APIKey: [REDACTED] → [REDACTED]") {
		t.Errorf("expected redacted LLM API key line, got:\n%s", logged)
	}
	if strings.Contains(logged, "[REDACTED]") && strings.Contains(logged, "APIKeyEnv") {
		// APIKeyEnv is a variable name, not a secret, and should be shown.
		if !strings.Contains(logged, "Subtitle.LLM.APIKeyEnv: OLD_ENV → NEW_ENV") {
			t.Errorf("expected APIKeyEnv values to remain visible, got:\n%s", logged)
		}
	}

	emptyBuf := buf.String()
	logChanges(&Config{Subtitle: SubtitleConfig{LLM: LLMConfig{APIKey: ""}}},
		&Config{Subtitle: SubtitleConfig{LLM: LLMConfig{APIKey: "now-set"}}}, "")
	if strings.Contains(buf.String(), "now-set") || strings.Contains(buf.String(), emptyBuf+emptyBuf) {
		t.Errorf("unexpected log output:\n%s", buf.String())
	}
	if !strings.Contains(buf.String(), "Subtitle.LLM.APIKey: (empty) → [REDACTED]") {
		t.Errorf("expected (empty) → [REDACTED], got:\n%s", buf.String())
	}
}

func TestLegacyStyleMigrationFromEnvironment(t *testing.T) {
	t.Setenv("FUSIONN_SUBTITLE_ASS_STYLE_PRIMARY_SIZE", "20")
	t.Setenv("FUSIONN_SUBTITLE_ASS_STYLE_OUTLINE", "0.5")

	cfg, err := Load(writeConfig(t, `
server:
  port: 8080
subtitle:
  enabled: true
  engine: "duosubs"
  duosubs:
    timeout_minutes: 10
  ass_style:
    enabled: true
`))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Subtitle.ASSStyle.PrimarySize != 75 {
		t.Errorf("primary_size = %d, want 75 from env override migration", cfg.Subtitle.ASSStyle.PrimarySize)
	}
	if cfg.Subtitle.ASSStyle.Outline != 1.875 {
		t.Errorf("outline = %v, want 1.875 from env override migration", cfg.Subtitle.ASSStyle.Outline)
	}
}

func TestExampleConfigLoads(t *testing.T) {
	cfg, err := Load("../../config/config.example.yaml")
	if err != nil {
		t.Fatalf("config.example.yaml does not load: %v", err)
	}
	if cfg.Subtitle.Engine != EngineLLM {
		t.Errorf("example engine = %q, want %q", cfg.Subtitle.Engine, EngineLLM)
	}
}

func TestEngineEnvOverrideWithoutDuosubsSection(t *testing.T) {
	// An environment-only engine must bind even though unset keys have defaults.
	t.Setenv("FUSIONN_SUBTITLE_ENGINE", "llm")

	cfg, err := Load(writeConfig(t, `
server:
  port: 8080
subtitle:
  enabled: true
  llm:
    base_url: "http://127.0.0.1:3425/v1"
    model: "group/flash-models"
    cache_dir: "/tmp/fusionn-cache"
`))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Subtitle.Engine != EngineLLM {
		t.Fatalf("Engine = %q, want %q", cfg.Subtitle.Engine, EngineLLM)
	}
}

func TestExplicitPlayResYFromEnvironmentOptsOutOfMigration(t *testing.T) {
	t.Setenv("FUSIONN_SUBTITLE_ASS_STYLE_PLAY_RES_Y", "1080")

	cfg, err := Load(writeConfig(t, `
server:
  port: 8080
subtitle:
  enabled: true
  engine: "duosubs"
  duosubs:
    timeout_minutes: 10
  ass_style:
    enabled: true
    primary_size: 20
    margin_v: 5
`))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Subtitle.ASSStyle.PrimarySize != 20 {
		t.Errorf("primary_size = %d, want 20", cfg.Subtitle.ASSStyle.PrimarySize)
	}
	if cfg.Subtitle.ASSStyle.MarginV != 5 {
		t.Errorf("margin_v = %d, want 5", cfg.Subtitle.ASSStyle.MarginV)
	}
}

func TestASSStyleEnabledByDefault(t *testing.T) {
	// An absent ass_style section must receive the requested new default style,
	// not the renderer-neutral zero config.
	cfg, err := Load(writeConfig(t, `
server:
  port: 8080
subtitle:
  enabled: true
  engine: "duosubs"
  duosubs:
    timeout_minutes: 10
`))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	style := cfg.Subtitle.ASSStyle
	if !style.Enabled {
		t.Error("ass_style.enabled = false, want true by default")
	}
	if style.PrimaryFont != "PingFang SC" || style.PrimarySize != 52 {
		t.Errorf("default primary style = %s/%d, want PingFang SC/52", style.PrimaryFont, style.PrimarySize)
	}
}

func TestASSStyleExplicitFalseIsPreserved(t *testing.T) {
	cfg, err := Load(writeConfig(t, `
server:
  port: 8080
subtitle:
  enabled: true
  engine: "duosubs"
  duosubs:
    timeout_minutes: 10
  ass_style:
    enabled: false
    primary_font: ""
    primary_size: 0
`))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Subtitle.ASSStyle.Enabled {
		t.Error("ass_style.enabled = true, want explicit false preserved")
	}
}

func TestLLMBaseURLRejectsUserinfoAndQuery(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
	}{
		{name: "userinfo", baseURL: "http://user:secret@example.com/v1"},
		{name: "query", baseURL: "http://example.com/v1?api_key=secret"},
		{name: "fragment", baseURL: "http://example.com/v1#secret"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Load(writeConfig(t, `
server:
  port: 8080
subtitle:
  enabled: true
  engine: "llm"
  llm:
    base_url: "`+tt.baseURL+`"
    model: "group/flash-models"
    cache_dir: "/tmp/fusionn-cache"
`))
			if err == nil {
				t.Fatalf("Expected error for base_url %q", tt.baseURL)
			}
			if strings.Contains(err.Error(), "secret") {
				t.Errorf("validation error leaked URL secret: %v", err)
			}
		})
	}
}

func TestLLMBaseURLAllowsNormalPaths(t *testing.T) {
	for _, baseURL := range []string{
		"http://127.0.0.1:3425/v1",
		"https://llm.example.com/openai/v1/",
		"http://host.docker.internal:3425/v1",
	} {
		_, err := Load(writeConfig(t, `
server:
  port: 8080
subtitle:
  enabled: true
  engine: "llm"
  llm:
    base_url: "`+baseURL+`"
    model: "group/flash-models"
    cache_dir: "/tmp/fusionn-cache"
`))
		if err != nil {
			t.Errorf("base_url %q rejected: %v", baseURL, err)
		}
	}
}

func TestLegacyStyleMigrationUsesAxisSpecificScales(t *testing.T) {
	// play_res_y is omitted (default 1080, vertical scale 1080/288 = 3.75) while
	// play_res_x is 1280 (horizontal scale 1280/384 = 3.33...). The two axes must
	// migrate with their own scales.
	cfg, err := Load(writeConfig(t, `
server:
  port: 8080
subtitle:
  enabled: true
  engine: "duosubs"
  duosubs:
    timeout_minutes: 10
  ass_style:
    enabled: true
    play_res_x: 1280
    primary_size: 20
    margin_v: 8
    margin_left: 12
    margin_right: 24
    outline: 0.4
`))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	style := cfg.Subtitle.ASSStyle
	if style.PrimarySize != 75 {
		t.Errorf("primary_size = %d, want 75 (20 * 3.75 vertical)", style.PrimarySize)
	}
	if style.MarginV != 30 {
		t.Errorf("margin_v = %d, want 30 (8 * 3.75 vertical)", style.MarginV)
	}
	if style.MarginLeft != 40 {
		t.Errorf("margin_left = %d, want 40 (12 * 3.33 horizontal)", style.MarginLeft)
	}
	if style.MarginRight != 80 {
		t.Errorf("margin_right = %d, want 80 (24 * 3.33 horizontal)", style.MarginRight)
	}
	if style.Outline != 1.5 {
		t.Errorf("outline = %v, want 1.5 (0.4 * 3.75 vertical)", style.Outline)
	}
}

func TestWordAlignmentURLValidation(t *testing.T) {
	cfg, err := Load(writeConfig(t, `
server:
  port: 8080
subtitle:
  enabled: true
  llm:
    base_url: "http://unused.invalid/v1"
    model: "test"
    cache_enabled: false
`))
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"", "http://word-align:8767", "https://inference.example/subtitles"} {
		cfg.Subtitle.LLM.WordAlignURL = value
		if err := cfg.Subtitle.LLM.validate(); err != nil {
			t.Errorf("valid URL rejected: %v", err)
		}
	}
	for _, value := range []string{"word-align:8767", "ftp://word-align", "http://user:secret@word-align", "http://word-align?key=secret", "http://word-align#fragment"} {
		cfg.Subtitle.LLM.WordAlignURL = value
		if err := cfg.Subtitle.LLM.validate(); err == nil {
			t.Errorf("invalid URL accepted: %s", value)
		}
	}
}
