package repository

import (
	"fmt"
	"time"

	"github.com/asticode/go-astisub"
)

type IAlgo interface {
	MatchSubtitlesCueClustering(englishItems, chineseItems []*astisub.Item, timeTolerance time.Duration) []*astisub.Item
}

type algo struct{}

func NewAlgo() *algo {
	return &algo{}
}

func overlapDuration(start1, end1, start2, end2 time.Duration) time.Duration {
	if start1 < start2 {
		start1 = start2
	}
	if end1 > end2 {
		end1 = end2
	}
	if start1 < end1 {
		return end1 - start1
	}
	return 0
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}

func (a *algo) MatchSubtitlesCueClustering(chineseItems, englishItems []*astisub.Item, timeTolerance time.Duration) []*astisub.Item {
	mergedItems := make([]*astisub.Item, 0, max(len(chineseItems), len(englishItems)))
	usedEnglish := make(map[int]bool)

	for _, cnItem := range chineseItems {
		var matchedEnglishItems []*astisub.Item

		for idx, enItem := range englishItems {
			if usedEnglish[idx] {
				continue
			}

			overlap := overlapDuration(cnItem.StartAt, cnItem.EndAt, enItem.StartAt, enItem.EndAt)
			if overlap > 0 || absDuration(cnItem.StartAt-enItem.StartAt) <= timeTolerance {
				matchedEnglishItems = append(matchedEnglishItems, enItem)
				usedEnglish[idx] = true
			}
		}

		mergedText := cnItem.String()
		for _, enItem := range matchedEnglishItems {
			mergedText += fmt.Sprintf("\n%s", enItem.String())
		}

		newItem := &astisub.Item{
			StartAt: cnItem.StartAt,
			EndAt:   cnItem.EndAt,
			Lines: []astisub.Line{
				{Items: []astisub.LineItem{{Text: mergedText}}},
			},
		}
		mergedItems = append(mergedItems, newItem)
	}

	// Append remaining unmatched English subtitles
	for idx, enItem := range englishItems {
		if !usedEnglish[idx] {
			newItem := &astisub.Item{
				StartAt: enItem.StartAt,
				EndAt:   enItem.EndAt,
				Lines: []astisub.Line{
					{Items: []astisub.LineItem{{Text: enItem.String()}}},
				},
			}
			mergedItems = append(mergedItems, newItem)
		}
	}

	return mergedItems
}
