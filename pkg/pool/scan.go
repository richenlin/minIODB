package pool

import (
	"context"

	"github.com/go-redis/redis/v8"
)

// ScanKeys 使用 SCAN 命令迭代获取所有匹配的键
// 这是 KEYS 命令的安全替代，不会阻塞 Redis 实例
func ScanKeys(ctx context.Context, client redis.Cmdable, pattern string) ([]string, error) {
	var allKeys []string
	var cursor uint64

	for {
		keys, nextCursor, err := client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return nil, err
		}
		allKeys = append(allKeys, keys...)
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	return allKeys, nil
}
