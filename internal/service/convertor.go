package service

import (
	"errors"
	"fusionn/pkg"
	"sync"

	"github.com/asticode/go-astisub"
	"github.com/bytedance/sonic"
	"github.com/longbridgeapp/opencc"
)

type Convertor interface {
	ConvertToSimplified(sub *astisub.Subtitles) (*astisub.Subtitles, error)
	TranslateToSimplified(sub *astisub.Subtitles) (*astisub.Subtitles, error)
}

type convertor struct {
	deepl pkg.DeepL
}

func NewConvertor(d pkg.DeepL) *convertor {
	return &convertor{
		deepl: d,
	}
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

	newSub := &astisub.Subtitles{}
	var texts []string
	itemIndexMap := make(map[int][]int)

	b, _ := sonic.Marshal(sub)
	_ = sonic.Unmarshal(b, newSub)

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
		newSub.Items[indices[0]].Lines[indices[1]].Items[indices[2]].Text = translatedText
	}
	return newSub, nil
}

func (c *convertor) ConvertToSimplified(sub *astisub.Subtitles) (*astisub.Subtitles, error) {
	if sub == nil {
		return nil, errors.New("subtitles is nil")
	}

	for _, item := range sub.Items {
		convertLines(item.Lines)
	}

	return sub, nil
}

func convertLines(lines []astisub.Line) {
	for i := range lines {
		for j := range lines[i].Items {
			lines[i].Items[j].Text = convertText(lines[i].Items[j].Text)
		}
	}
}

func convertText(text string) string {
	t2s := getT2S()
	text, _ = t2s.Convert(text)
	return text
}
