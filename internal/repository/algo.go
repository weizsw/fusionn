package repository

import (
	"fmt"
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
	usedEnglish := make(map[int]bool)

	for _, cnItem := range chineseItems {
		var bestMatch *astisub.Item
		var bestMatchIdx int
		var bestOverlap time.Duration
		var bestTimeDiff time.Duration

		for idx, enItem := range englishItems {
			if usedEnglish[idx] {
				continue
			}

			overlap := overlapDuration(cnItem.StartAt, cnItem.EndAt, enItem.StartAt, enItem.EndAt)
			timeDiff := absDuration(cnItem.StartAt - enItem.StartAt)

			if overlap > 0 || timeDiff <= timeTolerance {
				enItemDuration := enItem.EndAt - enItem.StartAt

				if bestMatch == nil ||
					overlap > bestOverlap ||
					(overlap == bestOverlap && timeDiff < bestTimeDiff) ||
					(overlap == bestOverlap && timeDiff == bestTimeDiff && enItemDuration < bestMatch.EndAt-bestMatch.StartAt) {
					bestMatch = enItem
					bestMatchIdx = idx
					bestOverlap = overlap
					bestTimeDiff = timeDiff
				}
			}
		}

		mergedText := cnItem.String()
		if bestMatch != nil {
			mergedText += fmt.Sprintf(" %s", bestMatch.String())
			usedEnglish[bestMatchIdx] = true
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

	// Append remaining unmatched English subtitles to the best-matched item
	for idx, enItem := range englishItems {
		if !usedEnglish[idx] {
			// Find the best-matched item to append this English subtitle
			appendIndex := sort.Search(len(mergedItems), func(i int) bool {
				return mergedItems[i].StartAt > enItem.StartAt
			}) - 1

			// Append the English subtitle to the best-matched item
			if appendIndex >= 0 {
				mergedItems[appendIndex].Lines[0].Items[0].Text += fmt.Sprintf(" %s", enItem.String())
			} else {
				// If no best-matched item is found, add as a new item
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
	}

	return mergedItems
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
