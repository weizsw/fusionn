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
	Enabled   bool `mapstructure:"enabled"`
	EmbedOnly bool `mapstructure:"embed_only"`
}

type StyleConfig struct {
	ChsStyle       *ChsStyleConfig `mapstructure:"chs_style"`
	EngStyle       *EngStyleConfig `mapstructure:"eng_style"`
	MarginLeft     int             `mapstructure:"margin_left"`
	MarginRight    int             `mapstructure:"margin_right"`
	MarginVertical int             `mapstructure:"margin_vertical"`
	Alignment      int             `mapstructure:"alignment"`
	BorderStyle    int             `mapstructure:"border_style"`
	WrapStyle      string          `mapstructure:"wrap_style"`
	Outline        float64         `mapstructure:"outline"`
	Shadow         float64         `mapstructure:"shadow"`
	SecondaryColor string          `mapstructure:"secondary_color"`
	OutlineColor   string          `mapstructure:"outline_color"`
	BackColor      string          `mapstructure:"back_color"`
}

type ChsStyleConfig struct {
	FontName     string  `mapstructure:"font_name"`
	FontSize     float64 `mapstructure:"font_size"`
	Bold         bool    `mapstructure:"bold"`
	PrimaryColor string  `mapstructure:"primary_color"`
}

type EngStyleConfig struct {
	FontName     string  `mapstructure:"font_name"`
	FontSize     float64 `mapstructure:"font_size"`
	Bold         bool    `mapstructure:"bold"`
	PrimaryColor string  `mapstructure:"primary_color"`
}

type AlgoConfig struct {
	MaxOverlappingSegments int `mapstructure:"max_overlapping_segments"`
}

type AfterConfig struct {
	ReduceMargin  bool   `mapstructure:"reduce_margin"`
	EngMargin     string `mapstructure:"eng_margin"`
	DefaultMargin string `mapstructure:"default_margin"`
}

type GeneralConfig struct {
	ForceSimplified bool `mapstructure:"force_simplified"`
}

type TVDBConfig struct {
	Enabled bool   `mapstructure:"enabled"`
	ApiKey  string `mapstructure:"api_key"`
}
