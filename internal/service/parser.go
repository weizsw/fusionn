package service

import (
	"fusionn/logger"
	"regexp"
	"strings"

	astisub "github.com/asticode/go-astisub"
)

type Parser interface {
	Parse(input string) (*astisub.Subtitles, error)
	RemoveSDH(subs *astisub.Subtitles) *astisub.Subtitles
}

type parser struct{}

func NewParser() *parser {
	return &parser{}
}

func (p *parser) Parse(input string) (*astisub.Subtitles, error) {
	if input == "" {
		logger.Sugar.Infof("input is empty")
		return nil, nil
	}
	s, err := astisub.OpenFile(input)
	if err != nil {
		return nil, err
	}

	for _, item := range s.Items {
		if len(item.Lines) > 1 {
			mergedText := ""
			for _, line := range item.Lines {
				for _, lineItem := range line.Items {
					// Remove the leading "-" from the text and trim spaces
					lineItem.Text = strings.TrimSpace(strings.TrimPrefix(lineItem.Text, "-"))
					mergedText += strings.TrimSpace(lineItem.Text) + " "
				}
			}
			// Trim the final merged text to remove any trailing spaces
			item.Lines[0].Items[0].Text = strings.TrimSpace(mergedText)
			item.Lines = item.Lines[:1] // Keep only the first line after merging
		}
	}

	return s, nil
}

func (p *parser) RemoveSDH(subs *astisub.Subtitles) *astisub.Subtitles {
	sdhPattern := regexp.MustCompile(`\[.*?\]|\(.*?\)`)
	cleanedSubs := &astisub.Subtitles{}

	// Iterate over the subtitles and remove SDH text
	for _, item := range subs.Items {
		cleanedItem := *item // Create a copy of the item to avoid modifying the original
		shouldAdd := false
		for _, line := range cleanedItem.Lines {
			for i := range line.Items {
				line.Items[i].Text = strings.TrimSpace(sdhPattern.ReplaceAllString(line.Items[i].Text, ""))
				if line.Items[i].Text != "" {
					shouldAdd = true
				}
			}
		}
		if shouldAdd {
			cleanedSubs.Items = append(cleanedSubs.Items, &cleanedItem)
		}
	}
	return cleanedSubs
}
