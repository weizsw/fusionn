package pkg

import (
	"bytes"
	"errors"
	"fusionn/config"
	"fusionn/internal/consts"
	"fusionn/logger"
	"io"
	"net/http"

	"github.com/bytedance/sonic"
	remote "github.com/xiaoxuan6/deeplx"
	"golang.org/x/sync/errgroup"
)

type DeepL interface {
	Translate(text []string, targetLang, sourceLang string) (*deepLTranslateResp, error)
	TranslateDeepLX(text []string, targetLang, sourceLang string) (*deepLTranslateResp, error)
}
type deepL struct {
	client *http.Client
}

func NewDeepL() *deepL {
	client := &http.Client{
		Transport: NewLoggingRoundTripper(logger.L),
	}
	return &deepL{client: client}
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
		logger.S.Infof("Error marshaling request body: %s", err)
		return nil, err
	}

	req, err := http.NewRequest("POST", cmd, bytes.NewBuffer(reqBodyByte))
	if err != nil {
		logger.S.Infof("Error creating request: %s", err)
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "DeepL-Auth-Key 6ec98a4c-52f1-a773-d4a6-7606a3720c3f:fx")

	resp, err := d.client.Do(req)
	if err != nil || resp.StatusCode != 200 {
		logger.S.Infof("Error sending request: %s", err)
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.S.Infof("Error reading response body: %s", err)
		return nil, err
	}

	var translateResp deepLTranslateResp
	err = sonic.Unmarshal(body, &translateResp)
	if err != nil {
		logger.S.Infof("Error unmarshaling response body: %s", err)
		return nil, err
	}

	return &translateResp, nil
}

func (d *deepL) TranslateDeepLX(text []string, targetLang, sourceLang string) (*deepLTranslateResp, error) {
	local := config.C.DeepLX.Local
	if !local {
		translateResp := &deepLTranslateResp{
			Translations: make([]*translations, len(text)),
		}

		g := new(errgroup.Group)

		for i, t := range text {
			i, t := i, t
			g.Go(func() error {
				resp := remote.Translate(t, sourceLang, targetLang)
				if resp.Code != 200 {
					resp.Data = t
				}

				translateResp.Translations[i] = &translations{
					DetectedSourceLanguage: sourceLang,
					Text:                   resp.Data,
				}
				return nil
			})
		}

		if err := g.Wait(); err != nil {
			return nil, err
		}

		return translateResp, nil
	}

	cmd := config.C.DeepLX.Url
	reqBody := deepLTranslateReq{
		Text:       text,
		TargetLang: targetLang,
		SourceLang: sourceLang,
	}

	reqBodyByte, err := sonic.Marshal(reqBody)
	if err != nil {
		logger.S.Infof("Error marshaling request body: %s", err)
		return nil, err
	}

	req, err := http.NewRequest("POST", cmd, bytes.NewBuffer(reqBodyByte))
	if err != nil {
		logger.S.Infof("Error creating request: %s", err)
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "DeepL-Auth-Key helloworld")

	resp, err := d.client.Do(req)
	if err != nil {
		logger.S.Infof("Error sending request: %s", err)
		return nil, err
	}

	if resp.StatusCode != 200 {
		logger.S.Infof("Error sending request: %s", resp.Status)
		return nil, errors.New(resp.Status)
	}

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.S.Infof("Error reading response body: %s", err)
		return nil, err
	}

	var translateResp deepLTranslateResp
	err = sonic.Unmarshal(body, &translateResp)
	if err != nil {
		logger.S.Infof("Error unmarshaling response body: %s", err)
		return nil, err
	}

	return &translateResp, nil
}
