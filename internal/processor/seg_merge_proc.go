package processor

import (
	"context"
	"fusionn/internal/model"
	"fusionn/internal/service"

	"github.com/asticode/go-astisub"
	"github.com/mohae/deepcopy"
)

type SegMergeStage struct {
	algo service.Algo
}

func NewSegMergeStage(algo service.Algo) *SegMergeStage {
	return &SegMergeStage{
		algo: algo,
	}
}

func (s *SegMergeStage) Process(ctx context.Context, input any) (any, error) {
	req, ok := input.(*model.ParsedSubtitles)
	if !ok {
		return nil, ErrInvalidInput
	}

	merged := deepcopy.Copy(req.ChsSubtitle)
	mergedSubs, ok := merged.(*astisub.Subtitles)
	if !ok {
		return nil, ErrInvalidInput
	}

	mergedItems := s.algo.MatchSubtitleSegment(req.ChsSubtitle.Items, req.EngSubtitle.Items)
	mergedSubs.Items = mergedItems
	req.MergeSubtitle = mergedSubs
	return req, nil
}
