package deepl

import (
	"fusionn/internal/consts"
	"log"

	"github.com/bytedance/sonic"
	"github.com/valyala/fasthttp"
)

type DeepL struct {
}

func NewDeepL() *DeepL {
	return &DeepL{}
}

type deepLTranslateRequest struct {
	Text       []string `json:"text"`
	TargetLang string   `json:"target_lang"`
	SourceLang string   `json:"source_lang"`
}

func (d *DeepL) Translate(text, targetLang, souceLang string) (string, error) {
	cmd := consts.CMDDeepLTranslate
	req := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(req)

	req.Header.SetMethod("POST")
	req.SetRequestURI(cmd)
	req.Header.Set("Content-Type", "application/json")

	reqBody := deepLTranslateRequest{
		Text:       []string{text},
		TargetLang: targetLang,
		SourceLang: souceLang,
	}
	reqBodyByte, err := sonic.Marshal(reqBody)
	if err != nil {
		log.Fatalf("Error marshaling request body: %s", err)
		return "", err
	}
	req.SetBody(reqBodyByte)

	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(resp)

	if err := fasthttp.Do(req, resp); err != nil {
		log.Fatalf("Error sending request: %s", err)
	}

	return string(resp.Body()), nil
}
