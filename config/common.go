package config

type AppriseConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Url     string `mapstructure:"url"`
}

type TranslateConfig struct {
	Enabled  bool   `mapstructure:"enabled"`
	Provider string `mapstructure:"provider"`
	Url      string `mapstructure:"url"`
}

type RedisConfig struct {
	Addr     string `mapstructure:"addr"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

type LLMConfig struct {
	Base     string `mapstructure:"base"`
	Endpoint string `mapstructure:"endpoint"`
	ApiKey   string `mapstructure:"api_key"`
	Model    string `mapstructure:"model"`
	Language string `mapstructure:"language"`
}

type DeepLXConfig struct {
	Local bool   `mapstructure:"local"`
	Url   string `mapstructure:"url"`
}

type SQLiteConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	Path    string `mapstructure:"path"`
}

type SubsetConfig struct {
	Enabled bool `mapstructure:"enabled"`
}

type StyleConfig struct {
	ChsFontName  string  `mapstructure:"chs_font_name"`
	EngFontName  string  `mapstructure:"eng_font_name"`
	ChsFontSize  float64 `mapstructure:"chs_font_size"`
	EngFontSize  float64 `mapstructure:"eng_font_size"`
	ChsBold      bool    `mapstructure:"chs_bold"`
	EngBold      bool    `mapstructure:"eng_bold"`
	ReduceMargin bool    `mapstructure:"reduce_margin"`
}
