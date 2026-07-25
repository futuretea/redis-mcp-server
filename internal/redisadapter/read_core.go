package redisadapter

import (
	"context"
	cryptorand "crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	redistoolset "github.com/futuretea/redis-mcp-server/pkg/toolset/redis"
)

func (a *Adapter) executeRead(ctx context.Context, call redistoolset.Call) (map[string]any, error) {
	args := call.Args
	key := stringArg(args, "key")
	switch call.Tool + "/" + call.Operation {
	case "redis_key/exists":
		count, err := a.client.Exists(ctx, stringSlice(args["keys"])...).Result()
		return map[string]any{"count": count}, err
	case "redis_key/random_key":
		return a.randomKey(ctx)
	case "redis_key/type":
		kind, err := a.client.Type(ctx, key).Result()
		return map[string]any{"metadata": map[string]any{"key_type": kind}}, err
	case "redis_key/expiry":
		return a.keyExpiry(ctx, args)
	case "redis_key/dump":
		value, err := a.client.Dump(ctx, key).Result()
		return map[string]any{"value": binaryOutput(value)}, err
	case "redis_key/object_attribute":
		return a.objectAttribute(ctx, args)
	case "redis_string/get":
		value, err := a.client.Get(ctx, key).Result()
		return map[string]any{"value": binaryOutput(value)}, err
	case "redis_string/get_range":
		value, err := a.client.GetRange(ctx, key, int64Arg(args, "start"), int64Arg(args, "stop")).Result()
		return map[string]any{"value": binaryOutput(value)}, err
	case "redis_string/get_many":
		keys := stringSlice(args["keys"])
		values, err := a.client.MGet(ctx, keys...).Result()
		result := make(map[string]any, len(keys))
		for index, value := range values {
			if value != nil {
				result[keys[index]] = binaryOutput(fmt.Sprint(value))
			} else {
				result[keys[index]] = nil
			}
		}
		return map[string]any{"values_by_key": result}, err
	case "redis_string/length":
		count, err := a.client.StrLen(ctx, key).Result()
		return map[string]any{"count": count}, err
	case "redis_string/digest":
		value, err := a.client.Digest(ctx, key).Result()
		return map[string]any{"metadata": map[string]any{"xxh3": fmt.Sprintf("%016x", value)}}, err
	case "redis_string/longest_common_substring":
		return a.longestCommonSubstring(ctx, args)
	case "redis_hash/field_exists":
		value, err := a.client.HExists(ctx, key, binaryString(args["field"])).Result()
		return map[string]any{"scalar": value}, err
	case "redis_hash/get":
		value, err := a.client.HGet(ctx, key, binaryString(args["field"])).Result()
		return map[string]any{"value": binaryOutput(value)}, err
	case "redis_hash/length":
		count, err := a.client.HLen(ctx, key).Result()
		return map[string]any{"count": count}, err
	case "redis_hash/get_many":
		fields := binarySlice(args["fields"])
		values, err := a.client.HMGet(ctx, key, fields...).Result()
		return map[string]any{"fields": binaryOptionalValues(fields, values)}, err
	case "redis_hash/field_expiry":
		return a.hashExpiry(ctx, args)
	case "redis_hash/random_field":
		return a.randomHashField(ctx, args)
	case "redis_hash/scan":
		return a.hashScan(ctx, args)
	case "redis_hash/value_length":
		count, err := a.client.HStrLen(ctx, key, binaryString(args["field"])).Result()
		return map[string]any{"count": count}, err
	case "redis_observe/scan":
		return a.observeScan(ctx, args)
	case "redis_observe/db_size":
		return a.databaseSize(ctx)
	case "redis_observe/memory_usage":
		count, err := a.client.MemoryUsage(ctx, key, intArg(args, "samples")).Result()
		return map[string]any{"metadata": map[string]any{"bytes": count}}, err
	}
	return a.executeCollectionRead(ctx, call)
}

func (a *Adapter) randomKey(ctx context.Context) (map[string]any, error) {
	cluster, ok := a.client.(*redis.ClusterClient)
	if !ok {
		value, err := a.client.RandomKey(ctx).Result()
		return map[string]any{"value": binaryOutput(value), "metadata": map[string]any{"selection": "single_server"}}, err
	}
	var (
		candidates []string
		mu         sync.Mutex
	)
	err := cluster.ForEachMaster(ctx, func(ctx context.Context, client *redis.Client) error {
		value, err := client.RandomKey(ctx).Result()
		if errors.Is(err, redis.Nil) {
			return nil
		}
		if err != nil {
			return err
		}
		if value != "" {
			mu.Lock()
			candidates = append(candidates, value)
			mu.Unlock()
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return map[string]any{"value": nil, "metadata": map[string]any{"selection": "uniform_nonempty_primary"}}, nil
	}
	choice, err := cryptorand.Int(cryptorand.Reader, big.NewInt(int64(len(candidates))))
	if err != nil {
		return nil, err
	}
	return map[string]any{"value": binaryOutput(candidates[choice.Int64()]), "metadata": map[string]any{"selection": "uniform_nonempty_primary"}}, nil
}

func (a *Adapter) keyExpiry(ctx context.Context, args map[string]any) (map[string]any, error) {
	key := stringArg(args, "key")
	unit := stringArg(args, "unit")
	representation := stringArg(args, "representation")
	if representation == "absolute" {
		if unit == "seconds" {
			value, err := a.client.ExpireTime(ctx, key).Result()
			return map[string]any{"scalar": value / time.Second}, err
		}
		value, err := a.client.PExpireTime(ctx, key).Result()
		return map[string]any{"scalar": value / time.Millisecond}, err
	}
	if unit == "seconds" {
		value, err := a.client.TTL(ctx, key).Result()
		return map[string]any{"scalar": value / time.Second}, err
	}
	value, err := a.client.PTTL(ctx, key).Result()
	return map[string]any{"scalar": value / time.Millisecond}, err
}

func (a *Adapter) objectAttribute(ctx context.Context, args map[string]any) (map[string]any, error) {
	key := stringArg(args, "key")
	switch stringArg(args, "attribute") {
	case "encoding":
		value, err := a.client.ObjectEncoding(ctx, key).Result()
		return map[string]any{"scalar": value}, err
	case "freq":
		value, err := a.client.ObjectFreq(ctx, key).Result()
		return map[string]any{"scalar": value}, err
	case "idletime":
		value, err := a.client.ObjectIdleTime(ctx, key).Result()
		return map[string]any{"scalar": value / time.Second}, err
	default:
		value, err := a.client.ObjectRefCount(ctx, key).Result()
		return map[string]any{"scalar": value}, err
	}
}

func (a *Adapter) longestCommonSubstring(ctx context.Context, args map[string]any) (map[string]any, error) {
	query := &redis.LCSQuery{Key1: stringArg(args, "first_key"), Key2: stringArg(args, "second_key")}
	switch stringArg(args, "mode") {
	case "length":
		query.Len = true
	}
	result, err := a.client.LCS(ctx, query).Result()
	if err != nil {
		return nil, err
	}
	if query.Len {
		return map[string]any{"scalar": result.Len}, nil
	}
	return map[string]any{"value": binaryOutput(result.MatchString)}, nil
}

func (a *Adapter) hashExpiry(ctx context.Context, args map[string]any) (map[string]any, error) {
	fields := binarySlice(args["fields"])
	key := stringArg(args, "key")
	unit := stringArg(args, "unit")
	representation := stringArg(args, "representation")
	if representation == "absolute" {
		if unit == "seconds" {
			values, err := a.client.HExpireTime(ctx, key, fields...).Result()
			return map[string]any{"items": values}, err
		}
		values, err := a.client.HPExpireTime(ctx, key, fields...).Result()
		return map[string]any{"items": values}, err
	}
	if unit == "seconds" {
		values, err := a.client.HTTL(ctx, key, fields...).Result()
		return map[string]any{"items": values}, err
	}
	values, err := a.client.HPTTL(ctx, key, fields...).Result()
	return map[string]any{"items": values}, err
}

func (a *Adapter) randomHashField(ctx context.Context, args map[string]any) (map[string]any, error) {
	count, present := args["count"]
	if !present {
		count = float64(1)
	}
	if withValues, _ := args["with_values"].(bool); withValues {
		values, err := a.client.HRandFieldWithValues(ctx, stringArg(args, "key"), intArg(map[string]any{"count": count}, "count")).Result()
		items := make([]map[string]any, 0, len(values))
		for _, value := range values {
			items = append(items, map[string]any{"field": binaryOutput(value.Key), "value": binaryOutput(value.Value)})
		}
		return map[string]any{"items": items}, err
	}
	values, err := a.client.HRandField(ctx, stringArg(args, "key"), intArg(map[string]any{"count": count}, "count")).Result()
	return map[string]any{"fields": binaryOutputs(values)}, err
}

func (a *Adapter) hashScan(ctx context.Context, args map[string]any) (map[string]any, error) {
	key := stringArg(args, "key")
	match := stringArg(args, "match")
	count := int64Arg(args, "count")
	noValues, _ := args["no_values"].(bool)
	state := continuationState{kind: scanKindHash, key: key, match: match, noValues: noValues}
	width := 2
	if noValues {
		width = 1
	}
	values, next, _, err := a.continueScan(ctx, args, state, width, func(state continuationState) ([]string, continuationState, error) {
		var page []string
		var cursor uint64
		var pageErr error
		if state.noValues {
			page, cursor, pageErr = a.client.HScanNoValues(ctx, state.key, state.cursor, state.match, count).Result()
		} else {
			page, cursor, pageErr = a.client.HScan(ctx, state.key, state.cursor, state.match, count).Result()
		}
		state.cursor, state.started = cursor, true
		return page, state, pageErr
	})
	if err != nil {
		return nil, err
	}
	metadata := map[string]any{"next_cursor": next}
	if noValues {
		return map[string]any{"fields": binaryOutputs(values), "metadata": metadata}, nil
	}
	return map[string]any{"fields": binaryPairs(values), "metadata": metadata}, nil
}

func (a *Adapter) observeScan(ctx context.Context, args map[string]any) (map[string]any, error) {
	state, err := a.observeInitialState(ctx, args)
	if err != nil {
		return nil, err
	}
	values, next, complete, err := a.continueScan(ctx, args, state, 1, a.observeScanPage(ctx, int64Arg(args, "count")))
	if err != nil {
		return nil, err
	}
	return map[string]any{"items": map[string]any{"keys": values}, "metadata": map[string]any{"next_cursor": next, "complete": complete}}, nil
}

func (a *Adapter) databaseSize(ctx context.Context) (map[string]any, error) {
	if cluster, ok := a.client.(*redis.ClusterClient); ok {
		var total int64
		count := 0
		err := cluster.ForEachMaster(ctx, func(ctx context.Context, client *redis.Client) error {
			value, err := client.DBSize(ctx).Result()
			if err == nil {
				total += value
				count++
			}
			return err
		})
		return map[string]any{"count": total, "metadata": map[string]any{"aggregate": "sum_current_primaries", "primary_count": count}}, err
	}
	count, err := a.client.DBSize(ctx).Result()
	return map[string]any{"count": count, "metadata": map[string]any{"aggregate": "single_server"}}, err
}

func binaryOptionalValues(fields []string, values []any) []map[string]any {
	result := make([]map[string]any, 0, len(fields))
	for index, field := range fields {
		item := map[string]any{"field": binaryOutput(field), "value": nil}
		if index < len(values) && values[index] != nil {
			item["value"] = binaryOutput(fmt.Sprint(values[index]))
		}
		result = append(result, item)
	}
	return result
}

func binaryPairs(values []string) []map[string]any {
	result := make([]map[string]any, 0, len(values)/2)
	for index := 0; index+1 < len(values); index += 2 {
		result = append(result, map[string]any{"field": binaryOutput(values[index]), "value": binaryOutput(values[index+1])})
	}
	return result
}
