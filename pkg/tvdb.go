package pkg

import (
	"bytes"
	"context"
	"fmt"
	"fusionn/config"
	"fusionn/internal/cache"
	"fusionn/internal/model"
	"fusionn/logger"
	"io"
	"net/http"
	"time"

	"github.com/avast/retry-go"
	"github.com/bytedance/sonic"
)

var (
	loginUrl  = "https://api4.thetvdb.com/v4/login"
	seriesUrl = "https://api4.thetvdb.com/v4/series/%d/episodes/default"
)

type TVDB interface {
	GetSeriesEpisodes(ctx context.Context, id int) (*model.TVDBSeries, error)
	Login() (string, error)
}

type tvdb struct {
	client *http.Client
	cache  cache.RedisClient
}

func NewTVDB(cache cache.RedisClient) *tvdb {
	client := &http.Client{
		Transport: NewLoggingRoundTripper(logger.L),
	}
	return &tvdb{client: client, cache: cache}
}

func (t *tvdb) Login() (string, error) {
	reqBody := model.TVDBLoginRequest{
		ApiKey: config.C.TVDB.ApiKey,
	}

	reqBodyByte, err := sonic.Marshal(reqBody)
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", loginUrl, bytes.NewBuffer(reqBodyByte))
	if err != nil {
		return "", err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	var loginResp model.TVDBLoginResponse

	if err := retry.Do(func() error {
		resp, err := t.client.Do(req)
		if err != nil {
			return err
		}

		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("failed to login to TVDB: %s", resp.Status)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		if err := sonic.Unmarshal(body, &loginResp); err != nil {
			return err
		}

		if loginResp.Status != "success" {
			return fmt.Errorf("failed to login to TVDB: %s", loginResp.Status)
		}

		return nil
	}, retryOptions...); err != nil {
		return "", err
	}

	return loginResp.Data.Token, nil
}

func (t *tvdb) GetSeriesEpisodes(ctx context.Context, id int) (*model.TVDBSeries, error) {
	token, err := t.cache.Get(ctx, cache.TVDBTokenKey)
	if err != nil {
		logger.S.Infof("Error getting TVDB token: %s", err)
	}

	if token == "" {
		token, err = t.Login()
		if err != nil {
			return nil, err
		}
		go func() {
			t.cache.Set(ctx, cache.TVDBTokenKey, token, 20*24*time.Hour)
		}()
	}

	url := fmt.Sprintf(seriesUrl, id)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", token))
	req.Header.Set("Accept", "application/json")
	var series model.TVDBSeries

	if err := retry.Do(func() error {
		resp, err := t.client.Do(req)
		if err != nil {
			return err
		}

		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("failed to get series episodes: %s", resp.Status)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}

		if err := sonic.Unmarshal(body, &series); err != nil {
			return err
		}

		return nil
	}, retryOptions...); err != nil {
		return nil, err
	}

	return &series, nil
}
