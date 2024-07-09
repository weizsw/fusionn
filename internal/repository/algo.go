package repository

import (
	"sort"
	"time"

	"github.com/asticode/go-astisub"
)

type IAlgo interface {
	MatchSubtitlesCueClustering(chineseItems, englishItems []*astisub.Item, timeTolerance time.Duration) []*astisub.Item
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
	usedChinese := make(map[int]bool)
	usedEnglish := make(map[int]bool)

	// First pass: Match Chinese to English
	for cnIdx, cnItem := range chineseItems {
		if usedChinese[cnIdx] {
			continue
		}

		matchedEnItems := make([]*astisub.Item, 0)
		matchedEnIndices := make([]int, 0)

		for enIdx, enItem := range englishItems {
			if usedEnglish[enIdx] {
				continue
			}

			overlap := overlapDuration(cnItem.StartAt, cnItem.EndAt, enItem.StartAt, enItem.EndAt)
			timeDiff := absDuration(cnItem.StartAt - enItem.StartAt)

			if overlap > 0 || timeDiff <= timeTolerance {
				matchedEnItems = append(matchedEnItems, enItem)
				matchedEnIndices = append(matchedEnIndices, enIdx)
			}
		}

		if len(matchedEnItems) > 0 {
			mergedText := cnItem.String()
			for _, enItem := range matchedEnItems {
				mergedText += " " + enItem.String()
			}

			endAt := cnItem.EndAt
			if len(matchedEnItems) > 1 {
				endAt = matchedEnItems[len(matchedEnItems)-1].EndAt
			}

			newItem := &astisub.Item{
				StartAt: cnItem.StartAt,
				EndAt:   endAt,
				Lines: []astisub.Line{
					{Items: []astisub.LineItem{{Text: mergedText}}},
				},
			}
			mergedItems = append(mergedItems, newItem)

			usedChinese[cnIdx] = true
			for _, idx := range matchedEnIndices {
				usedEnglish[idx] = true
			}
		}
	}

	// Second pass: Match English to remaining Chinese
	for enIdx, enItem := range englishItems {
		if usedEnglish[enIdx] {
			continue
		}

		matchedCnItems := make([]*astisub.Item, 0)
		matchedCnIndices := make([]int, 0)

		for cnIdx, cnItem := range chineseItems {
			if usedChinese[cnIdx] {
				continue
			}

			overlap := overlapDuration(enItem.StartAt, enItem.EndAt, cnItem.StartAt, cnItem.EndAt)
			timeDiff := absDuration(enItem.StartAt - cnItem.StartAt)

			if overlap > 0 || timeDiff <= timeTolerance {
				matchedCnItems = append(matchedCnItems, cnItem)
				matchedCnIndices = append(matchedCnIndices, cnIdx)
			}
		}

		if len(matchedCnItems) > 0 {
			mergedText := enItem.String()
			for _, cnItem := range matchedCnItems {
				mergedText = cnItem.String() + " " + mergedText
			}

			startAt := enItem.StartAt
			if len(matchedCnItems) > 0 {
				startAt = matchedCnItems[0].StartAt
			}

			newItem := &astisub.Item{
				StartAt: startAt,
				EndAt:   enItem.EndAt,
				Lines: []astisub.Line{
					{Items: []astisub.LineItem{{Text: mergedText}}},
				},
			}
			mergedItems = append(mergedItems, newItem)

			usedEnglish[enIdx] = true
			for _, idx := range matchedCnIndices {
				usedChinese[idx] = true
			}
		}
	}

	// Add remaining unmatched subtitles
	for idx, item := range chineseItems {
		if !usedChinese[idx] {
			mergedItems = append(mergedItems, item)
		}
	}

	for idx, item := range englishItems {
		if !usedEnglish[idx] {
			mergedItems = append(mergedItems, item)
		}
	}

	// Sort the merged items by start time
	sort.Slice(mergedItems, func(i, j int) bool {
		return mergedItems[i].StartAt < mergedItems[j].StartAt
	})

	return mergedItems
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
