package convertor

import (
	"errors"
	"fusionn/pkg/deepl"
	"sync"

	"github.com/asticode/go-astisub"
	"github.com/longbridgeapp/opencc"
)

type Convertor interface {
	ConvertToSimplified(sub *astisub.Subtitles) (*astisub.Subtitles, error)
	TranslateToSimplified(sub *astisub.Subtitles) (*astisub.Subtitles, error)
}

type convertor struct {
	deepl deepl.DeepL
}

var (
	t2s  *opencc.OpenCC
	once sync.Once
)

func getT2S() *opencc.OpenCC {
	once.Do(func() {
		t2s, _ = opencc.New("t2s")
	})
	return t2s
}

func (c *convertor) TranslateToSimplified(sub *astisub.Subtitles) (*astisub.Subtitles, error) {
	if sub == nil {
		return nil, errors.New("subtitles is nil")
	}

	var texts []string
	itemIndexMap := make(map[int][]int)

	for i, item := range sub.Items {
		for j, line := range item.Lines {
			for k, lineItem := range line.Items {
				texts = append(texts, lineItem.Text)
				itemIndexMap[len(texts)-1] = []int{i, j, k}
			}
		}
	}

	var translatedTexts []string
	for i := 0; i < len(texts); i += 50 {
		end := i + 50
		if end > len(texts) {
			end = len(texts)
		}

		batch := texts[i:end]
		translatedBatch, err := c.deepl.Translate(batch, "zh", "en")
		if err != nil {
			return nil, err
		}
		for _, translatedText := range translatedBatch.Translations {
			translatedTexts = append(translatedTexts, translatedText.Text)
		}
	}

	for i, translatedText := range translatedTexts {
		indices := itemIndexMap[i]
		sub.Items[indices[0]].Lines[indices[1]].Items[indices[2]].Text = translatedText
	}
	return sub, nil
}

func (c *convertor) ConvertToSimplified(sub *astisub.Subtitles) (*astisub.Subtitles, error) {
	if sub == nil {
		return nil, errors.New("subtitles is nil")
	}

	for _, item := range sub.Items {
		item.Lines = convertLines(item.Lines)
	}
	return sub, nil
}

func convertLines(lines []astisub.Line) []astisub.Line {
	for _, line := range lines {
		for _, item := range line.Items {
			item.Text = convertText(item.Text)
		}
	}
	return lines
}

func convertText(text string) string {
	t2s := getT2S()
	text, _ = t2s.Convert(text)
	return text
}
