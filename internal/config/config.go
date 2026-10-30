package config

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/spf13/viper"
	"go.uber.org/zap"

	"github.com/fusionn/pkg/logger"
)

// Subtitle merge engine identifiers.
const (
	// EngineLLM merges subtitles with LLM-produced ID mappings.
	EngineLLM = "llm"
	// EngineDuoSubs merges subtitles with the legacy DuoSubs aligner.
	EngineDuoSubs = "duosubs"
)

// Supported LLM response formats.
const (
	ResponseFormatJSONObject = "json_object"
	ResponseFormatJSONSchema = "json_schema"
	ResponseFormatNone       = "none"
)

// Config holds the application configuration.
type Config struct {
	// LogLevel overrides LOG_LEVEL at run time; see logger.SetConfigLevel.
	LogLevel string         `mapstructure:"log_level"`
	Server   ServerConfig   `mapstructure:"server"`
	Subtitle SubtitleConfig `mapstructure:"subtitle"`
	Redis    RedisConfig    `mapstructure:"redis"`
	Apprise  AppriseConfig  `mapstructure:"apprise"`
	Queue    QueueConfig    `mapstructure:"queue"`
	Bazarr   BazarrConfig   `mapstructure:"bazarr"`
}

// ServerConfig holds server settings.
type ServerConfig struct {
	Host string `mapstructure:"host"`
	Port int    `mapstructure:"port"`
}

// SubtitleConfig holds subtitle processing settings.
type SubtitleConfig struct {
	Enabled             bool                `mapstructure:"enabled"`
	Engine              string              `mapstructure:"engine"` // "llm" or "duosubs"
	DuoSubs             DuoSubsConfig       `mapstructure:"duosubs"`
	LLM                 LLMConfig           `mapstructure:"llm"`
	OutputSameDir       bool                `mapstructure:"output_same_dir"`
	EnglishVariants     []string            `mapstructure:"english_variants"`
	ChineseVariants     []string            `mapstructure:"chinese_variants"`
	SimplifiedKeywords  []string            `mapstructure:"simplified_keywords"`
	TraditionalKeywords []string            `mapstructure:"traditional_keywords"`
	OpenCC              OpenCCConfig        `mapstructure:"opencc"`
	ASSStyle            ASSStyleConfig      `mapstructure:"ass_style"`
	FontEmbedding       FontEmbeddingConfig `mapstructure:"font_embedding"`
}

// DuoSubsConfig holds DuoSubs settings.
type DuoSubsConfig struct {
	Mode           string `mapstructure:"mode"` // "local" or "http"
	Model          string `mapstructure:"model"`
	Device         string `mapstructure:"device"`
	TimeoutMinutes int    `mapstructure:"timeout_minutes"`

	// HTTP mode settings
	HTTPURL             string `mapstructure:"http_url"`
	HTTPContainerPrefix string `mapstructure:"http_container_prefix"`
	HTTPHostPrefix      string `mapstructure:"http_host_prefix"`
}

// LLMConfig holds native Go LLM subtitle merge settings.
type LLMConfig struct {
	// Connection
	BaseURL         string `mapstructure:"base_url"`
	Model           string `mapstructure:"model"`
	APIKey          string `mapstructure:"api_key"`
	APIKeyEnv       string `mapstructure:"api_key_env"` // Env var name; takes precedence over APIKey
	ReasoningEffort string `mapstructure:"reasoning_effort"`
	ResponseFormat  string `mapstructure:"response_format"` // "json_object", "json_schema", or "none"
	TimeoutSeconds  int    `mapstructure:"timeout_seconds"`
	MaxTokens       int    `mapstructure:"max_tokens"` // Optional output cap per window, reasoning included; 0 omits it

	// Concurrency and retries
	Workers int `mapstructure:"workers"`
	Retries int `mapstructure:"retries"`

	// Sliding window
	WindowSeconds  int `mapstructure:"window_seconds"`
	OverlapSeconds int `mapstructure:"overlap_seconds"`

	// Mapping cache
	CacheEnabled bool   `mapstructure:"cache_enabled"`
	CacheDir     string `mapstructure:"cache_dir"`
	MappingPath  string `mapstructure:"mapping_path"` // Optional precomputed ID mapping to reuse

	// Layout thresholds
	EnChars      int    `mapstructure:"en_chars"`
	ZhChars      int    `mapstructure:"zh_chars"`
	EnGapMS      int    `mapstructure:"en_gap_ms"`
	ZhGapMS      int    `mapstructure:"zh_gap_ms"`
	SpanMS       int    `mapstructure:"span_ms"`
	FillGapMS    int    `mapstructure:"fill_gap_ms"`
	WordAlignURL string `mapstructure:"word_align_url"` // Optional internal HTTP word-alignment service
}

// OpenCCConfig holds OpenCC settings.
type OpenCCConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Config  string `mapstructure:"config"`
}

// ASSStyleConfig holds ASS subtitle styling settings.
// Sizes, margins, outline and shadow use play_res_x/play_res_y script coordinates.
type ASSStyleConfig struct {
	Enabled         bool    `mapstructure:"enabled"`
	PlayResX        int     `mapstructure:"play_res_x"`
	PlayResY        int     `mapstructure:"play_res_y"`
	PrimaryFont     string  `mapstructure:"primary_font"`
	PrimarySize     int     `mapstructure:"primary_size"`
	PrimaryColor    string  `mapstructure:"primary_color"`
	SecondaryFont   string  `mapstructure:"secondary_font"`
	SecondarySize   int     `mapstructure:"secondary_size"`
	SecondaryColor  string  `mapstructure:"secondary_color"`
	Bold            bool    `mapstructure:"bold"`
	Outline         float64 `mapstructure:"outline"`
	OutlineColor    string  `mapstructure:"outline_color"`
	Shadow          float64 `mapstructure:"shadow"`
	BackColor       string  `mapstructure:"back_color"`
	MarginV         int     `mapstructure:"margin_v"`
	MarginLeft      int     `mapstructure:"margin_left"`
	MarginRight     int     `mapstructure:"margin_right"`
	Alignment       int     `mapstructure:"alignment"`
	BorderStyle     int     `mapstructure:"border_style"`
	WrapStyle       string  `mapstructure:"wrap_style"`
	ScaleX          int     `mapstructure:"scale_x"`
	Spacing         float64 `mapstructure:"spacing"`
	LineHeightRatio float64 `mapstructure:"line_height_ratio"`
	BlockPadding    int     `mapstructure:"block_padding"`
	CollisionGap    int     `mapstructure:"collision_gap"`
}

// FontEmbeddingConfig holds font embedding settings.
type FontEmbeddingConfig struct {
	Enabled        bool   `mapstructure:"enabled"`
	FontsDir       string `mapstructure:"fonts_dir"`
	TimeoutSeconds int    `mapstructure:"timeout_seconds"`
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

// BazarrConfig holds Bazarr integration settings.
type BazarrConfig struct {
	Enabled             bool   `mapstructure:"enabled"`
	URL                 string `mapstructure:"url"`
	APIKey              string `mapstructure:"api_key"`
	SearchTimeout       int    `mapstructure:"search_timeout"`
	LanguageCode        string `mapstructure:"language_code"`
	PollIntervalSeconds int    `mapstructure:"poll_interval_seconds"`
	PollTimeoutSeconds  int    `mapstructure:"poll_timeout_seconds"`
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
	cfg, err := loadConfig(path)
	if err != nil {
		return nil, err
	}

	// Get initial file mod time
	var lastMod time.Time
	if stat, err := os.Stat(path); err == nil {
		lastMod = stat.ModTime()
	}

	m := &Manager{
		cfg:         cfg,
		stop:        make(chan struct{}),
		path:        path,
		lastModTime: lastMod,
	}

	// Start polling for config changes
	go m.pollForChanges(10 * time.Second)

	logger.Component("config").Info(context.Background(), "config_loaded", "Config loaded, polling for changes")

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
		logger.Component("config").Warn(context.Background(), "config_stat_failed", "Failed to stat config file", zap.Error(err))
		return
	}

	if stat.ModTime().After(m.lastModTime) {
		logger.Component("config").Info(context.Background(), "config_changed", "Config file changed, reloading")
		m.lastModTime = stat.ModTime()

		// Build a fresh Viper instance so keys removed from the file do not
		// linger from the previous load.
		newCfg, err := loadConfig(m.path)
		if err != nil {
			logger.Component("config").Error(context.Background(), "config_reload_failed", "Failed to reload config", zap.Error(err))
			return
		}

		// Log changes
		m.mu.RLock()
		oldCfg := m.cfg
		m.mu.RUnlock()

		logChanges(oldCfg, newCfg, "")

		// Update config
		m.mu.Lock()
		m.cfg = newCfg
		callbacks := m.callbacks
		m.mu.Unlock()

		// Trigger callbacks
		for _, cb := range callbacks {
			cb(oldCfg, newCfg)
		}

		logger.Component("config").Info(context.Background(), "config_reloaded", "Config reloaded")
	}
}

// Validate checks if the configuration is valid.
func (c *Config) Validate() error {
	if c.Server.Port <= 0 || c.Server.Port > 65535 {
		return fmt.Errorf("invalid server port: %d", c.Server.Port)
	}

	if c.Subtitle.Enabled {
		if err := c.Subtitle.validateSelectedEngine(); err != nil {
			return err
		}

		if c.Subtitle.ASSStyle.Enabled {
			if err := c.Subtitle.ASSStyle.validate(); err != nil {
				return err
			}
		}

		// Font embedding validation
		if c.Subtitle.FontEmbedding.Enabled {
			if c.Subtitle.FontEmbedding.FontsDir == "" {
				return fmt.Errorf("subtitle.font_embedding.fonts_dir must be set when font embedding is enabled")
			}
			if _, err := os.Stat(c.Subtitle.FontEmbedding.FontsDir); os.IsNotExist(err) {
				return fmt.Errorf("subtitle.font_embedding.fonts_dir does not exist: %s", c.Subtitle.FontEmbedding.FontsDir)
			}
			// Set default timeout if not specified
			if c.Subtitle.FontEmbedding.TimeoutSeconds <= 0 {
				c.Subtitle.FontEmbedding.TimeoutSeconds = 300
			}
		}
	}

	return nil
}

// validateSelectedEngine validates only the engine that will actually run.
func (c *SubtitleConfig) validateSelectedEngine() error {
	switch c.Engine {
	case EngineDuoSubs, "":
		// Model is optional - DuoSubs will use its default (LaBSE) if empty.
		if c.DuoSubs.TimeoutMinutes <= 0 {
			return fmt.Errorf("subtitle.duosubs.timeout_minutes must be positive")
		}
	case EngineLLM:
		return c.LLM.validate()
	default:
		return fmt.Errorf("subtitle.engine must be %q or %q, got %q", EngineLLM, EngineDuoSubs, c.Engine)
	}
	return nil
}

func (c *LLMConfig) validate() error {
	baseURL, err := url.Parse(c.BaseURL)
	if err != nil {
		return fmt.Errorf("subtitle.llm.base_url is not a valid URL")
	}
	if baseURL.Scheme != "http" && baseURL.Scheme != "https" {
		return fmt.Errorf("subtitle.llm.base_url must be an http(s) URL")
	}
	if baseURL.Host == "" {
		return fmt.Errorf("subtitle.llm.base_url must include a host")
	}
	// Do not echo the value when it contains credentials; the error is logged.
	if baseURL.User != nil {
		return fmt.Errorf("subtitle.llm.base_url must not contain embedded userinfo; use api_key or api_key_env")
	}
	if baseURL.RawQuery != "" || baseURL.Fragment != "" {
		return fmt.Errorf("subtitle.llm.base_url must not contain a query or fragment")
	}
	if strings.TrimSpace(c.Model) == "" {
		return fmt.Errorf("subtitle.llm.model must be set")
	}
	switch c.ResponseFormat {
	case ResponseFormatJSONObject, ResponseFormatJSONSchema, ResponseFormatNone:
	default:
		return fmt.Errorf("subtitle.llm.response_format must be %q, %q, or %q, got %q",
			ResponseFormatJSONObject, ResponseFormatJSONSchema, ResponseFormatNone, c.ResponseFormat)
	}
	if c.TimeoutSeconds <= 0 {
		return fmt.Errorf("subtitle.llm.timeout_seconds must be positive")
	}
	if c.MaxTokens < 0 {
		return fmt.Errorf("subtitle.llm.max_tokens must not be negative")
	}
	if c.Workers < 1 {
		return fmt.Errorf("subtitle.llm.workers must be at least 1")
	}
	if c.Retries < 0 {
		return fmt.Errorf("subtitle.llm.retries must not be negative")
	}
	if c.WindowSeconds <= 0 {
		return fmt.Errorf("subtitle.llm.window_seconds must be positive")
	}
	if c.OverlapSeconds < 0 || c.OverlapSeconds >= c.WindowSeconds {
		return fmt.Errorf("subtitle.llm.overlap_seconds must be non-negative and less than window_seconds")
	}
	if c.CacheEnabled && strings.TrimSpace(c.CacheDir) == "" {
		return fmt.Errorf("subtitle.llm.cache_dir must be set when cache_enabled is true")
	}
	if c.WordAlignURL != "" {
		u, err := url.Parse(c.WordAlignURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("subtitle.llm.word_align_url must be an http(s) URL without credentials, query, or fragment")
		}
	}
	if c.EnChars <= 0 {
		return fmt.Errorf("subtitle.llm.en_chars must be positive")
	}
	if c.ZhChars <= 0 {
		return fmt.Errorf("subtitle.llm.zh_chars must be positive")
	}
	if c.SpanMS <= 0 {
		return fmt.Errorf("subtitle.llm.span_ms must be positive")
	}
	if c.EnGapMS < 0 || c.ZhGapMS < 0 || c.FillGapMS < 0 {
		return fmt.Errorf("subtitle.llm gaps must not be negative")
	}
	return nil
}

func (c *ASSStyleConfig) validate() error {
	if c.PlayResX <= 0 || c.PlayResY <= 0 {
		return fmt.Errorf("subtitle.ass_style.play_res_x and play_res_y must be positive")
	}
	if strings.TrimSpace(c.PrimaryFont) == "" {
		return fmt.Errorf("subtitle.ass_style.primary_font must be set")
	}
	if strings.TrimSpace(c.SecondaryFont) == "" {
		return fmt.Errorf("subtitle.ass_style.secondary_font must be set")
	}
	if c.PrimarySize <= 0 || c.SecondarySize <= 0 {
		return fmt.Errorf("subtitle.ass_style primary_size and secondary_size must be positive")
	}
	if c.ScaleX <= 0 {
		return fmt.Errorf("subtitle.ass_style.scale_x must be positive")
	}
	if c.LineHeightRatio <= 0 {
		return fmt.Errorf("subtitle.ass_style.line_height_ratio must be positive")
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
			if isSecretField(field) {
				oldStr = redactValue(oldField)
				newStr = redactValue(newField)
			}
			logger.Component("config").Debug(context.Background(), "config_field_changed", "Config field changed",
				zap.String("field", fieldName),
				zap.String("old_value", boundedDebugValue(oldStr)),
				zap.String("new_value", boundedDebugValue(newStr)))
		}
	}
}

// isSecretField reports whether a config field must never be logged in clear text.
// debugValueLimit matches the shared preview limit: config values can contain
// prompts or other long text, so DEBUG is not a full-text exemption.
const debugValueLimit = 512

// boundedDebugValue truncates a value to the preview limit.
func boundedDebugValue(s string) string {
	n := 0
	for i := range s {
		if n == debugValueLimit {
			return s[:i] + "…"
		}
		n++
	}
	return s
}

func isSecretField(field reflect.StructField) bool {
	key := strings.ToLower(strings.ReplaceAll(field.Tag.Get("mapstructure"), "-", "_"))
	key = strings.TrimSuffix(key, ",omitempty")
	switch key {
	case "api_key", "password", "key":
		return true
	}
	return false
}

// redactValue renders a secret as a fixed placeholder without revealing its value.
func redactValue(v reflect.Value) string {
	if v.Kind() == reflect.String && v.String() == "" {
		return "(empty)"
	}
	return "[REDACTED]"
}

// formatValue formats a reflect.Value for logging.
func formatValue(v reflect.Value) string {
	if v.Kind() == reflect.Slice {
		return fmt.Sprintf("%v", v.Interface())
	}
	return fmt.Sprintf("%v", v.Interface())
}

// legacyStylePlayResX and legacyStylePlayResY are the script canvas dimensions
// used by style configs written before play_res_x/play_res_y were configurable.
const (
	legacyStylePlayResX = 384
	legacyStylePlayResY = 288
)

// loadConfig reads, defaults, migrates, and validates a config file.
// Each call uses a fresh Viper instance so reloads cannot inherit stale keys.
func loadConfig(path string) (*Config, error) {
	v := viper.New()
	v.SetConfigFile(path)
	v.SetConfigType("yaml")

	// Environment variable override support
	v.SetEnvPrefix("FUSIONN")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	setDefaults(v)

	if err := v.ReadInConfig(); err != nil {
		return nil, err
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, err
	}

	cfg.Subtitle.Engine = resolveEngine(v, cfg.Subtitle.Engine)
	migrateLegacyStyle(v, &cfg.Subtitle.ASSStyle)

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// setDefaults installs defaults for every supported key. Keys with empty-string
// defaults are still registered so FUSIONN_* environment overrides bind during
// Unmarshal; explicit file values (including false and 0) always win.
func setDefaults(v *viper.Viper) {
	// Registering an empty default lets FUSIONN_SUBTITLE_ENGINE bind during
	// Unmarshal. An empty resolved value then falls through to the legacy
	// detection in resolveEngine.
	v.SetDefault("subtitle.engine", "")

	v.SetDefault("subtitle.llm.base_url", "")
	v.SetDefault("subtitle.llm.model", "")
	v.SetDefault("subtitle.llm.api_key", "")
	v.SetDefault("subtitle.llm.api_key_env", "")
	v.SetDefault("subtitle.llm.reasoning_effort", "low")
	v.SetDefault("subtitle.llm.response_format", ResponseFormatJSONObject)
	v.SetDefault("subtitle.llm.timeout_seconds", 300)
	v.SetDefault("subtitle.llm.max_tokens", 0)
	v.SetDefault("subtitle.llm.workers", 3)
	v.SetDefault("subtitle.llm.retries", 1)
	v.SetDefault("subtitle.llm.window_seconds", 210)
	v.SetDefault("subtitle.llm.overlap_seconds", 15)
	v.SetDefault("subtitle.llm.cache_enabled", true)
	v.SetDefault("subtitle.llm.cache_dir", "")
	v.SetDefault("subtitle.llm.mapping_path", "")
	v.SetDefault("subtitle.llm.en_chars", 65)
	v.SetDefault("subtitle.llm.zh_chars", 25)
	v.SetDefault("subtitle.llm.en_gap_ms", 400)
	v.SetDefault("subtitle.llm.zh_gap_ms", 500)
	v.SetDefault("subtitle.llm.span_ms", 5500)
	v.SetDefault("subtitle.llm.fill_gap_ms", 500)
	v.SetDefault("subtitle.llm.word_align_url", "")

	v.SetDefault("subtitle.ass_style.enabled", true)
	v.SetDefault("subtitle.ass_style.play_res_x", 1920)
	v.SetDefault("subtitle.ass_style.play_res_y", 1080)
	v.SetDefault("subtitle.ass_style.primary_font", "PingFang SC")
	v.SetDefault("subtitle.ass_style.primary_size", 52)
	v.SetDefault("subtitle.ass_style.primary_color", "&H08EFECEA")
	v.SetDefault("subtitle.ass_style.secondary_font", "Helvetica Neue")
	v.SetDefault("subtitle.ass_style.secondary_size", 36)
	v.SetDefault("subtitle.ass_style.secondary_color", "&H087BC0E5")
	v.SetDefault("subtitle.ass_style.bold", false)
	v.SetDefault("subtitle.ass_style.outline", 1.4)
	v.SetDefault("subtitle.ass_style.outline_color", "&H30141414")
	v.SetDefault("subtitle.ass_style.shadow", 1.5)
	v.SetDefault("subtitle.ass_style.back_color", "&H80000000")
	v.SetDefault("subtitle.ass_style.margin_v", 32)
	v.SetDefault("subtitle.ass_style.margin_left", 80)
	v.SetDefault("subtitle.ass_style.margin_right", 80)
	v.SetDefault("subtitle.ass_style.scale_x", 100)
	v.SetDefault("subtitle.ass_style.spacing", 0.0)
	v.SetDefault("subtitle.ass_style.line_height_ratio", 1.23)
	v.SetDefault("subtitle.ass_style.block_padding", 16)
	v.SetDefault("subtitle.ass_style.collision_gap", 12)
}

// resolveEngine applies engine defaults while preserving explicit values.
// Explicit engine (file or environment) wins; otherwise a legacy config that
// contains a duosubs section keeps duosubs, and new configs use llm.
func resolveEngine(v *viper.Viper, raw string) string {
	engine := strings.ToLower(strings.TrimSpace(raw))
	if engine != "" {
		return engine
	}
	if v.InConfig("subtitle.duosubs") {
		return EngineDuoSubs
	}
	return EngineLLM
}

// fieldExplicit reports whether a key was set in the file or environment
// rather than coming from defaults.
func fieldExplicit(v *viper.Viper, key string) bool {
	if v.InConfig(key) {
		return true
	}
	envKey := "FUSIONN_" + strings.ToUpper(strings.ReplaceAll(key, ".", "_"))
	return os.Getenv(envKey) != ""
}

// migrateLegacyStyle converts style values written for the legacy 384x288
// script canvas to the configured play_res_x/play_res_y canvas.
//
// Configs without an explicit play_res_y are treated as legacy: explicit
// primary_size, secondary_size, margin_v, outline, and shadow values scale by
// play_res_y/288 (3.75 at the default 1920x1080 canvas); margin_left and
// margin_right are horizontal and scale by play_res_x/384 (5 at 1920). Fields
// left unset keep their new defaults, explicit zero stays zero, and setting
// play_res_y explicitly opts out of the migration.
func migrateLegacyStyle(v *viper.Viper, style *ASSStyleConfig) {
	if fieldExplicit(v, "subtitle.ass_style.play_res_y") {
		return
	}

	scaleY := float64(style.PlayResY) / legacyStylePlayResY
	scaleX := float64(style.PlayResX) / legacyStylePlayResX
	scaleInt := func(key string, scale float64, target *int) {
		if fieldExplicit(v, key) {
			*target = int(math.Round(float64(*target) * scale))
		}
	}
	scaleFloat := func(key string, scale float64, target *float64) {
		if fieldExplicit(v, key) {
			*target *= scale
		}
	}

	scaleInt("subtitle.ass_style.primary_size", scaleY, &style.PrimarySize)
	scaleInt("subtitle.ass_style.secondary_size", scaleY, &style.SecondarySize)
	scaleInt("subtitle.ass_style.margin_v", scaleY, &style.MarginV)
	scaleInt("subtitle.ass_style.margin_left", scaleX, &style.MarginLeft)
	scaleInt("subtitle.ass_style.margin_right", scaleX, &style.MarginRight)
	scaleFloat("subtitle.ass_style.outline", scaleY, &style.Outline)
	scaleFloat("subtitle.ass_style.shadow", scaleY, &style.Shadow)
}

// Load is a convenience function for one-time loading (backwards compatible).
func Load(path string) (*Config, error) {
	return loadConfig(path)
}
