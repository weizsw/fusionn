package cache

import (
	"context"
	"fmt"
	"time"

	"fusionn/config"

	"github.com/redis/go-redis/v9"
)

// RedisClient wraps redis operations
type RedisClient interface {
	Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error
	Get(ctx context.Context, key string) (string, error)
	Delete(ctx context.Context, key string) error
	LPush(ctx context.Context, key string, values ...interface{}) error
}

// redisClient implements RedisClient interface
type redisClient struct {
	client *redis.Client
}

// NewRedisClient creates a new Redis client instance
func NewRedisClient() (*redisClient, error) {
	a := config.C.Redis.Addr
	fmt.Println(a)
	client := redis.NewClient(&redis.Options{
		Addr: config.C.Redis.Addr,
		DB:   config.C.Redis.DB,
	})

	// Verify connection
	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, err
	}

	return &redisClient{
		client: client,
	}, nil
}

func (r *redisClient) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	return r.client.Set(ctx, key, value, expiration).Err()
}

func (r *redisClient) Get(ctx context.Context, key string) (string, error) {
	return r.client.Get(ctx, key).Result()
}

func (r *redisClient) Delete(ctx context.Context, key string) error {
	return r.client.Del(ctx, key).Err()
}

func (r *redisClient) LPush(ctx context.Context, key string, values ...interface{}) error {
	return r.client.LPush(ctx, key, values...).Err()
}
