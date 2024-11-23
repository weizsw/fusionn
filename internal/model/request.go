package model

type ExtractRequest struct {
	SonarrEpisodefilePath string `json:"sonarr_episodefile_path"`
}

type BatchRequest struct {
	Path string `json:"path"`
}
