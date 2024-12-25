package model

type ExtractRequest struct {
	SonarrEpisodefilePath           string `json:"sonarr_episodefile_path"`
	SonarrSeriesTVDBID              string `json:"sonarr_series_tvdbid"`
	SonarrEpisodefileSeasonNumber   string `json:"sonarr_episodefile_seasonnumber"`
	SonarrEpisodefileEpisodeNumbers string `json:"sonarr_episodefile_episodenumbers"`
}

type BatchRequest struct {
	Path string `json:"path"`
}

type AsyncMergeRequest struct {
	ChsSubtilePath string `json:"chs_subtitle_path"`
	EngSubtilePath string `json:"eng_subtitle_path"`
}
