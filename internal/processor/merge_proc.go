package processor

import (
	"context"
	"fusionn/internal/model"
	"fusionn/internal/service"
	"fusionn/logger"
	"time"

	"github.com/asticode/go-astisub"
	"github.com/mohae/deepcopy"
)

type MergeStage struct {
	algo service.Algo
}

func NewMergeStage(algo service.Algo) *MergeStage {
	return &MergeStage{
		algo: algo,
	}
}

func (s *MergeStage) Process(ctx context.Context, input any) (any, error) {
	req, ok := input.(*model.ParsedSubtitles)
	if !ok {
		return nil, ErrInvalidInput
	}

	logger.L.Info("[MergeStage] merging subtitles")

	merged := deepcopy.Copy(req.ChsSubtitle)
	mergedSubs, ok := merged.(*astisub.Subtitles)
	if !ok {
		return nil, ErrInvalidInput
	}
	mergedSubs.Items = s.algo.MatchSubtitleCueClustering(req.ChsSubtitle.Items, req.EngSubtitle.Items, 1000*time.Millisecond)
	req.MergeSubtitle = mergedSubs

	return req, nil
}
