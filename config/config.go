package config

import (
	"fusionn/logger"
	"sync"

	"github.com/spf13/viper"
)

var (
	C    *Config
	once sync.Once
)

type Config struct {
	viper *viper.Viper
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

	logger.S.Infow("Config loaded successfully",
		"file", c.viper.ConfigFileUsed(),
		"paths", []string{".", "./configs"})
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
