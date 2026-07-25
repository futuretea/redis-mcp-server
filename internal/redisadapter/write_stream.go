package redisadapter

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"

	redistoolset "github.com/futuretea/redis-mcp-server/pkg/toolset/redis"
)

func (a *Adapter) executeStreamWrite(ctx context.Context, call redistoolset.Call) (map[string]any, error) {
	args := call.Args
	key := stringArg(args, "key")
	switch call.Tool + "/" + call.Operation {
	case "redis_stream_write/group":
		return a.streamGroup(ctx, args)
	case "redis_stream_write/ack":
		return a.ackStream(ctx, args)
	case "redis_stream_write/add":
		values, err := fieldValues(args["fields"])
		if err != nil {
			return nil, err
		}
		value, err := a.client.XAdd(ctx, &redis.XAddArgs{Stream: key, ID: stringArg(args, "id"), NoMkStream: boolArg(args, "nomkstream"), MaxLen: int64Arg(args, "maxlen"), MinID: stringArg(args, "minid"), Approx: boolArg(args, "approximate"), Limit: int64Arg(args, "limit"), Values: values}).Result()
		return map[string]any{"metadata": map[string]any{"id": value}}, err
	case "redis_stream_write/claim":
		return a.claim(ctx, args)
	case "redis_stream_write/delete":
		return a.deleteStream(ctx, args)
	case "redis_stream_write/nack":
		options := &redis.XNackArgs{Stream: key, Group: stringArg(args, "group"), Mode: stringArg(args, "mode"), IDs: stringSlice(args["ids"]), Force: boolArg(args, "force")}
		if _, present := args["retry_count"]; present {
			count := uint64(int64Arg(args, "retry_count"))
			options.RetryCount = &count
		}
		count, err := a.client.XNack(ctx, options).Result()
		return mutation("nacked_count", count), err
	case "redis_stream_write/trim":
		return a.trimStream(ctx, args)
	default:
		return nil, callError("topology_unsupported", "Redis operation mapping is unavailable")
	}
}

func (a *Adapter) ackStream(ctx context.Context, args map[string]any) (map[string]any, error) {
	ids := stringSlice(args["ids"])
	if _, present := args["delete_references"]; present {
		values, err := a.client.XAckDel(ctx, stringArg(args, "key"), stringArg(args, "group"), stringArg(args, "delete_references"), ids...).Result()
		return map[string]any{"items": streamReferenceStatuses(ids, values)}, err
	}
	count, err := a.client.XAck(ctx, stringArg(args, "key"), stringArg(args, "group"), ids...).Result()
	return mutation("acknowledged_count", count), err
}

func (a *Adapter) deleteStream(ctx context.Context, args map[string]any) (map[string]any, error) {
	ids := stringSlice(args["ids"])
	if _, present := args["delete_references"]; present {
		values, err := a.client.XDelEx(ctx, stringArg(args, "key"), stringArg(args, "delete_references"), ids...).Result()
		return map[string]any{"items": streamReferenceStatuses(ids, values)}, err
	}
	count, err := a.client.XDel(ctx, stringArg(args, "key"), ids...).Result()
	return mutation("deleted_count", count), err
}

func streamReferenceStatuses(ids []string, values []any) []map[string]any {
	items := make([]map[string]any, 0, len(ids))
	for index, id := range ids {
		status := any(nil)
		if index < len(values) {
			status = values[index]
		}
		items = append(items, map[string]any{"id": id, "status": status})
	}
	return items
}

func (a *Adapter) streamGroup(ctx context.Context, args map[string]any) (map[string]any, error) {
	key, group, action := stringArg(args, "key"), stringArg(args, "group"), stringArg(args, "action")
	var err error
	var count int64
	switch action {
	case "create":
		if boolArg(args, "mkstream") {
			err = a.client.XGroupCreateMkStream(ctx, key, group, stringArg(args, "id")).Err()
		} else {
			err = a.client.XGroupCreate(ctx, key, group, stringArg(args, "id")).Err()
		}
	case "create_consumer":
		count, err = a.client.XGroupCreateConsumer(ctx, key, group, stringArg(args, "consumer")).Result()
	case "delete_consumer":
		count, err = a.client.XGroupDelConsumer(ctx, key, group, stringArg(args, "consumer")).Result()
	case "destroy":
		count, err = a.client.XGroupDestroy(ctx, key, group).Result()
	case "set_id":
		err = a.client.XGroupSetID(ctx, key, group, stringArg(args, "id")).Err()
	}
	return map[string]any{"mutation": map[string]any{"applied": err == nil, "count": count}}, err
}

func (a *Adapter) claim(ctx context.Context, args map[string]any) (map[string]any, error) {
	options := &redis.XClaimArgs{Stream: stringArg(args, "key"), Group: stringArg(args, "group"), Consumer: stringArg(args, "consumer"), MinIdle: time.Duration(int64Arg(args, "min_idle_ms")) * time.Millisecond, Messages: stringSlice(args["ids"])}
	values, err := a.client.XClaimJustID(ctx, options).Result()
	return map[string]any{"items": values}, err
}

func (a *Adapter) trimStream(ctx context.Context, args map[string]any) (map[string]any, error) {
	key, strategy := stringArg(args, "key"), stringArg(args, "strategy")
	approx, limit := boolArg(args, "approximate"), int64Arg(args, "limit")
	var count int64
	var err error
	if strategy == "maxlen" {
		count, err = a.client.XTrimMaxLenApproxMode(ctx, key, int64Value(args["threshold"], 0), limit, trimMode(approx)).Result()
	} else {
		count, err = a.client.XTrimMinIDApproxMode(ctx, key, stringArg(args, "threshold"), limit, trimMode(approx)).Result()
	}
	return mutation("deleted_count", count), err
}

func trimMode(approx bool) string {
	if approx {
		return "~"
	}
	return "="
}
