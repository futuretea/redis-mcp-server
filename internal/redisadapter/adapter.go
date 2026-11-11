// Package redisadapter owns Redis client selection and lifecycle.
package redisadapter

import (
	"crypto/tls"
	"fmt"
	"sync"

	"github.com/redis/go-redis/v9"

	"github.com/futuretea/redis-mcp-server/pkg/core/config"
)

// Adapter owns exactly one explicit go-redis topology client.
type Adapter struct {
	topology      string
	client        redis.UniversalClient
	continuations *continuationStore
	aclProbe      aclProbe
	close         sync.Once
	closeErr      error
}

// New constructs a topology-specific client without probing Redis. Service
// startup must call VerifyStartup before any tool command. The caller owns
// Close and no configuration value retains the password.
func New(cfg config.RedisConfig) (*Adapter, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	password := cfg.Password()
	var tlsConfig *tls.Config
	if cfg.TLS {
		tlsConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	}

	adapter := &Adapter{topology: cfg.Topology, continuations: defaultContinuationStore()}
	switch cfg.Topology {
	case "standalone":
		adapter.client = redis.NewClient(&redis.Options{
			Addr:       cfg.Addresses[0],
			Username:   cfg.Username,
			Password:   password,
			TLSConfig:  tlsConfig,
			MaxRetries: -1,
		})
	case "cluster":
		adapter.client = redis.NewClusterClient(&redis.ClusterOptions{
			Addrs:      cfg.Addresses,
			Username:   cfg.Username,
			Password:   password,
			TLSConfig:  tlsConfig,
			MaxRetries: -1,
		})
	case "sentinel":
		adapter.client = redis.NewFailoverClient(&redis.FailoverOptions{
			MasterName:       cfg.SentinelMaster,
			SentinelAddrs:    cfg.Addresses,
			Username:         cfg.Username,
			Password:         password,
			SentinelUsername: cfg.Username,
			SentinelPassword: password,
			TLSConfig:        tlsConfig,
			MaxRetries:       -1,
		})
	default:
		return nil, fmt.Errorf("unsupported redis topology %q", cfg.Topology)
	}
	return adapter, nil
}

// Topology returns the explicit topology selected at construction time.
func (a *Adapter) Topology() string {
	if a == nil {
		return ""
	}
	return a.topology
}

// Close closes the underlying client at most once.
func (a *Adapter) Close() error {
	if a == nil {
		return nil
	}
	a.close.Do(func() {
		if a.continuations != nil {
			a.continuations.close()
		}
		if a.client != nil {
			a.closeErr = a.client.Close()
		}
	})
	return a.closeErr
}
