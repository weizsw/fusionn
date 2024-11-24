package model

type ExtractRequest struct {
	SonarrEpisodefilePath string `json:"sonarr_episodefile_path"`
}

type BatchRequest struct {
	Path string `json:"path"`
}

type AsyncMergeRequest struct {
	ChsSubtilePath string `json:"chs_subtitle_path"`
	EngSubtilePath string `json:"eng_subtitle_path"`
}
