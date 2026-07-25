package redisadapter

import (
	"context"

	"github.com/redis/go-redis/v9"
)

func (a *Adapter) streamPending(ctx context.Context, args map[string]any) (map[string]any, error) {
	value, err := a.client.XPendingExt(ctx, &redis.XPendingExtArgs{
		Stream: stringArg(args, "key"), Group: stringArg(args, "group"), Start: stringArg(args, "start"), End: stringArg(args, "end"),
		Count: int64Arg(args, "count"), Consumer: stringArg(args, "consumer"),
	}).Result()
	return map[string]any{"items": value}, err
}
