package config

import (
	"fusionn/logger"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
	"github.com/jinzhu/copier"
	"github.com/r3labs/diff"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

var (
	C    *Config
	once sync.Once
)

type Config struct {
	Apprise   *AppriseConfig
	Translate *TranslateConfig
	Redis     *RedisConfig
	LLM       *LLMConfig
	DeepLX    *DeepLXConfig
	SQLite    *SQLiteConfig
	Subset    *SubsetConfig
	Style     *StyleConfig
	Algo      *AlgoConfig
}

// MustLoad ensures config is loaded before server starts
func MustLoad() {
	once.Do(func() {
		cfg := &Config{}
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

	v := viper.New()
	v.SetConfigName("config")
	v.SetConfigType("yml")
	v.AddConfigPath(".")
	v.AddConfigPath("./configs")

	if err := v.ReadInConfig(); err != nil {
		logger.S.Errorw("Failed to read config file",
			"error", err,
			"paths", []string{".", "./configs"})
		return err
	}

	if err := v.Unmarshal(c); err != nil {
		logger.S.Errorw("Failed to unmarshal config",
			"error", err)
		return err
	}

	logger.L.Info("Config loaded successfully",
		zap.String("file", v.ConfigFileUsed()),
		zap.String("paths", strings.Join([]string{".", "./configs"}, ",")),
		zap.Any("config", c))

	// Watch config changes
	v.WatchConfig()
	v.OnConfigChange(func(e fsnotify.Event) {
		logger.S.Infow("Config file changed, reloading...",
			"file", e.Name,
			"operation", e.Op.String())

		// Create a deep copy of the current config
		var oldConfig Config
		if err := copier.Copy(&oldConfig, c); err != nil {
			logger.S.Errorw("Failed to create config backup",
				"error", err)
			return
		}

		if err := v.Unmarshal(c); err != nil {
			logger.S.Errorw("Failed to unmarshal updated config",
				"error", err)
			return
		}

		// Log the differences
		logConfigDiff(&oldConfig, c)

		logger.S.Info("Config reloaded successfully")
	})

	return nil
}

// logConfigDiff logs the differences between old and new configs
func logConfigDiff(old, new *Config) {
	changelog, err := diff.Diff(old, new)
	if err != nil {
		logger.S.Errorw("Failed to compare configs", "error", err)
		return
	}

	if len(changelog) > 0 {
		logger.S.Infow("Config changes detected", "changes", changelog)
	}
}

// GetString returns string config value
func (c *Config) GetString(key string) string {
	return viper.GetString(key)
}

// GetInt returns integer config value
func (c *Config) GetInt(key string) int {
	return viper.GetInt(key)
}

// GetBool returns boolean config value
func (c *Config) GetBool(key string) bool {
	return viper.GetBool(key)
}
