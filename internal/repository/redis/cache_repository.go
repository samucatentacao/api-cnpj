package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"dataws_api/internal/model"
)

type cacheRepository struct {
	client *redis.Client
}

// NewCacheRepository cria uma implementação do CacheRepository usando Redis.
func NewCacheRepository(client *redis.Client) model.CacheRepository {
	return &cacheRepository{client: client}
}

func (r *cacheRepository) Get(ctx context.Context, key string) (string, error) {
	val, err := r.client.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return "", model.ErrCachemiss
		}
		return "", fmt.Errorf("redis.Get: %w", err)
	}
	return val, nil
}

func (r *cacheRepository) Set(ctx context.Context, key string, value string, ttlSeconds int) error {
	ttl := time.Duration(ttlSeconds) * time.Second
	if err := r.client.Set(ctx, key, value, ttl).Err(); err != nil {
		return fmt.Errorf("redis.Set: %w", err)
	}
	return nil
}

func (r *cacheRepository) Delete(ctx context.Context, key string) error {
	if err := r.client.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("redis.Delete: %w", err)
	}
	return nil
}
