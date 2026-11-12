package handler

import (
	"context"

	"github.com/fusionn/internal/service/subtitle"
)

// MediaProcessor is the subtitle work the webhook and callback handlers
// trigger. The handler package owns this interface so it depends on the two
// calls it makes; *subtitle.Service satisfies it.
type MediaProcessor interface {
	ProcessMedia(ctx context.Context, params subtitle.MediaParams) error
	ProcessWithSubtitles(ctx context.Context, videoPath, engSubPath, chsSubPath, parentJobID string) error
}
