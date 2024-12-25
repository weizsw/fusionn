package mq

import (
	"context"
	"fusionn/internal/cache"

	"github.com/bytedance/sonic"
)

type MessageQueue interface {
	Publish(ctx context.Context, queueName string, msg Message) error
}

type Message struct {
	FileName string `json:"file_name"`
	Path     string `json:"path"`
	Overview string `json:"overview"`
}

type messageQueue struct {
	client cache.RedisClient
}

func NewMessageQueue(client cache.RedisClient) *messageQueue {
	return &messageQueue{client: client}
}

func (mq *messageQueue) Publish(ctx context.Context, queueName string, msg Message) error {
	payload, err := sonic.Marshal(msg)
	if err != nil {
		return err
	}
	return mq.client.LPush(ctx, queueName, payload)
}
