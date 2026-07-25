package redisadapter

import (
	"context"
	"fmt"
	"strconv"

	"github.com/redis/go-redis/v9"

	redistoolset "github.com/futuretea/redis-mcp-server/pkg/toolset/redis"
)

func (a *Adapter) executeCollectionRead(ctx context.Context, call redistoolset.Call) (map[string]any, error) {
	switch call.Tool {
	case "redis_list":
		return a.executeListRead(ctx, call)
	case "redis_set":
		return a.executeSetRead(ctx, call)
	case "redis_sorted_set":
		return a.executeSortedSetRead(ctx, call)
	case "redis_stream":
		return a.executeStreamRead(ctx, call)
	default:
		return nil, callError("topology_unsupported", "Redis operation mapping is unavailable")
	}
}

func (a *Adapter) executeListRead(ctx context.Context, call redistoolset.Call) (map[string]any, error) {
	args := call.Args
	key := stringArg(args, "key")
	switch call.Operation {
	case "index":
		value, err := a.client.LIndex(ctx, key, int64Arg(args, "index")).Result()
		return map[string]any{"value": binaryOutput(value)}, err
	case "length":
		count, err := a.client.LLen(ctx, key).Result()
		return map[string]any{"count": count}, err
	case "position":
		return a.listPosition(ctx, args)
	case "range":
		start, count := int64Arg(args, "start"), int64Arg(args, "count")
		values, err := a.client.LRange(ctx, key, start, start+count-1).Result()
		return map[string]any{"items": binaryOutputs(values)}, err
	default:
		return nil, callError("topology_unsupported", "Redis operation mapping is unavailable")
	}
}

func (a *Adapter) executeSetRead(ctx context.Context, call redistoolset.Call) (map[string]any, error) {
	args := call.Args
	key := stringArg(args, "key")
	switch call.Operation {
	case "cardinality":
		count, err := a.client.SCard(ctx, key).Result()
		return map[string]any{"count": count}, err
	case "intersection_cardinality":
		count, err := a.client.SInterCard(ctx, int64Arg(args, "limit"), stringSlice(args["keys"])...).Result()
		return map[string]any{"count": count}, err
	case "member_exists":
		value, err := a.client.SIsMember(ctx, key, binaryString(args["member"])).Result()
		return map[string]any{"scalar": value}, err
	case "members_exist":
		members := interfaces(binarySlice(args["members"]))
		values, err := a.client.SMIsMember(ctx, key, members...).Result()
		return map[string]any{"items": values}, err
	case "random_members":
		return a.randomSetMembers(ctx, args)
	case "scan":
		return a.setScan(ctx, args)
	default:
		return nil, callError("topology_unsupported", "Redis operation mapping is unavailable")
	}
}

func (a *Adapter) executeSortedSetRead(ctx context.Context, call redistoolset.Call) (map[string]any, error) {
	args := call.Args
	key := stringArg(args, "key")
	switch call.Operation {
	case "cardinality":
		count, err := a.client.ZCard(ctx, key).Result()
		return map[string]any{"count": count}, err
	case "count_by_score":
		min, err := scoreBound(args["min"])
		if err != nil {
			return nil, err
		}
		max, err := scoreBound(args["max"])
		if err != nil {
			return nil, err
		}
		count, err := a.client.ZCount(ctx, key, min, max).Result()
		return map[string]any{"count": count}, err
	case "intersection_cardinality":
		count, err := a.client.ZInterCard(ctx, int64Arg(args, "limit"), stringSlice(args["keys"])...).Result()
		return map[string]any{"count": count}, err
	case "lex_count":
		min, err := lexBound(args["min"])
		if err != nil {
			return nil, err
		}
		max, err := lexBound(args["max"])
		if err != nil {
			return nil, err
		}
		count, err := a.client.ZLexCount(ctx, key, min, max).Result()
		return map[string]any{"count": count}, err
	case "scores":
		values, err := a.client.ZMScore(ctx, key, binarySlice(args["members"])...).Result()
		return map[string]any{"items": values}, err
	case "random_members":
		return a.randomSortedMembers(ctx, args)
	case "range":
		return a.sortedRange(ctx, args)
	case "rank":
		return a.sortedRank(ctx, args)
	case "scan":
		return a.sortedScan(ctx, args)
	case "score":
		value, err := a.client.ZScore(ctx, key, binaryString(args["member"])).Result()
		return map[string]any{"scalar": value}, err
	default:
		return nil, callError("topology_unsupported", "Redis operation mapping is unavailable")
	}
}

func (a *Adapter) executeStreamRead(ctx context.Context, call redistoolset.Call) (map[string]any, error) {
	args := call.Args
	key := stringArg(args, "key")
	switch call.Operation {
	case "length":
		count, err := a.client.XLen(ctx, key).Result()
		return map[string]any{"count": count}, err
	case "pending":
		return a.streamPending(ctx, args)
	default:
		return nil, callError("topology_unsupported", "Redis operation mapping is unavailable")
	}
}

func scoreBound(value any) (string, error) {
	switch value := value.(type) {
	case string:
		return value, nil
	case float64:
		return strconv.FormatFloat(value, 'g', -1, 64), nil
	default:
		return "", callError("invalid_argument", "score bound must be a number or Redis bound string")
	}
}

func lexBound(value any) (string, error) {
	bound, ok := value.(string)
	if !ok {
		return "", callError("invalid_argument", "lex bound must be a string")
	}
	return bound, nil
}

func (a *Adapter) listPosition(ctx context.Context, args map[string]any) (map[string]any, error) {
	options := redis.LPosArgs{Rank: int64Arg(args, "rank"), MaxLen: int64Arg(args, "maxlen")}
	key, element := stringArg(args, "key"), binaryString(args["element"])
	if _, present := args["count"]; present {
		values, err := a.client.LPosCount(ctx, key, element, int64Arg(args, "count"), options).Result()
		return map[string]any{"items": values}, err
	}
	value, err := a.client.LPos(ctx, key, element, options).Result()
	return map[string]any{"scalar": value}, err
}

func (a *Adapter) randomSetMembers(ctx context.Context, args map[string]any) (map[string]any, error) {
	key := stringArg(args, "key")
	if _, present := args["count"]; present {
		values, err := a.client.SRandMemberN(ctx, key, int64Arg(args, "count")).Result()
		return map[string]any{"members": binaryOutputs(values)}, err
	}
	value, err := a.client.SRandMember(ctx, key).Result()
	return map[string]any{"value": binaryOutput(value)}, err
}

func (a *Adapter) setScan(ctx context.Context, args map[string]any) (map[string]any, error) {
	state := continuationState{kind: scanKindSet, key: stringArg(args, "key"), match: stringArg(args, "match")}
	values, next, _, err := a.continueScan(ctx, args, state, 1, func(state continuationState) ([]string, continuationState, error) {
		page, cursor, pageErr := a.client.SScan(ctx, state.key, state.cursor, state.match, int64Arg(args, "count")).Result()
		state.cursor, state.started = cursor, true
		return page, state, pageErr
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"members": binaryOutputs(values), "metadata": map[string]any{"next_cursor": next}}, nil
}

func (a *Adapter) randomSortedMembers(ctx context.Context, args map[string]any) (map[string]any, error) {
	count := intArg(args, "count")
	if count == 0 {
		count = 1
	}
	if withScores, _ := args["with_scores"].(bool); withScores {
		values, err := a.client.ZRandMemberWithScores(ctx, stringArg(args, "key"), count).Result()
		return map[string]any{"members": sortedMembers(values)}, err
	}
	values, err := a.client.ZRandMember(ctx, stringArg(args, "key"), count).Result()
	if _, present := args["count"]; present {
		return map[string]any{"members": binaryOutputs(values)}, err
	}
	if len(values) == 0 {
		return map[string]any{"value": nil}, err
	}
	return map[string]any{"value": binaryOutput(values[0])}, err
}

func (a *Adapter) sortedRange(ctx context.Context, args map[string]any) (map[string]any, error) {
	by := stringArg(args, "by")
	rangeArgs := redis.ZRangeArgs{Key: stringArg(args, "key")}
	rangeArgs.ByScore = by == "score"
	rangeArgs.ByLex = by == "lex"
	rangeArgs.Rev, _ = args["reverse"].(bool)
	if by == "rank" {
		start, count := int64Arg(args, "start"), int64Arg(args, "count")
		rangeArgs.Start = start
		rangeArgs.Stop = start + count - 1
	} else {
		rangeArgs.Start = args["min"]
		rangeArgs.Stop = args["max"]
		rangeArgs.Offset = int64Arg(args, "offset")
		rangeArgs.Count = int64Arg(args, "count")
	}
	if withScores, _ := args["with_scores"].(bool); withScores {
		values, err := a.client.ZRangeArgsWithScores(ctx, rangeArgs).Result()
		return map[string]any{"members": sortedMembers(values)}, err
	}
	values, err := a.client.ZRangeArgs(ctx, rangeArgs).Result()
	return map[string]any{"members": binaryOutputs(values)}, err
}

func (a *Adapter) sortedRank(ctx context.Context, args map[string]any) (map[string]any, error) {
	key, member := stringArg(args, "key"), binaryString(args["member"])
	reverse, withScore := false, false
	reverse, _ = args["reverse"].(bool)
	withScore, _ = args["with_score"].(bool)
	if withScore {
		var result *redis.RankWithScoreCmd
		if reverse {
			result = a.client.ZRevRankWithScore(ctx, key, member)
		} else {
			result = a.client.ZRankWithScore(ctx, key, member)
		}
		value, err := result.Result()
		return map[string]any{"scalar": value.Rank, "metadata": map[string]any{"score": value.Score}}, err
	}
	if reverse {
		value, err := a.client.ZRevRank(ctx, key, member).Result()
		return map[string]any{"scalar": value}, err
	}
	value, err := a.client.ZRank(ctx, key, member).Result()
	return map[string]any{"scalar": value}, err
}

func (a *Adapter) sortedScan(ctx context.Context, args map[string]any) (map[string]any, error) {
	state := continuationState{kind: scanKindSorted, key: stringArg(args, "key"), match: stringArg(args, "match")}
	values, next, _, err := a.continueScan(ctx, args, state, 2, func(state continuationState) ([]string, continuationState, error) {
		page, cursor, pageErr := a.client.ZScan(ctx, state.key, state.cursor, state.match, int64Arg(args, "count")).Result()
		state.cursor, state.started = cursor, true
		return page, state, pageErr
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"members": binaryPairs(values), "metadata": map[string]any{"next_cursor": next}}, nil
}

func sortedMembers(values []redis.Z) []map[string]any {
	result := make([]map[string]any, 0, len(values))
	for _, value := range values {
		result = append(result, map[string]any{"member": binaryOutput(fmt.Sprint(value.Member)), "score": value.Score})
	}
	return result
}

func interfaces(values []string) []interface{} {
	result := make([]interface{}, len(values))
	for index, value := range values {
		result[index] = value
	}
	return result
}
