package service

import (
	"context"
	"fusionn/internal/cache"
	"fusionn/pkg"
)

type Facade interface {
	GetSeriesEpisodeOverview(ctx context.Context, seriesID int, season int, episode int) (string, error)
}

type facade struct {
	cache cache.RedisClient
	tvdb  pkg.TVDB
}

func NewFacade(cache cache.RedisClient, tvdb pkg.TVDB) *facade {
	return &facade{cache: cache, tvdb: tvdb}
}

func (f *facade) GetSeriesEpisodeOverview(ctx context.Context, seriesID int, season int, episode int) (string, error) {
	series, err := f.tvdb.GetSeriesEpisodes(ctx, seriesID)
	if err != nil {
		return "", err
	}

	for _, each := range series.Data.Episodes {
		if each.SeasonNumber == season && each.Number == episode {
			return each.Overview, nil
		}
	}

	return "", nil
}
