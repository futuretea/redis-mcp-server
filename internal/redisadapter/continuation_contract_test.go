package redisadapter

import (
	"context"
	"encoding/base64"
	"errors"
	"testing"
	"time"
)

func TestContinuationStoreUsesFixedOpaqueReferencesAndExpiresIdleSessions(t *testing.T) {
	now := time.Date(2026, time.July, 25, 12, 0, 0, 0, time.UTC)
	store := newContinuationStore(2, 1024, func() time.Time { return now })
	token, err := store.create(continuationState{kind: scanKindSet, key: "set", surplus: []string{"member"}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if len(token) != continuationReferenceLength {
		t.Fatalf("token length = %d, want %d", len(token), continuationReferenceLength)
	}
	if _, err := base64.RawURLEncoding.DecodeString(token); err != nil {
		t.Fatalf("token is not base64url: %v", err)
	}
	if state, err := store.get(token); err != nil || len(state.surplus) != 1 {
		t.Fatalf("get = %#v, %v", state, err)
	}

	now = now.Add(continuationIdleTTL + time.Nanosecond)
	if _, err := store.get(token); !errors.Is(err, errCursorExpired) {
		t.Fatalf("expired get error = %v, want cursor_expired", err)
	}
}

func TestContinuationStoreEvictsWholeLeastRecentlyUsedSession(t *testing.T) {
	now := time.Date(2026, time.July, 25, 12, 0, 0, 0, time.UTC)
	store := newContinuationStore(2, 1024, func() time.Time { return now })
	first, err := store.create(continuationState{kind: scanKindObserve, surplus: []string{"first"}})
	if err != nil {
		t.Fatalf("first create: %v", err)
	}
	now = now.Add(time.Second)
	second, err := store.create(continuationState{kind: scanKindObserve, surplus: []string{"second"}})
	if err != nil {
		t.Fatalf("second create: %v", err)
	}
	now = now.Add(time.Second)
	state, err := store.get(second)
	if err != nil {
		t.Fatalf("second get: %v", err)
	}
	if err := store.replace(second, state); err != nil {
		t.Fatalf("second successful continuation: %v", err)
	}
	now = now.Add(time.Second)
	if _, err := store.create(continuationState{kind: scanKindObserve, surplus: []string{"third"}}); err != nil {
		t.Fatalf("third create: %v", err)
	}
	if _, err := store.get(first); !errors.Is(err, errCursorExpired) {
		t.Fatalf("evicted get error = %v, want cursor_expired", err)
	}
}

func TestContinuationStoreRefreshesOnlyAfterSuccessfulContinuation(t *testing.T) {
	now := time.Date(2026, time.July, 25, 12, 0, 0, 0, time.UTC)
	store := newContinuationStore(2, 1024, func() time.Time { return now })
	token, err := store.create(continuationState{kind: scanKindSet, surplus: []string{"member"}})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	now = now.Add(continuationIdleTTL - time.Second)
	state, err := store.get(token)
	if err != nil {
		t.Fatalf("get before expiry: %v", err)
	}
	if err := store.replace(token, state); err != nil {
		t.Fatalf("replace after successful continuation: %v", err)
	}
	now = now.Add(continuationIdleTTL - time.Second)
	if _, err := store.get(token); err != nil {
		t.Fatalf("get after refreshed TTL: %v", err)
	}
	now = now.Add(time.Minute + time.Second)
	if _, err := store.get(token); !errors.Is(err, errCursorExpired) {
		t.Fatalf("get after refreshed expiry = %v, want cursor_expired", err)
	}
}

func TestContinuationScanDrainsCompactPageWithoutExposingState(t *testing.T) {
	store := newContinuationStore(2, 1024, time.Now)
	adapter := &Adapter{continuations: store}
	initial := continuationState{kind: scanKindSet, key: "set"}
	fetch := func(state continuationState) ([]string, continuationState, error) {
		state.started = true
		return []string{"first", "second"}, state, nil
	}
	values, token, complete, err := adapter.continueScan(context.Background(), map[string]any{"count": float64(1)}, initial, 1, fetch)
	if err != nil {
		t.Fatalf("first page: %v", err)
	}
	if complete || len(values) != 1 || values[0] != "first" || len(token) != continuationReferenceLength {
		t.Fatalf("first page = values=%v token=%q complete=%v", values, token, complete)
	}
	values, next, complete, err := adapter.continueScan(context.Background(), map[string]any{"count": float64(1), "cursor": token}, initial, 1, fetch)
	if err != nil {
		t.Fatalf("second page: %v", err)
	}
	if !complete || next != "" || len(values) != 1 || values[0] != "second" {
		t.Fatalf("second page = values=%v token=%q complete=%v", values, next, complete)
	}
}

func TestContinuationStoreRejectsOversizedNewState(t *testing.T) {
	state := continuationState{kind: scanKindSet, surplus: []string{"a buffered Redis member"}}
	size, err := encodedStateSize(state)
	if err != nil {
		t.Fatalf("encoded state size: %v", err)
	}
	if _, err := newContinuationStore(1, size-1, time.Now).create(state); !errors.Is(err, errCursorUnavailable) {
		t.Fatalf("oversized create = %v, want cursor_unavailable", err)
	}
}
