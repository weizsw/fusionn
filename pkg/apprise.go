package pkg

import (
	"bytes"
	"io"
	"net/http"
)

type Apprise interface {
	SendBasicMessage(url string, data []byte) ([]byte, error)
}

type apprise struct {
	client *http.Client
}

func NewApprise() *apprise {
	return &apprise{
		client: &http.Client{},
	}
}

func (a *apprise) SendBasicMessage(url string, data []byte) ([]byte, error) {
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}
