package config

const (
	// LLM configuration keys
	LLM_BASE     = "llm.base"
	LLM_ENDPOINT = "llm.endpoint"
	LLM_API_KEY  = "llm.api_key"
	LLM_MODEL    = "llm.model"
	LLM_LANGUAGE = "llm.language"

	// Apprise configuration keys
	APPRISE_ENABLED = "apprise.enabled"
	APPRISE_URL     = "apprise.url"

	// SQLite configuration keys
	SQLITE_ENABLED = "sqlite.enabled"
	SQLITE_PATH    = "sqlite.path"

	// Translate configuration keys
	TRANSLATE_ENABLED  = "translate.enabled"
	TRANSLATE_PROVIDER = "translate.provider"

	// DeepLX configuration keys
	DEEPLX_URL   = "deeplx.url"
	DEEPLX_LOCAL = "deeplx.local"

	// Redis configuration keys
	REDIS_ADDR     = "redis.addr"
	REDIS_PASSWORD = "redis.password"
	REDIS_DB       = "redis.db"
)
