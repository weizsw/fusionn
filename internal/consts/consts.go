package consts

const (
	TMP_DIR               = "/tmp/"
	CHT_TITLE             = "Chinese (Traditional)"
	CHT_TITLE_II          = "Chinese Traditional"
	CHS_TITLE             = "Chinese (Simplified)"
	CHS_TITLE_II          = "Chinese Simplified"
	ENG_TITLE             = ""
	ENG_TITLE_II          = "English"
	CHT_LAN               = "cht"
	CHI_LAN               = "chi"
	CHS_LAN               = "chs"
	ENG_LAN               = "eng"
	DUAL_LAN              = "dual"
	TIME_CODE_PATTERN     = `(\d{2}):(\d{2}):(\d{2}),(\d{3}) --> (\d{2}):(\d{2}):(\d{2}),(\d{3})`
	TIME_CODE_PATTERN_II  = `(\d{2}:\d{2}:\d{2},)(1000) --> (\d{2}:\d{2}:\d{2},\d{3})`
	TIME_CODE_PATTERN_III = `(\d{2}:\d{2}:\d{2},\d{3}) --> (\d{2}:\d{2}:\d{2},)(1000)`
	APPRISE               = "http://192.168.50.163:10000/notify/fusionn"
)
