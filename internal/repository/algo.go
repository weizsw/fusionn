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
	var mergedItems []*astisub.Item

	// Sort both slices by StartAt time
	sort.Slice(chineseItems, func(i, j int) bool {
		return chineseItems[i].StartAt < chineseItems[j].StartAt
	})
	sort.Slice(englishItems, func(i, j int) bool {
		return englishItems[i].StartAt < englishItems[j].StartAt
	})

	i, j := 0, 0
	for i < len(chineseItems) && j < len(englishItems) {
		if abs(chineseItems[i].StartAt-englishItems[j].StartAt) <= timeTolerance ||
			abs(chineseItems[i].EndAt-englishItems[j].EndAt) <= timeTolerance {
			// Merge the subtitles
			merged := mergeItems(chineseItems[i], englishItems[j])
			mergedItems = append(mergedItems, merged)
			i++
			j++
		} else if chineseItems[i].StartAt < englishItems[j].StartAt {
			mergedItems = append(mergedItems, chineseItems[i])
			i++
		} else {
			mergedItems = append(mergedItems, englishItems[j])
			j++
		}
	}

	// Append any remaining items
	mergedItems = append(mergedItems, chineseItems[i:]...)
	mergedItems = append(mergedItems, englishItems[j:]...)

	return mergedItems
}

func mergeItems(chinese, english *astisub.Item) *astisub.Item {
	merged := *chinese // Create a copy of the Chinese item

	// Append English lines to the existing Chinese lines
	for _, englishLine := range english.Lines {
		merged.Lines = append(merged.Lines, englishLine)
	}

	return &merged
}

func abs(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
