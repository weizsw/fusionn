package config

import (
	"fmt"
	"fusionn/logger"
	"reflect"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
	"github.com/jinzhu/copier"
	"github.com/r3labs/diff"
	"github.com/spf13/viper"
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

	logger.L.Info("Config loaded successfully\n" +
		fmt.Sprintf("file: %s\n", v.ConfigFileUsed()) +
		fmt.Sprintf("paths: %s\n", strings.Join([]string{".", "./configs"}, ",")) +
		prettyPrintConfig(c))

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

func prettyPrintConfig(v interface{}) string {
	var b strings.Builder
	val := reflect.ValueOf(v).Elem()
	typ := val.Type()

	for i := 0; i < val.NumField(); i++ {
		field := val.Field(i)
		fieldType := typ.Field(i)

		if !field.IsNil() {
			b.WriteString(fmt.Sprintf("\n%s:\n", fieldType.Name))
			structVal := field.Elem()
			structType := structVal.Type()

			for j := 0; j < structVal.NumField(); j++ {
				subField := structVal.Field(j)
				subFieldType := structType.Field(j)

				if strings.Contains(strings.ToLower(subFieldType.Name), "password") ||
					strings.Contains(strings.ToLower(subFieldType.Name), "secret") ||
					strings.Contains(strings.ToLower(subFieldType.Name), "key") {
					b.WriteString(fmt.Sprintf("  %q: \"****\"\n", subFieldType.Name))
				} else {
					b.WriteString(fmt.Sprintf("  %q: %#v\n", subFieldType.Name, subField.Interface()))
				}
			}
		}
	}
	return b.String()
}
