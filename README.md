# Subtitle Matching and Clustering Tool

This tool is designed to match and cluster subtitles from two different languages (specifically Chinese and English) based on their timing. It aims to synchronize subtitles for bilingual video content, making it easier to create accurate and time-aligned subtitles for viewers.

## Features

- **Subtitle Matching**: Matches subtitles from Chinese and English based on their start and end times, ensuring they are synchronized for bilingual content.
- **Cue Clustering**: Clusters matched subtitles to create a single, unified subtitle file that combines both languages, making it easier for viewers to follow along.
- **Time Tolerance Adjustment**: Allows for customization of the time tolerance for matching subtitles, accommodating slight discrepancies in subtitle timings.

## How It Works

The tool processes two sets of subtitles (Chinese and English) and performs the following operations:

1. **Sorting**: Sorts both sets of subtitles by their start times to prepare them for matching.
2. **Matching**: Matches subtitles from the two sets based on a specified time tolerance. If the start or end times of subtitles from both languages are within the tolerance, they are considered a match.
3. **Merging**: Merges matched subtitles into a single item, preserving the timing and content from both languages.
4. **Clustering**: Adds unmatched subtitles as-is into the final output to ensure no content is lost.

## Usage

To use this tool, you need to have Go installed on your machine. After cloning the repository, you can integrate the provided functions into your Go project to match and cluster subtitles.

### Example

```go
package main

import (
    "time"
    // Assume "subtitlematcher" is the package name
    "subtitlematcher"
)

func main() {
    // Load your Chinese and English subtitle items
    chineseItems := []*astisub.Item{...}
    englishItems := []*astisub.Item{...}

    // Specify the time tolerance for matching
    timeTolerance := time.Duration(2 * time.Second)

    // Match and cluster subtitles
    mergedItems := subtitlematcher.MatchSubtitlesCueClustering(chineseItems, englishItems, timeTolerance)

    // Output or further process mergedItems
}
