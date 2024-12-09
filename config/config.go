package config

import (
	"fusionn/logger"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

var (
	C    *Config
	once sync.Once
)

type Config struct {
	viper     *viper.Viper
	Apprise   *AppriseConfig
	Translate *TranslateConfig
	Redis     *RedisConfig
	LLM       *LLMConfig
	DeepLX    *DeepLXConfig
	SQLite    *SQLiteConfig
}

// MustLoad ensures config is loaded before server starts
func MustLoad() {
	once.Do(func() {
		cfg := &Config{
			viper: viper.New(),
		}
		if err := cfg.Load(); err != nil {
			logger.S.Fatalw("Failed to load config",
				"error", err)
		}
		C = cfg
	})
}

// Load initializes the configuration
func (c *Config) Load() error {
	logger.S.Debug("Loading configuration...")

	c.viper.SetConfigName("config")
	c.viper.SetConfigType("yml")
	c.viper.AddConfigPath(".")
	c.viper.AddConfigPath("./configs")

	if err := c.viper.ReadInConfig(); err != nil {
		logger.S.Errorw("Failed to read config file",
			"error", err,
			"paths", []string{".", "./configs"})
		return err
	}

	if err := c.viper.Unmarshal(c); err != nil {
		logger.S.Errorw("Failed to unmarshal config",
			"error", err)
		return err
	}

	logger.L.Info("Config loaded successfully",
		zap.String("file", c.viper.ConfigFileUsed()),
		zap.String("paths", strings.Join([]string{".", "./configs"}, ",")),
		zap.Any("config", c))

	// Add watcher after successful load
	c.viper.WatchConfig()
	c.viper.OnConfigChange(func(e fsnotify.Event) {
		logger.S.Infow("Config file changed, reloading...",
			"file", e.Name,
			"operation", e.Op.String())

		if err := c.viper.Unmarshal(c); err != nil {
			logger.S.Errorw("Failed to unmarshal updated config",
				"error", err)
			return
		}

		logger.S.Info("Config reloaded successfully")
	})

	return nil
}

// GetString returns string config value
func (c *Config) GetString(key string) string {
	return c.viper.GetString(key)
}

// GetInt returns integer config value
func (c *Config) GetInt(key string) int {
	return c.viper.GetInt(key)
}

// GetBool returns boolean config value
func (c *Config) GetBool(key string) bool {
	return c.viper.GetBool(key)
}
