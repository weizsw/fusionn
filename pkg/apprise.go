package pkg

import (
	"fusionn/logger"

	"github.com/valyala/fasthttp"
)

type Apprise interface {
	SendBasicMessage(url string, data []byte) ([]byte, error)
}

type apprise struct {
}

func NewApprise() *apprise {
	return &apprise{}
}

func (a *apprise) SendBasicMessage(url string, data []byte) ([]byte, error) {
	req := fasthttp.AcquireRequest()
	defer fasthttp.ReleaseRequest(req)

	req.SetRequestURI(url)
	req.Header.SetMethod("POST")
	req.Header.SetContentType("application/json")
	req.SetBody(data)

	resp := fasthttp.AcquireResponse()
	defer fasthttp.ReleaseResponse(resp)

	err := fasthttp.Do(req, resp)
	if err != nil {
		return nil, err
	}
	logger.Sugar.Info("send apprise message success")

	return resp.Body(), nil
}
