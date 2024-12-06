package processor

import (
	"context"
	"fmt"
	"fusionn/config"
	"fusionn/errs"
	"fusionn/internal/model"
	"fusionn/logger"
	"fusionn/pkg"
)

type NotiStage struct {
	apprise pkg.Apprise
}

func NewNotiStage(apprise pkg.Apprise) *NotiStage {
	return &NotiStage{
		apprise: apprise,
	}
}

var msgFormat = `{"title":"Fusionn notification","body":"%s"}`

func (s *NotiStage) Process(ctx context.Context, input any) (any, error) {
	req, ok := input.(*model.ParsedSubtitles)
	if !ok {
		return nil, errs.ErrInvalidInput
	}

	logger.L.Info("[NotiStage] sending notification")

	mode := "generated"
	if req.Translated {
		mode = "translated"
	}

	if config.C.Apprise.Enabled {
		s.apprise.SendBasicMessage(config.C.Apprise.Url, []byte(fmt.Sprintf(msgFormat, fmt.Sprintf("Subtitle for %s %s successfully", req.FileName, mode))))
	}

	return req, nil
}
