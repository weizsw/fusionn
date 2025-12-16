package config

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/spf13/viper"

	"github.com/fusionn/pkg/logger"
)

// Config holds the application configuration.
type Config struct {
	Server   ServerConfig   `mapstructure:"server"`
	Subtitle SubtitleConfig `mapstructure:"subtitle"`
	Redis    RedisConfig    `mapstructure:"redis"`
	Apprise  AppriseConfig  `mapstructure:"apprise"`
	Queue    QueueConfig    `mapstructure:"queue"`
}

// ServerConfig holds server settings.
type ServerConfig struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
}

// SubtitleConfig holds subtitle processing settings.
type SubtitleConfig struct {
	Enabled             bool           `mapstructure:"enabled"`
	DuoSubs             DuoSubsConfig  `mapstructure:"duosubs"`
	OutputSameDir       bool           `mapstructure:"output_same_dir"`
	EnglishVariants     []string       `mapstructure:"english_variants"`
	ChineseVariants     []string       `mapstructure:"chinese_variants"`
	SimplifiedKeywords  []string       `mapstructure:"simplified_keywords"`
	TraditionalKeywords []string       `mapstructure:"traditional_keywords"`
	OpenCC              OpenCCConfig   `mapstructure:"opencc"`
	ASSStyle            ASSStyleConfig `mapstructure:"ass_style"`
}

// DuoSubsConfig holds DuoSubs settings.
type DuoSubsConfig struct {
	Model          string `mapstructure:"model"`
	Device         string `mapstructure:"device"`
	TimeoutMinutes int    `mapstructure:"timeout_minutes"`
}

// OpenCCConfig holds OpenCC settings.
type OpenCCConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Config  string `mapstructure:"config"`
}

// ASSStyleConfig holds ASS subtitle styling settings.
type ASSStyleConfig struct {
	Enabled        bool    `mapstructure:"enabled"`
	PrimaryFont    string  `mapstructure:"primary_font"`
	PrimarySize    int     `mapstructure:"primary_size"`
	PrimaryColor   string  `mapstructure:"primary_color"`
	SecondaryFont  string  `mapstructure:"secondary_font"`
	SecondarySize  int     `mapstructure:"secondary_size"`
	SecondaryColor string  `mapstructure:"secondary_color"`
	Bold           bool    `mapstructure:"bold"`
	Outline        float64 `mapstructure:"outline"`
	Shadow         float64 `mapstructure:"shadow"`
	MarginV        int     `mapstructure:"margin_v"`
}

// RedisConfig holds Redis connection settings.
type RedisConfig struct {
	Host     string `mapstructure:"host"`
	Port     int    `mapstructure:"port"`
	Password string `mapstructure:"password"`
	Database int    `mapstructure:"database"`
	QueueKey string `mapstructure:"queue_key"`
}

// AppriseConfig holds Apprise notification settings.
type AppriseConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	BaseURL string `mapstructure:"base_url"`
	Key     string `mapstructure:"key"`
	Tag     string `mapstructure:"tag"`
}

// QueueConfig holds job queue settings.
type QueueConfig struct {
	Workers    int `mapstructure:"workers"`     // Number of concurrent workers (default: 1)
	QueueSize  int `mapstructure:"queue_size"`  // Max pending jobs (default: 100)
	MaxRetries int `mapstructure:"max_retries"` // Max retry attempts (default: 3)
}

// ChangeCallback is called when config changes. Receives old and new config.
type ChangeCallback func(old, new *Config)

// Manager handles config loading and hot-reload.
type Manager struct {
	mu        sync.RWMutex
	cfg       *Config
	callbacks []ChangeCallback
	stop      chan struct{}

	// Polling state
	path        string
	lastModTime time.Time
}

// NewManager creates a config manager with hot-reload support via polling.
// Config changes are detected automatically every 10 seconds.
func NewManager(path string) (*Manager, error) {
	viper.SetConfigFile(path)
	viper.SetConfigType("yaml")

	// Environment variable override support
	viper.SetEnvPrefix("FUSIONN")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		return nil, err
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, err
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	// Get initial file mod time
	var lastMod time.Time
	if stat, err := os.Stat(path); err == nil {
		lastMod = stat.ModTime()
	}

	m := &Manager{
		cfg:         &cfg,
		stop:        make(chan struct{}),
		path:        path,
		lastModTime: lastMod,
	}

	// Start polling for config changes
	go m.pollForChanges(10 * time.Second)

	logger.Infof("📋 Config loaded (polling every 10s for changes)")

	return m, nil
}

// Get returns the current config (thread-safe).
func (m *Manager) Get() *Config {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cfg
}

// OnChange registers a callback for config changes.
func (m *Manager) OnChange(cb ChangeCallback) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.callbacks = append(m.callbacks, cb)
}

// Stop stops the config polling goroutine.
func (m *Manager) Stop() {
	close(m.stop)
}

// pollForChanges checks file modtime periodically for Docker bind mount compatibility.
func (m *Manager) pollForChanges(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.checkAndReload()
		case <-m.stop:
			return
		}
	}
}

// checkAndReload checks if config file changed and reloads if needed.
func (m *Manager) checkAndReload() {
	stat, err := os.Stat(m.path)
	if err != nil {
		logger.Warnf("Failed to stat config file: %v", err)
		return
	}

	if stat.ModTime().After(m.lastModTime) {
		logger.Info("🔄 Config file changed, reloading...")
		m.lastModTime = stat.ModTime()

		if err := viper.ReadInConfig(); err != nil {
			logger.Errorf("Failed to reload config: %v", err)
			return
		}

		var newCfg Config
		if err := viper.Unmarshal(&newCfg); err != nil {
			logger.Errorf("Failed to unmarshal config: %v", err)
			return
		}

		if err := newCfg.Validate(); err != nil {
			logger.Errorf("Invalid config after reload: %v", err)
			return
		}

		// Log changes
		m.mu.RLock()
		oldCfg := m.cfg
		m.mu.RUnlock()

		logChanges(oldCfg, &newCfg, "")

		// Update config
		m.mu.Lock()
		m.cfg = &newCfg
		callbacks := m.callbacks
		m.mu.Unlock()

		// Trigger callbacks
		for _, cb := range callbacks {
			cb(oldCfg, &newCfg)
		}

		logger.Info("✅ Config reloaded successfully")
	}
}

// Validate checks if the configuration is valid.
func (c *Config) Validate() error {
	if c.Server.Port <= 0 || c.Server.Port > 65535 {
		return fmt.Errorf("invalid server port: %d", c.Server.Port)
	}

	if c.Subtitle.Enabled {
		if c.Subtitle.DuoSubs.Model == "" {
			return fmt.Errorf("subtitle.duosubs.model is required when subtitle is enabled")
		}
		if c.Subtitle.DuoSubs.TimeoutMinutes <= 0 {
			return fmt.Errorf("subtitle.duosubs.timeout_minutes must be positive")
		}
	}

	return nil
}

// logChanges logs field-level differences between old and new config.
func logChanges(old, cur any, prefix string) {
	oldVal := reflect.ValueOf(old)
	newVal := reflect.ValueOf(cur)

	// Dereference pointers
	if oldVal.Kind() == reflect.Ptr {
		oldVal = oldVal.Elem()
	}
	if newVal.Kind() == reflect.Ptr {
		newVal = newVal.Elem()
	}

	if oldVal.Kind() != reflect.Struct {
		return
	}

	t := oldVal.Type()
	for i := range t.NumField() {
		field := t.Field(i)
		oldField := oldVal.Field(i)
		newField := newVal.Field(i)

		fieldName := field.Name
		if prefix != "" {
			fieldName = prefix + "." + fieldName
		}

		// Recurse into nested structs
		if oldField.Kind() == reflect.Struct {
			logChanges(oldField.Interface(), newField.Interface(), fieldName)
			continue
		}

		// Compare values
		if !reflect.DeepEqual(oldField.Interface(), newField.Interface()) {
			oldStr := formatValue(oldField)
			newStr := formatValue(newField)
			logger.Infof("  📝 %s: %s → %s", fieldName, oldStr, newStr)
		}
	}
}

// formatValue formats a reflect.Value for logging.
func formatValue(v reflect.Value) string {
	if v.Kind() == reflect.Slice {
		return fmt.Sprintf("%v", v.Interface())
	}
	return fmt.Sprintf("%v", v.Interface())
}

// Load is a convenience function for one-time loading (backwards compatible).
func Load(path string) (*Config, error) {
	viper.SetConfigFile(path)
	viper.SetConfigType("yaml")

	viper.SetEnvPrefix("FUSIONN")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		return nil, err
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		return nil, err
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}
