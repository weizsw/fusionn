package model

type TVDBSeries struct {
	Status string `json:"status"`
	Data   struct {
		Series struct {
			ID                   int      `json:"id"`
			Name                 string   `json:"name"`
			Slug                 string   `json:"slug"`
			Image                string   `json:"image"`
			NameTranslations     []string `json:"nameTranslations"`
			OverviewTranslations []string `json:"overviewTranslations"`
			Aliases              []string `json:"aliases"`
			FirstAired           string   `json:"firstAired"`
			LastAired            string   `json:"lastAired"`
			NextAired            string   `json:"nextAired"`
			Score                int      `json:"score"`
			Status               struct {
				ID          int    `json:"id"`
				Name        string `json:"name"`
				RecordType  string `json:"recordType"`
				KeepUpdated bool   `json:"keepUpdated"`
			} `json:"status"`
			OriginalCountry   string      `json:"originalCountry"`
			OriginalLanguage  string      `json:"originalLanguage"`
			DefaultSeasonType int         `json:"defaultSeasonType"`
			IsOrderRandomized bool        `json:"isOrderRandomized"`
			LastUpdated       string      `json:"lastUpdated"`
			AverageRuntime    int         `json:"averageRuntime"`
			Episodes          interface{} `json:"episodes"`
			Overview          string      `json:"overview"`
			Year              string      `json:"year"`
		} `json:"series"`
		Episodes []struct {
			ID                   int         `json:"id"`
			SeriesID             int         `json:"seriesId"`
			Name                 string      `json:"name"`
			Aired                string      `json:"aired,omitempty"`
			Runtime              int         `json:"runtime"`
			NameTranslations     []string    `json:"nameTranslations"`
			Overview             string      `json:"overview,omitempty"`
			OverviewTranslations []string    `json:"overviewTranslations"`
			Image                string      `json:"image,omitempty"`
			ImageType            int         `json:"imageType,omitempty"`
			IsMovie              int         `json:"isMovie"`
			Seasons              interface{} `json:"seasons"`
			Number               int         `json:"number"`
			AbsoluteNumber       int         `json:"absoluteNumber"`
			SeasonNumber         int         `json:"seasonNumber"`
			LastUpdated          string      `json:"lastUpdated"`
			FinaleType           string      `json:"finaleType,omitempty"`
			AirsAfterSeason      int         `json:"airsAfterSeason,omitempty"`
			AirsBeforeSeason     int         `json:"airsBeforeSeason,omitempty"`
			AirsBeforeEpisode    int         `json:"airsBeforeEpisode,omitempty"`
			Year                 string      `json:"year,omitempty"`
		} `json:"episodes"`
	} `json:"data"`
	Links struct {
		Prev       interface{} `json:"prev"`
		Self       string      `json:"self"`
		Next       interface{} `json:"next"`
		TotalItems int         `json:"total_items"`
		PageSize   int         `json:"page_size"`
	} `json:"links"`
}

type TVDBLoginRequest struct {
	ApiKey string `json:"apikey"`
}

type TVDBLoginResponse struct {
	Status string `json:"status"`
	Data   struct {
		Token string `json:"token"`
	} `json:"data"`
}
