package redisadapter

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"time"
)

const (
	continuationIdleTTL         = 5 * time.Minute
	continuationSessionLimit    = 1000
	continuationEncodedMaxSize  = 16 * 1024 * 1024
	continuationReferenceLength = 22
)

var (
	errCursorExpired     = errors.New("cursor expired")
	errCursorUnavailable = errors.New("cursor unavailable")
)

type scanKind string

const (
	scanKindObserve scanKind = "observe"
	scanKindHash    scanKind = "hash"
	scanKindSet     scanKind = "set"
	scanKindSorted  scanKind = "sorted"
)

type continuationState struct {
	kind         scanKind
	key          string
	match        string
	typeName     string
	noValues     bool
	cursor       uint64
	started      bool
	surplus      []string
	clusterNodes []clusterScanNode
	nextNode     int
}

type clusterScanNode struct {
	address string
	id      string
	cursor  uint64
	done    bool
}

type continuationSession struct {
	state      continuationState
	lastAccess time.Time
	expiresAt  time.Time
	encodedLen int
}

type continuationStore struct {
	mu             sync.Mutex
	sessions       map[string]continuationSession
	maxSessions    int
	maxEncodedSize int
	encodedSize    int
	now            func() time.Time
}

func newContinuationStore(maxSessions, maxEncodedSize int, now func() time.Time) *continuationStore {
	return &continuationStore{
		sessions:       make(map[string]continuationSession),
		maxSessions:    maxSessions,
		maxEncodedSize: maxEncodedSize,
		now:            now,
	}
}

func defaultContinuationStore() *continuationStore {
	return newContinuationStore(continuationSessionLimit, continuationEncodedMaxSize, time.Now)
}

func (s *continuationStore) create(state continuationState) (string, error) {
	encodedLen, err := encodedStateSize(state)
	if err != nil {
		return "", errCursorUnavailable
	}
	if encodedLen > s.maxEncodedSize {
		return "", errCursorUnavailable
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.evictExpired(now)
	for len(s.sessions) >= s.maxSessions || s.encodedSize+encodedLen > s.maxEncodedSize {
		if !s.evictLeastRecentlyUsed() {
			return "", errCursorUnavailable
		}
	}
	for attempt := 0; attempt < 10; attempt++ {
		token, err := newContinuationReference()
		if err != nil {
			return "", errCursorUnavailable
		}
		if _, exists := s.sessions[token]; exists {
			continue
		}
		s.sessions[token] = continuationSession{
			state: cloneContinuationState(state), lastAccess: now, expiresAt: now.Add(continuationIdleTTL), encodedLen: encodedLen,
		}
		s.encodedSize += encodedLen
		return token, nil
	}
	return "", errCursorUnavailable
}

func (s *continuationStore) get(token string) (continuationState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.evictExpired(now)
	session, ok := s.sessions[token]
	if !ok {
		return continuationState{}, errCursorExpired
	}
	return cloneContinuationState(session.state), nil
}

func (s *continuationStore) replace(token string, state continuationState) error {
	encodedLen, err := encodedStateSize(state)
	if err != nil || encodedLen > s.maxEncodedSize {
		return errCursorUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	s.evictExpired(now)
	previous, ok := s.sessions[token]
	if !ok {
		return errCursorExpired
	}
	s.encodedSize -= previous.encodedLen
	for s.encodedSize+encodedLen > s.maxEncodedSize {
		if !s.evictLeastRecentlyUsedExcept(token) {
			s.encodedSize += previous.encodedLen
			return errCursorUnavailable
		}
	}
	s.sessions[token] = continuationSession{
		state: cloneContinuationState(state), lastAccess: now, expiresAt: now.Add(continuationIdleTTL), encodedLen: encodedLen,
	}
	s.encodedSize += encodedLen
	return nil
}

func (s *continuationStore) remove(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if session, ok := s.sessions[token]; ok {
		delete(s.sessions, token)
		s.encodedSize -= session.encodedLen
	}
}

func (s *continuationStore) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions = make(map[string]continuationSession)
	s.encodedSize = 0
}

func (s *continuationStore) evictExpired(now time.Time) {
	for token, session := range s.sessions {
		if !now.Before(session.expiresAt) {
			delete(s.sessions, token)
			s.encodedSize -= session.encodedLen
		}
	}
}

func (s *continuationStore) evictLeastRecentlyUsed() bool {
	return s.evictLeastRecentlyUsedExcept("")
}

func (s *continuationStore) evictLeastRecentlyUsedExcept(except string) bool {
	if len(s.sessions) == 0 {
		return false
	}
	tokens := make([]string, 0, len(s.sessions))
	for token := range s.sessions {
		if token != except {
			tokens = append(tokens, token)
		}
	}
	if len(tokens) == 0 {
		return false
	}
	sort.Slice(tokens, func(i, j int) bool {
		left, right := s.sessions[tokens[i]], s.sessions[tokens[j]]
		if left.lastAccess.Equal(right.lastAccess) {
			return tokens[i] < tokens[j]
		}
		return left.lastAccess.Before(right.lastAccess)
	})
	victim := s.sessions[tokens[0]]
	delete(s.sessions, tokens[0])
	s.encodedSize -= victim.encodedLen
	return true
}

func encodedStateSize(state continuationState) (int, error) {
	type encodedNode struct {
		Address string `json:"address"`
		ID      string `json:"id"`
		Cursor  uint64 `json:"cursor"`
		Done    bool   `json:"done"`
	}
	type encodedState struct {
		Kind         scanKind      `json:"kind"`
		Key          string        `json:"key"`
		Match        string        `json:"match"`
		TypeName     string        `json:"type_name"`
		NoValues     bool          `json:"no_values"`
		Cursor       uint64        `json:"cursor"`
		Started      bool          `json:"started"`
		Surplus      []string      `json:"surplus"`
		ClusterNodes []encodedNode `json:"cluster_nodes"`
		NextNode     int           `json:"next_node"`
	}
	nodes := make([]encodedNode, len(state.clusterNodes))
	for index, node := range state.clusterNodes {
		nodes[index] = encodedNode{Address: node.address, ID: node.id, Cursor: node.cursor, Done: node.done}
	}
	encoded, err := json.Marshal(encodedState{
		Kind: state.kind, Key: state.key, Match: state.match, TypeName: state.typeName,
		NoValues: state.noValues, Cursor: state.cursor, Started: state.started, Surplus: state.surplus,
		ClusterNodes: nodes, NextNode: state.nextNode,
	})
	if err != nil {
		return 0, err
	}
	return base64.RawURLEncoding.EncodedLen(len(encoded)), nil
}

func cloneContinuationState(state continuationState) continuationState {
	state.surplus = append([]string(nil), state.surplus...)
	state.clusterNodes = append([]clusterScanNode(nil), state.clusterNodes...)
	return state
}

func newContinuationReference() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}
