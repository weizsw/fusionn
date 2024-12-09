package consts

const (
	TMP_DIR               = "/tmp/"
	CHT_TITLE             = "Chinese (Traditional)"
	CHT_TITLE_II          = "Chinese Traditional"
	CHT_TITLE_III         = "中文（繁體）"
	CHT_TITLE_IV          = "Traditional"
	CHS_TITLE             = "Chinese (Simplified)"
	CHS_TITLE_II          = "Chinese Simplified"
	CHS_TITLE_III         = "中文（简体）"
	CHS_TITLE_IV          = "Simplified"
	ENG_TITLE             = ""
	ENG_TITLE_II          = "English"
	ENG_TITLE_III         = "English (SDH)"
	CHT_LAN               = "cht"
	CHI_LAN               = "chi"
	CHS_LAN               = "chs"
	ENG_LAN               = "eng"
	SDH_LAN               = "sdh"
	DUAL_LAN              = "dual"
	TIME_CODE_PATTERN     = `(\d{2}):(\d{2}):(\d{2}),(\d{3}) --> (\d{2}):(\d{2}):(\d{2}),(\d{3})`
	TIME_CODE_PATTERN_II  = `(\d{2}:\d{2}:\d{2},)(1000) --> (\d{2}:\d{2}:\d{2},\d{3})`
	TIME_CODE_PATTERN_III = `(\d{2}:\d{2}:\d{2},\d{3}) --> (\d{2}:\d{2}:\d{2},)(1000)`
	APPRISE               = "http://192.168.50.135:10000/notify/fusionn"
)

// Queue names
const (
	TRANSLATE_QUEUE = "translate_queue"
)

const (
	ASSFONTS_PATH = "/usr/local/bin/assfonts"
)
