package redisadapter

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"

	redistoolset "github.com/futuretea/redis-mcp-server/pkg/toolset/redis"
)

func (a *Adapter) executeKeyAndStringWrite(ctx context.Context, call redistoolset.Call) (map[string]any, error) {
	switch call.Tool {
	case "redis_key_write":
		return a.executeKeyWrite(ctx, call)
	case "redis_string_write":
		return a.executeStringWrite(ctx, call)
	default:
		return a.executeOtherWrite(ctx, call)
	}
}

func (a *Adapter) executeKeyWrite(ctx context.Context, call redistoolset.Call) (map[string]any, error) {
	args := call.Args
	key := stringArg(args, "key")
	switch call.Operation {
	case "copy":
		if a.topology == "cluster" && intArg(args, "destination_db") != 0 {
			return nil, callError("topology_unsupported", "cluster COPY only supports destination_db 0")
		}
		count, err := a.client.Copy(ctx, stringArg(args, "source_key"), stringArg(args, "destination_key"), intArg(args, "destination_db"), boolArg(args, "replace")).Result()
		return map[string]any{"mutation": map[string]any{"copied": count == 1}}, err
	case "delete":
		keys := stringSlice(args["keys"])
		if boolArg(args, "async") {
			count, err := a.client.Unlink(ctx, keys...).Result()
			return map[string]any{"mutation": map[string]any{"deleted_count": count}}, err
		}
		count, err := a.client.Del(ctx, keys...).Result()
		return map[string]any{"mutation": map[string]any{"deleted_count": count}}, err
	case "set_expiry":
		return a.setKeyExpiry(ctx, args)
	case "persist_expiry":
		applied, err := a.client.Persist(ctx, key).Result()
		return map[string]any{"mutation": map[string]any{"applied": applied}}, err
	case "rename":
		if boolArg(args, "only_if_absent") {
			applied, err := a.client.RenameNX(ctx, stringArg(args, "source_key"), stringArg(args, "destination_key")).Result()
			return map[string]any{"mutation": map[string]any{"applied": applied}}, err
		}
		err := a.client.Rename(ctx, stringArg(args, "source_key"), stringArg(args, "destination_key")).Err()
		return map[string]any{"mutation": map[string]any{"applied": err == nil}}, err
	case "restore":
		payload, err := binaryInput(args["payload"])
		if err != nil {
			return nil, err
		}
		if boolArg(args, "replace") {
			err = a.client.RestoreReplace(ctx, key, time.Duration(int64Arg(args, "ttl_ms"))*time.Millisecond, payload).Err()
		} else {
			err = a.client.Restore(ctx, key, time.Duration(int64Arg(args, "ttl_ms"))*time.Millisecond, payload).Err()
		}
		return map[string]any{"mutation": map[string]any{"restored": err == nil}}, err
	case "touch":
		count, err := a.client.Touch(ctx, stringSlice(args["keys"])...).Result()
		return map[string]any{"mutation": map[string]any{"touched_count": count}}, err
	default:
		return nil, callError("topology_unsupported", "Redis operation mapping is unavailable")
	}
}

func (a *Adapter) executeStringWrite(ctx context.Context, call redistoolset.Call) (map[string]any, error) {
	args := call.Args
	key := stringArg(args, "key")
	switch call.Operation {
	case "append":
		value, err := binaryInput(args["value"])
		if err != nil {
			return nil, err
		}
		length, err := a.client.Append(ctx, key, value).Result()
		return map[string]any{"mutation": map[string]any{"length": length}}, err
	case "decrement":
		amount := int64(1)
		if _, present := args["amount"]; present {
			amount = int64Arg(args, "amount")
		}
		value, err := a.client.DecrBy(ctx, key, amount).Result()
		return map[string]any{"scalar": value}, err
	case "delete_if":
		return a.deleteStringIf(ctx, args)
	case "get_and_delete":
		value, err := a.client.GetDel(ctx, key).Result()
		return map[string]any{"value": binaryOutput(value)}, err
	case "get_and_set_expiry":
		return a.getAndSetExpiry(ctx, args)
	case "get_and_set":
		value, err := binaryInput(args["value"])
		if err != nil {
			return nil, err
		}
		previous, err := a.client.GetSet(ctx, key, value).Result()
		return map[string]any{"value": binaryOutput(previous)}, err
	case "increment":
		return a.incrementString(ctx, args)
	case "set_many":
		return a.setMany(ctx, args)
	case "set":
		return a.setString(ctx, args)
	case "set_range":
		value, err := binaryInput(args["value"])
		if err != nil {
			return nil, err
		}
		length, err := a.client.SetRange(ctx, key, int64Arg(args, "offset"), value).Result()
		return map[string]any{"mutation": map[string]any{"length": length}}, err
	default:
		return nil, callError("topology_unsupported", "Redis operation mapping is unavailable")
	}
}

func (a *Adapter) setKeyExpiry(ctx context.Context, args map[string]any) (map[string]any, error) {
	expiry, err := expiration(args["expiry"])
	if err != nil {
		return nil, err
	}
	key, condition := stringArg(args, "key"), stringArg(args, "condition")
	var applied bool
	switch expiry.Mode {
	case redis.EX:
		switch condition {
		case "nx":
			applied, err = a.client.ExpireNX(ctx, key, time.Duration(expiry.Value)*time.Second).Result()
		case "xx":
			applied, err = a.client.ExpireXX(ctx, key, time.Duration(expiry.Value)*time.Second).Result()
		case "gt":
			applied, err = a.client.ExpireGT(ctx, key, time.Duration(expiry.Value)*time.Second).Result()
		case "lt":
			applied, err = a.client.ExpireLT(ctx, key, time.Duration(expiry.Value)*time.Second).Result()
		default:
			applied, err = a.client.Expire(ctx, key, time.Duration(expiry.Value)*time.Second).Result()
		}
	case redis.PX:
		applied, err = a.client.PExpire(ctx, key, time.Duration(expiry.Value)*time.Millisecond).Result()
	case redis.EXAT:
		applied, err = a.client.ExpireAt(ctx, key, time.Unix(expiry.Value, 0)).Result()
	case redis.PXAT:
		applied, err = a.client.PExpireAt(ctx, key, time.UnixMilli(expiry.Value)).Result()
	default:
		return nil, callError("invalid_argument", "expiry kind is not supported for key expiry")
	}
	return map[string]any{"mutation": map[string]any{"applied": applied}}, err
}

func (a *Adapter) deleteStringIf(ctx context.Context, args map[string]any) (map[string]any, error) {
	comparison := args["comparison"]
	condition := stringArg(args, "condition")
	options := redis.DelExArgs{Mode: condition}
	if condition == "ifeq" || condition == "ifne" {
		options.MatchValue = binaryString(comparison)
	} else {
		digest, err := digestComparison(comparison)
		if err != nil {
			return nil, err
		}
		options.MatchDigest = digest
	}
	count, err := a.client.DelExArgs(ctx, stringArg(args, "key"), options).Result()
	return map[string]any{"mutation": map[string]any{"deleted_count": count}}, err
}

func (a *Adapter) getAndSetExpiry(ctx context.Context, args map[string]any) (map[string]any, error) {
	expiry, err := expiration(args["expiry"])
	if err != nil {
		return nil, err
	}
	if expiry.Mode != redis.EX && expiry.Mode != redis.PX && expiry.Mode != redis.PERSIST {
		return nil, callError("topology_unsupported", "GETEX supports relative expiry or persist")
	}
	duration := time.Duration(expiry.Value) * time.Second
	if expiry.Mode == redis.PX {
		duration = time.Duration(expiry.Value) * time.Millisecond
	}
	if expiry.Mode == redis.PERSIST {
		duration = 0
	}
	value, err := a.client.GetEx(ctx, stringArg(args, "key"), duration).Result()
	return map[string]any{"value": binaryOutput(value)}, err
}

func (a *Adapter) incrementString(ctx context.Context, args map[string]any) (map[string]any, error) {
	if rawExpiry, present := args["expiry"]; present {
		expiry, err := expiration(rawExpiry)
		if err != nil {
			return nil, err
		}
		if stringArg(args, "number_kind") == "float" {
			value, err := a.client.IncrEXFloat(ctx, stringArg(args, "key"), redis.IncrEXFloatArgs{By: floatArg(args["amount"], 1), Expiration: &expiry}).Result()
			return map[string]any{"scalar": value.Value}, err
		}
		value, err := a.client.IncrEXInt(ctx, stringArg(args, "key"), redis.IncrEXIntArgs{By: int64Value(args["amount"], 1), HasBy: true, Expiration: &expiry}).Result()
		return map[string]any{"scalar": value.Value}, err
	}
	amount := args["amount"]
	if stringArg(args, "number_kind") == "float" {
		value, err := a.client.IncrByFloat(ctx, stringArg(args, "key"), floatArg(amount, 1)).Result()
		return map[string]any{"scalar": value}, err
	}
	value, err := a.client.IncrBy(ctx, stringArg(args, "key"), int64Value(amount, 1)).Result()
	return map[string]any{"scalar": value}, err
}

func (a *Adapter) setMany(ctx context.Context, args map[string]any) (map[string]any, error) {
	values, err := keyValues(args["items"])
	if err != nil {
		return nil, err
	}
	condition := stringArg(args, "condition")
	if rawExpiry, present := args["expiry"]; present {
		expiry, err := expiration(rawExpiry)
		if err != nil {
			return nil, err
		}
		count, err := a.client.MSetEX(ctx, redis.MSetEXArgs{Condition: redis.SetCondition(condition), Expiration: &expiry}, values...).Result()
		return map[string]any{"mutation": map[string]any{"applied": count > 0}}, err
	}
	if condition == "nx" {
		applied, err := a.client.MSetNX(ctx, values...).Result()
		return map[string]any{"mutation": map[string]any{"applied": applied}}, err
	}
	if condition == "xx" {
		count, err := a.client.MSetEX(ctx, redis.MSetEXArgs{Condition: redis.XX}, values...).Result()
		return map[string]any{"mutation": map[string]any{"applied": count > 0}}, err
	}
	err = a.client.MSet(ctx, values...).Err()
	return map[string]any{"mutation": map[string]any{"applied": err == nil}}, err
}

func (a *Adapter) setString(ctx context.Context, args map[string]any) (map[string]any, error) {
	value, err := binaryInput(args["value"])
	if err != nil {
		return nil, err
	}
	options, err := setStringOptions(args)
	if err != nil {
		return nil, err
	}
	if options.Get && conditionCanApplyWithoutPrevious(options.Mode) {
		options.Get = false
	}
	if !options.Get && conditionCanApplyWithoutPrevious(options.Mode) {
		_, err := a.client.SetArgs(ctx, stringArg(args, "key"), value, options).Result()
		if errors.Is(err, redis.Nil) {
			return nil, callError("condition_not_met", "Redis write condition was not met")
		}
		if err != nil {
			return nil, err
		}
		return map[string]any{"mutation": map[string]any{"applied": true}}, nil
	}
	result, err := a.client.SetArgs(ctx, stringArg(args, "key"), value, options).Result()
	if errors.Is(err, redis.Nil) && options.Mode != "" {
		return nil, callError("condition_not_met", "Redis write condition was not met")
	}
	if errors.Is(err, redis.Nil) && options.Get {
		return map[string]any{"mutation": map[string]any{"applied": true}}, nil
	}
	if err != nil {
		return nil, err
	}
	mutation := map[string]any{"applied": true}
	if options.Get {
		mutation["previous"] = binaryOutput(result)
	}
	return map[string]any{"mutation": mutation}, nil
}

func conditionCanApplyWithoutPrevious(condition string) bool {
	return condition == "nx"
}

func setStringOptions(args map[string]any) (redis.SetArgs, error) {
	options := redis.SetArgs{Mode: stringArg(args, "condition"), Get: boolArg(args, "return_previous")}
	if options.Get && (options.Mode == "ifne" || options.Mode == "ifdne") {
		return redis.SetArgs{}, callError("invalid_argument", "return_previous is unavailable for non-equal conditional SET")
	}
	if options.Mode == "ifeq" || options.Mode == "ifne" {
		comparison, err := binaryInput(args["comparison"])
		if err != nil {
			return redis.SetArgs{}, err
		}
		options.MatchValue = comparison
	}
	if options.Mode == "ifdeq" || options.Mode == "ifdne" {
		digest, err := digestComparison(args["comparison"])
		if err != nil {
			return redis.SetArgs{}, err
		}
		options.MatchDigest = digest
	}
	raw, present := args["expiry"]
	if !present {
		return options, nil
	}
	expiry, err := expiration(raw)
	if err != nil {
		return redis.SetArgs{}, err
	}
	switch expiry.Mode {
	case redis.EX:
		options.TTL = time.Duration(expiry.Value) * time.Second
	case redis.PX:
		options.TTL = time.Duration(expiry.Value) * time.Millisecond
	case redis.EXAT:
		options.ExpireAt = time.Unix(expiry.Value, 0)
	case redis.KEEPTTL:
		options.KeepTTL = true
	default:
		return redis.SetArgs{}, callError("invalid_argument", "SET expiry kind is unavailable")
	}
	return options, nil
}

func digestComparison(value any) (uint64, error) {
	text, ok := value.(string)
	if !ok || len(text) != 16 {
		return 0, fmt.Errorf("digest comparison must be a 16-character lowercase hexadecimal digest")
	}
	digest, err := strconv.ParseUint(text, 16, 64)
	if err != nil || fmt.Sprintf("%016x", digest) != text {
		return 0, fmt.Errorf("digest comparison must be a 16-character lowercase hexadecimal digest")
	}
	if digest == 0 {
		return 0, callError("invalid_argument", "digest comparison must be non-zero")
	}
	return digest, nil
}
