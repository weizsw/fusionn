package model

type ExtractRequest struct {
	FilePath       string `json:"file_path"`
	SeriesTVDBID   string `json:"series_tvdbid"`
	SeasonNumber   string `json:"season_number"`
	EpisodeNumbers string `json:"episode_numbers"`
}

type BatchRequest struct {
	Path string `json:"path"`
}

type AsyncMergeRequest struct {
	ChsSubtilePath string `json:"chs_subtitle_path"`
	EngSubtilePath string `json:"eng_subtitle_path"`
	VideoPath      string `json:"video_path"`
}
