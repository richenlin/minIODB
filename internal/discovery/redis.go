package discovery

import (
	"context"
	"minIODB/internal/config"

	"github.com/go-redis/redis/v8"
)

// NewRedisClient creates a new Redis client from the given configuration
func NewRedisClient(ctx context.Context, cfg config.RedisConfig) *redis.Client {
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Addr,
		Password: cfg.Password,
		DB:       cfg.DB,
	})
	return rdb
}

// PingRedis checks the connection to Redis
func PingRedis(ctx context.Context, client *redis.Client) error {
	_, err := client.Ping(ctx).Result()
	return err
}
