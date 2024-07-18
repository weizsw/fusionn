package configs

import (
	"sync"

	"github.com/gofiber/fiber/v2/log"
	"github.com/spf13/viper"
)

var (
	C    *Config
	once sync.Once
)

type Config struct {
	viper *viper.Viper
}

func init() {
	once.Do(func() {
		C = &Config{
			viper: viper.New(),
		}
		C.Load()
	})
}

func (c *Config) Load() {
	c.viper.SetConfigName("config")
	c.viper.SetConfigType("yaml")
	c.viper.AddConfigPath(".")
	err := c.viper.ReadInConfig()
	if err != nil {
		panic(err)
	}

	log.Info("Using config file:", c.viper.ConfigFileUsed())
}

// Add methods to access configuration values
func (c *Config) GetString(key string) string {
	return c.viper.GetString(key)
}

func (c *Config) GetInt(key string) int {
	return c.viper.GetInt(key)
}

func (c *Config) GetBool(key string) bool {
	return c.viper.GetBool(key)
}
