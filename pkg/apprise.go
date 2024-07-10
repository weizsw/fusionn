package pkg

import (
	"github.com/gofiber/fiber/v2/log"
	"github.com/valyala/fasthttp"
)

type IApprise interface {
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
	log.Info("send apprise message success")

	return resp.Body(), nil
}
