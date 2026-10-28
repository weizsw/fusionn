package subtitle

import (
	"context"

	"go.uber.org/zap"

	"github.com/fusionn/internal/client/bazarr"
	"github.com/fusionn/internal/config"
	"github.com/fusionn/internal/queue"
	"github.com/fusionn/pkg/logger"
)

// NewReloadableService reads updated configuration when each pipeline starts.
func NewReloadableService(manager *config.Manager, redisClient QueueClient, mergeQueue *queue.MergeQueue) (*Service, error) {
	service, err := newConfiguredService(manager.Get(), redisClient, mergeQueue)
	if err != nil {
		return nil, err
	}
	service.configSource = manager.Get
	return service, nil
}

// currentPipelines takes one snapshot so a running pipeline never mixes configurations.
func (s *Service) currentPipelines() *servicePipelines {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.configSource != nil {
		cfg := s.configSource()
		if cfg != s.lastConfig {
			// Retry failed construction only after the configuration changes again.
			s.lastConfig = cfg
			next, err := newConfiguredService(cfg, s.redisClient, s.mergeQueue)
			if err != nil {
				logger.Component("service").Error(context.Background(), "config_apply_failed",
					"Failed to apply subtitle configuration, keeping previous pipelines", zap.Error(err))
			} else {
				s.pipelines = next.pipelines
				logger.Component("service").Info(context.Background(), "config_reloaded", "Subtitle configuration applied to new jobs")
			}
		}
	}
	return s.pipelines
}

func newConfiguredService(cfg *config.Config, redisClient QueueClient, mergeQueue *queue.MergeQueue) (*Service, error) {
	var bazarrClient *bazarr.Client
	if cfg.Bazarr.Enabled {
		bazarrClient = bazarr.NewClient(cfg.Bazarr.URL, cfg.Bazarr.APIKey, cfg.Bazarr.SearchTimeout)
		logger.Component("service").Info(context.Background(), "bazarr_enabled", "Bazarr integration enabled")
	}
	return NewService(cfg, redisClient, mergeQueue, bazarrClient)
}
