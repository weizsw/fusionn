package pkg

import (
	"bytes"
	"fusionn/internal/consts"
	"fusionn/logger"
	"io"
	"net/http"

	"github.com/bytedance/sonic"
)

type DeepL interface {
	Translate(text []string, targetLang, sourceLang string) (*deepLTranslateResp, error)
}
type deepL struct {
}

func NewDeepL() *deepL {
	return &deepL{}
}

type deepLTranslateReq struct {
	Text        []string `json:"text"`
	TargetLang  string   `json:"target_lang"`
	SourceLang  string   `json:"source_lang"`
	TagHandling string   `json:"tag_handling"`
	IgnoreTags  []string `json:"ignore_tags"`
}

type deepLTranslateResp struct {
	Translations []*translations `json:"translations"`
}

type translations struct {
	DetectedSourceLanguage string `json:"detected_source_language"`
	Text                   string `json:"text"`
}

func (d *deepL) Translate(text []string, targetLang, sourceLang string) (*deepLTranslateResp, error) {
	cmd := consts.CMDDeepLTranslate

	reqBody := deepLTranslateReq{
		Text:        text,
		TargetLang:  targetLang,
		SourceLang:  sourceLang,
		TagHandling: "xml",
	}
	reqBodyByte, err := sonic.Marshal(reqBody)
	if err != nil {
		logger.S.Fatalf("Error marshaling request body: %s", err)
		return nil, err
	}

	req, err := http.NewRequest("POST", cmd, bytes.NewBuffer(reqBodyByte))
	if err != nil {
		logger.S.Fatalf("Error creating request: %s", err)
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "DeepL-Auth-Key 6ec98a4c-52f1-a773-d4a6-7606a3720c3f:fx")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode != 200 {
		logger.S.Fatalf("Error sending request: %s", err)
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.S.Fatalf("Error reading response body: %s", err)
		return nil, err
	}

	var translateResp deepLTranslateResp
	err = sonic.Unmarshal(body, &translateResp)
	if err != nil {
		logger.S.Fatalf("Error unmarshaling response body: %s", err)
		return nil, err
	}

	return &translateResp, nil
}
