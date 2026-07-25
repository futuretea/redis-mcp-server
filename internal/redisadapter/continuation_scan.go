package redisadapter

import (
	"context"
	"fmt"
	"sort"

	"github.com/redis/go-redis/v9"
)

type scanPage func(continuationState) ([]string, continuationState, error)

func (a *Adapter) continueScan(ctx context.Context, args map[string]any, initial continuationState, itemWidth int, fetch scanPage) ([]string, string, bool, error) {
	token, hasCursor := args["cursor"].(string)
	state := initial
	if hasCursor {
		var err error
		state, err = a.continuations.get(token)
		if err != nil {
			return nil, "", false, cursorStoreError(err)
		}
		if !sameScanIdentity(state, initial) {
			return nil, "", false, callError("invalid_argument", "cursor does not match this scan")
		}
	}

	count := intArg(args, "count")
	values := takeScanItems(&state.surplus, count, itemWidth)
	if len(values)/itemWidth < count && scanHasMore(state) {
		page, next, err := fetch(state)
		if err != nil {
			return nil, "", false, err
		}
		state = next
		state.surplus = append(state.surplus, page...)
		values = append(values, takeScanItems(&state.surplus, count-len(values)/itemWidth, itemWidth)...)
	}

	if !scanHasMore(state) && len(state.surplus) == 0 {
		if hasCursor {
			a.continuations.remove(token)
		}
		return values, "", true, nil
	}
	if hasCursor {
		if err := a.continuations.replace(token, state); err != nil {
			return nil, "", false, cursorStoreError(err)
		}
		return values, token, false, nil
	}
	token, err := a.continuations.create(state)
	if err != nil {
		return nil, "", false, cursorStoreError(err)
	}
	return values, token, false, nil
}

func takeScanItems(surplus *[]string, count, itemWidth int) []string {
	available := len(*surplus) / itemWidth
	if available > count {
		available = count
	}
	length := available * itemWidth
	values := append([]string(nil), (*surplus)[:length]...)
	*surplus = append([]string(nil), (*surplus)[length:]...)
	return values
}

func sameScanIdentity(left, right continuationState) bool {
	return left.kind == right.kind && left.key == right.key && left.match == right.match && left.typeName == right.typeName && left.noValues == right.noValues
}

func scanHasMore(state continuationState) bool {
	if len(state.clusterNodes) == 0 {
		return !state.started || state.cursor != 0
	}
	return state.nextNode < len(state.clusterNodes)
}

func cursorStoreError(err error) error {
	switch err {
	case errCursorExpired:
		return callError("cursor_expired", "scan cursor expired or was evicted")
	case errCursorUnavailable:
		return callError("cursor_unavailable", "scan continuation state cannot be admitted")
	default:
		return callError("cursor_unavailable", "scan continuation state is unavailable")
	}
}

func (a *Adapter) observeInitialState(ctx context.Context, args map[string]any) (continuationState, error) {
	state := continuationState{kind: scanKindObserve, match: stringArg(args, "match"), typeName: stringArg(args, "type")}
	cluster, ok := a.client.(*redis.ClusterClient)
	if !ok {
		return state, nil
	}
	clients, err := clusterPrimaryClients(ctx, cluster)
	if err != nil {
		return continuationState{}, err
	}
	ids := make([]string, 0, len(clients))
	for id := range clients {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		client := clients[id]
		state.clusterNodes = append(state.clusterNodes, clusterScanNode{address: client.Options().Addr, id: id})
	}
	return state, nil
}

func (a *Adapter) observeScanPage(ctx context.Context, count int64) scanPage {
	return func(state continuationState) ([]string, continuationState, error) {
		if len(state.clusterNodes) > 0 {
			return a.clusterObserveScanPage(ctx, count, state)
		}
		var (
			values []string
			next   uint64
			err    error
		)
		if state.typeName == "" {
			values, next, err = a.client.Scan(ctx, state.cursor, state.match, count).Result()
		} else {
			values, next, err = a.client.ScanType(ctx, state.cursor, state.match, count, state.typeName).Result()
		}
		state.cursor = next
		state.started = true
		return values, state, err
	}
}

func (a *Adapter) clusterObserveScanPage(ctx context.Context, count int64, state continuationState) ([]string, continuationState, error) {
	cluster, ok := a.client.(*redis.ClusterClient)
	if !ok {
		return nil, state, fmt.Errorf("cluster scan state used with a non-cluster client")
	}
	clients, err := clusterPrimaryClients(ctx, cluster)
	if err != nil {
		return nil, state, err
	}
	if len(clients) != len(state.clusterNodes) {
		return nil, state, callError("topology_changed", "cluster primary set changed during scan continuation")
	}
	for _, node := range state.clusterNodes {
		if _, ok := clients[node.id]; !ok {
			return nil, state, callError("topology_changed", "cluster primary set changed during scan continuation")
		}
	}
	for state.nextNode < len(state.clusterNodes) {
		node := &state.clusterNodes[state.nextNode]
		if node.done {
			state.nextNode++
			continue
		}
		client, ok := clients[node.id]
		if !ok {
			return nil, state, callError("topology_changed", fmt.Sprintf("cluster primary %q is no longer available", node.id))
		}
		var (
			values []string
			next   uint64
			err    error
		)
		if state.typeName == "" {
			values, next, err = client.Scan(ctx, node.cursor, state.match, count).Result()
		} else {
			values, next, err = client.ScanType(ctx, node.cursor, state.match, count, state.typeName).Result()
		}
		if err != nil {
			return nil, state, err
		}
		node.cursor = next
		if next == 0 {
			node.done = true
			state.nextNode++
		}
		return values, state, nil
	}
	return nil, state, nil
}

func clusterPrimaryClients(ctx context.Context, cluster *redis.ClusterClient) (map[string]*redis.Client, error) {
	clients := make(map[string]*redis.Client)
	err := cluster.ForEachMaster(ctx, func(ctx context.Context, client *redis.Client) error {
		id, err := client.ClusterMyID(ctx).Result()
		if err != nil {
			return err
		}
		clients[id] = client
		return nil
	})
	return clients, err
}
