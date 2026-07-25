package redisadapter

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	redistoolset "github.com/futuretea/redis-mcp-server/pkg/toolset/redis"
)

func (a *Adapter) executeOtherWrite(ctx context.Context, call redistoolset.Call) (map[string]any, error) {
	switch call.Tool {
	case "redis_hash_write":
		return a.executeHashWrite(ctx, call)
	case "redis_list_write":
		return a.executeListWrite(ctx, call)
	case "redis_set_write":
		return a.executeSetWrite(ctx, call)
	case "redis_sorted_set_write":
		return a.executeSortedSetWrite(ctx, call)
	default:
		return a.executeStreamWrite(ctx, call)
	}
}

func (a *Adapter) executeHashWrite(ctx context.Context, call redistoolset.Call) (map[string]any, error) {
	args := call.Args
	key := stringArg(args, "key")
	switch call.Operation {
	case "delete_fields":
		count, err := a.client.HDel(ctx, key, binarySlice(args["fields"])...).Result()
		return mutation("deleted_count", count), err
	case "set_field_expiry":
		return a.setHashExpiry(ctx, args)
	case "persist_field_expiry":
		values, err := a.client.HPersist(ctx, key, binarySlice(args["fields"])...).Result()
		return map[string]any{"items": values}, err
	case "get_and_delete":
		values, err := a.client.HGetDel(ctx, key, binarySlice(args["fields"])...).Result()
		return map[string]any{"fields": binaryOutputs(values)}, err
	case "get_and_set_expiry":
		return a.getAndSetHashExpiry(ctx, args)
	case "increment":
		if stringArg(args, "number_kind") == "float" {
			value, err := a.client.HIncrByFloat(ctx, key, binaryString(args["field"]), floatArg(args["amount"], 0)).Result()
			return map[string]any{"scalar": value}, err
		}
		value, err := a.client.HIncrBy(ctx, key, binaryString(args["field"]), int64Value(args["amount"], 0)).Result()
		return map[string]any{"scalar": value}, err
	case "set_fields":
		values, err := fieldValues(args["fields"])
		if err != nil {
			return nil, err
		}
		count, err := a.client.HSet(ctx, key, values...).Result()
		return mutation("added_count", count), err
	case "set_fields_with_expiry":
		return a.setHashFieldsWithExpiry(ctx, args)
	case "set_if_absent":
		applied, err := a.client.HSetNX(ctx, key, binaryString(args["field"]), binaryString(args["value"])).Result()
		return map[string]any{"mutation": map[string]any{"applied": applied}}, err
	default:
		return nil, callError("topology_unsupported", "Redis operation mapping is unavailable")
	}
}

func (a *Adapter) executeListWrite(ctx context.Context, call redistoolset.Call) (map[string]any, error) {
	args := call.Args
	key := stringArg(args, "key")
	switch call.Operation {
	case "insert":
		length, err := a.client.LInsert(ctx, key, stringArg(args, "where"), binaryString(args["pivot"]), binaryString(args["element"])).Result()
		return mutation("length", length), err
	case "move":
		value, err := a.client.LMove(ctx, stringArg(args, "source_key"), stringArg(args, "destination_key"), stringArg(args, "from"), stringArg(args, "to")).Result()
		return map[string]any{"value": binaryOutput(value)}, err
	case "pop":
		return a.popList(ctx, args)
	case "pop_many":
		key, values, err := a.client.LMPop(ctx, stringArg(args, "side"), popCount(args), stringSlice(args["keys"])...).Result()
		return map[string]any{"metadata": map[string]any{"key": key}, "items": binaryOutputs(values)}, err
	case "push":
		return a.pushList(ctx, args, false)
	case "push_if_exists":
		return a.pushList(ctx, args, true)
	case "remove":
		count, err := a.client.LRem(ctx, key, int64Arg(args, "count"), binaryString(args["element"])).Result()
		return mutation("removed_count", count), err
	case "set":
		err := a.client.LSet(ctx, key, int64Arg(args, "index"), binaryString(args["element"])).Err()
		return applied(err), err
	case "trim":
		err := a.client.LTrim(ctx, key, int64Arg(args, "start"), int64Arg(args, "stop")).Err()
		return applied(err), err
	default:
		return nil, callError("topology_unsupported", "Redis operation mapping is unavailable")
	}
}

func (a *Adapter) executeSetWrite(ctx context.Context, call redistoolset.Call) (map[string]any, error) {
	args := call.Args
	key := stringArg(args, "key")
	switch call.Operation {
	case "add":
		count, err := a.client.SAdd(ctx, key, interfaces(binarySlice(args["members"]))...).Result()
		return mutation("added_count", count), err
	case "store_difference":
		count, err := a.client.SDiffStore(ctx, stringArg(args, "destination_key"), stringSlice(args["keys"])...).Result()
		return mutation("length", count), err
	case "store_intersection":
		count, err := a.client.SInterStore(ctx, stringArg(args, "destination_key"), stringSlice(args["keys"])...).Result()
		return mutation("length", count), err
	case "move":
		appliedValue, err := a.client.SMove(ctx, stringArg(args, "source_key"), stringArg(args, "destination_key"), binaryString(args["member"])).Result()
		return map[string]any{"mutation": map[string]any{"applied": appliedValue}}, err
	case "pop":
		return a.popSet(ctx, args)
	case "remove":
		count, err := a.client.SRem(ctx, key, interfaces(binarySlice(args["members"]))...).Result()
		return mutation("removed_count", count), err
	case "store_union":
		count, err := a.client.SUnionStore(ctx, stringArg(args, "destination_key"), stringSlice(args["keys"])...).Result()
		return mutation("length", count), err
	default:
		return nil, callError("topology_unsupported", "Redis operation mapping is unavailable")
	}
}

func (a *Adapter) executeSortedSetWrite(ctx context.Context, call redistoolset.Call) (map[string]any, error) {
	args := call.Args
	key := stringArg(args, "key")
	switch call.Operation {
	case "add":
		return a.addSorted(ctx, args)
	case "store_difference":
		count, err := a.client.ZDiffStore(ctx, stringArg(args, "destination_key"), stringSlice(args["keys"])...).Result()
		return mutation("length", count), err
	case "increment":
		value, err := a.client.ZIncrBy(ctx, key, floatArg(args["increment"], 0), binaryString(args["member"])).Result()
		return map[string]any{"scalar": value}, err
	case "store_intersection":
		store := sortedStore(args)
		count, err := a.client.ZInterStore(ctx, stringArg(args, "destination_key"), &store).Result()
		return mutation("length", count), err
	case "pop":
		key, values, err := a.client.ZMPop(ctx, stringArg(args, "side"), popCount(args), stringSlice(args["keys"])...).Result()
		return map[string]any{"metadata": map[string]any{"key": key}, "members": sortedMembers(values)}, err
	case "remove":
		count, err := a.client.ZRem(ctx, key, interfaces(binarySlice(args["members"]))...).Result()
		return mutation("removed_count", count), err
	case "remove_range":
		return a.removeSortedRange(ctx, args)
	case "store_range":
		rangeArgs := redis.ZRangeArgs{Key: stringArg(args, "source_key"), Start: args["min"], Stop: args["max"], ByScore: stringArg(args, "by") == "score", ByLex: stringArg(args, "by") == "lex", Rev: boolArg(args, "reverse"), Offset: int64Arg(args, "offset"), Count: int64Arg(args, "count")}
		count, err := a.client.ZRangeStore(ctx, stringArg(args, "destination_key"), rangeArgs).Result()
		return mutation("length", count), err
	case "store_union":
		store := sortedStore(args)
		count, err := a.client.ZUnionStore(ctx, stringArg(args, "destination_key"), &store).Result()
		return mutation("length", count), err
	default:
		return nil, callError("topology_unsupported", "Redis operation mapping is unavailable")
	}
}

func mutation(name string, value any) map[string]any {
	return map[string]any{"mutation": map[string]any{name: value}}
}

func sortedStore(args map[string]any) redis.ZStore {
	weights := make([]float64, 0)
	if raw, ok := args["weights"].([]any); ok {
		for _, value := range raw {
			if number, ok := value.(float64); ok {
				weights = append(weights, number)
			}
		}
	}
	return redis.ZStore{Keys: stringSlice(args["keys"]), Weights: weights, Aggregate: stringArg(args, "aggregate")}
}

func applied(err error) map[string]any {
	return map[string]any{"mutation": map[string]any{"applied": err == nil}}
}

func (a *Adapter) setHashExpiry(ctx context.Context, args map[string]any) (map[string]any, error) {
	expiry, err := expiration(args["expiry"])
	if err != nil {
		return nil, err
	}
	fields, key := binarySlice(args["fields"]), stringArg(args, "key")
	conditions := redis.HExpireArgs{NX: stringArg(args, "condition") == "nx", XX: stringArg(args, "condition") == "xx", GT: stringArg(args, "condition") == "gt", LT: stringArg(args, "condition") == "lt"}
	var values []int64
	switch expiry.Mode {
	case redis.EX:
		values, err = a.client.HExpireWithArgs(ctx, key, time.Duration(expiry.Value)*time.Second, conditions, fields...).Result()
	case redis.PX:
		values, err = a.client.HPExpireWithArgs(ctx, key, time.Duration(expiry.Value)*time.Millisecond, conditions, fields...).Result()
	case redis.EXAT:
		values, err = a.client.HExpireAtWithArgs(ctx, key, time.Unix(expiry.Value, 0), conditions, fields...).Result()
	case redis.PXAT:
		values, err = a.client.HPExpireAtWithArgs(ctx, key, time.UnixMilli(expiry.Value), conditions, fields...).Result()
	default:
		return nil, callError("invalid_argument", "expiry kind is not supported for hash fields")
	}
	return map[string]any{"items": values}, err
}

func (a *Adapter) getAndSetHashExpiry(ctx context.Context, args map[string]any) (map[string]any, error) {
	expiry, err := expiration(args["expiry"])
	if err != nil {
		return nil, err
	}
	options := &redis.HGetEXOptions{ExpirationVal: expiry.Value}
	switch expiry.Mode {
	case redis.EX:
		options.ExpirationType = redis.HGetEXExpirationEX
	case redis.PX:
		options.ExpirationType = redis.HGetEXExpirationPX
	case redis.EXAT:
		options.ExpirationType = redis.HGetEXExpirationEXAT
	case redis.PXAT:
		options.ExpirationType = redis.HGetEXExpirationPXAT
	case redis.PERSIST:
		options.ExpirationType = redis.HGetEXExpirationPERSIST
	default:
		return nil, callError("invalid_argument", "unsupported hash GETEX expiry")
	}
	values, err := a.client.HGetEXWithArgs(ctx, stringArg(args, "key"), options, binarySlice(args["fields"])...).Result()
	return map[string]any{"fields": binaryOutputs(values)}, err
}

func (a *Adapter) setHashFieldsWithExpiry(ctx context.Context, args map[string]any) (map[string]any, error) {
	values, err := fieldValues(args["fields"])
	if err != nil {
		return nil, err
	}
	fields := stringsFromInterfaces(values)
	options := &redis.HSetEXOptions{}
	if stringArg(args, "condition") == "fnx" {
		options.Condition = redis.HSetEXFNX
	}
	if stringArg(args, "condition") == "fxx" {
		options.Condition = redis.HSetEXFXX
	}
	if rawExpiry, present := args["expiry"]; present {
		expiry, err := expiration(rawExpiry)
		if err != nil {
			return nil, err
		}
		options.ExpirationVal = expiry.Value
		switch expiry.Mode {
		case redis.EX:
			options.ExpirationType = redis.HSetEXExpirationEX
		case redis.PX:
			options.ExpirationType = redis.HSetEXExpirationPX
		case redis.EXAT:
			options.ExpirationType = redis.HSetEXExpirationEXAT
		case redis.PXAT:
			options.ExpirationType = redis.HSetEXExpirationPXAT
		case redis.KEEPTTL:
			options.ExpirationType = redis.HSetEXExpirationKEEPTTL
		default:
			return nil, callError("invalid_argument", "unsupported hash SETEX expiry")
		}
	}
	result, err := a.client.HSetEXWithArgs(ctx, stringArg(args, "key"), options, fields...).Result()
	return map[string]any{"mutation": map[string]any{"applied": result == 1}}, err
}

func (a *Adapter) popList(ctx context.Context, args map[string]any) (map[string]any, error) {
	key, side := stringArg(args, "key"), stringArg(args, "side")
	if _, present := args["count"]; present {
		if side == "left" {
			values, err := a.client.LPopCount(ctx, key, intArg(args, "count")).Result()
			return map[string]any{"items": binaryOutputs(values)}, err
		}
		values, err := a.client.RPopCount(ctx, key, intArg(args, "count")).Result()
		return map[string]any{"items": binaryOutputs(values)}, err
	}
	if side == "left" {
		value, err := a.client.LPop(ctx, key).Result()
		return map[string]any{"value": binaryOutput(value)}, err
	}
	value, err := a.client.RPop(ctx, key).Result()
	return map[string]any{"value": binaryOutput(value)}, err
}

func popCount(args map[string]any) int64 {
	if _, present := args["count"]; !present {
		return 1
	}
	return int64Arg(args, "count")
}

func (a *Adapter) pushList(ctx context.Context, args map[string]any, onlyIfExists bool) (map[string]any, error) {
	values := interfaces(binarySlice(args["elements"]))
	var length int64
	var err error
	if stringArg(args, "side") == "left" {
		if onlyIfExists {
			length, err = a.client.LPushX(ctx, stringArg(args, "key"), values...).Result()
		} else {
			length, err = a.client.LPush(ctx, stringArg(args, "key"), values...).Result()
		}
	} else {
		if onlyIfExists {
			length, err = a.client.RPushX(ctx, stringArg(args, "key"), values...).Result()
		} else {
			length, err = a.client.RPush(ctx, stringArg(args, "key"), values...).Result()
		}
	}
	return mutation("length", length), err
}

func (a *Adapter) popSet(ctx context.Context, args map[string]any) (map[string]any, error) {
	if _, present := args["count"]; present {
		values, err := a.client.SPopN(ctx, stringArg(args, "key"), int64Arg(args, "count")).Result()
		return map[string]any{"members": binaryOutputs(values)}, err
	}
	value, err := a.client.SPop(ctx, stringArg(args, "key")).Result()
	return map[string]any{"value": binaryOutput(value)}, err
}

func (a *Adapter) addSorted(ctx context.Context, args map[string]any) (map[string]any, error) {
	items, err := sortedItems(args["items"])
	if err != nil {
		return nil, err
	}
	options := redis.ZAddArgs{Members: items, NX: stringArg(args, "condition") == "nx", XX: stringArg(args, "condition") == "xx", GT: stringArg(args, "condition") == "gt", LT: stringArg(args, "condition") == "lt", Ch: boolArg(args, "change")}
	if boolArg(args, "increment") {
		value, err := a.client.ZAddArgsIncr(ctx, stringArg(args, "key"), options).Result()
		return map[string]any{"scalar": value}, err
	}
	count, err := a.client.ZAddArgs(ctx, stringArg(args, "key"), options).Result()
	return mutation("added_count", count), err
}

func (a *Adapter) removeSortedRange(ctx context.Context, args map[string]any) (map[string]any, error) {
	key := stringArg(args, "key")
	var count int64
	var err error
	switch stringArg(args, "by") {
	case "lex":
		count, err = a.client.ZRemRangeByLex(ctx, key, stringArg(args, "min"), stringArg(args, "max")).Result()
	case "rank":
		count, err = a.client.ZRemRangeByRank(ctx, key, int64Value(args["min"], 0), int64Value(args["max"], 0)).Result()
	default:
		count, err = a.client.ZRemRangeByScore(ctx, key, fmt.Sprint(args["min"]), fmt.Sprint(args["max"])).Result()
	}
	return mutation("removed_count", count), err
}
