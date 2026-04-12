package bootstrap

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// NewRedis creates and verifies a Redis client.
func NewRedis(cfg RedisConfig) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})

	if err := client.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("ping redis: %w", err)
	}
	return client, nil
}
