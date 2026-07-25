package redisadapter

import (
	"context"

	redistoolset "github.com/futuretea/redis-mcp-server/pkg/toolset/redis"
)

func (a *Adapter) executeWrite(ctx context.Context, call redistoolset.Call) (map[string]any, error) {
	return a.executeKeyAndStringWrite(ctx, call)
}
