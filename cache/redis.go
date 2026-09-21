package cache

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"

	"rbac/persist"
)

const (
	KindRedis   = "redis"
	DefaultAddr = "127.0.0.1:6379"
	DefaultKey  = "rbac:policy"
	dialTimeout = 5 * time.Second
	cmdTimeout  = 3 * time.Second
)

// Redis stores the sealed policy blob as a single String. No TTL: this is a
// replica of durable state, not an expiring lookup cache.
type Redis struct {
	cli *redis.Client
	key string
}

func OpenRedis(addr, password string, db int) (*Redis, error) {
	if addr == "" {
		addr = DefaultAddr
	}
	cli := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     password,
		DB:           db,
		DialTimeout:  dialTimeout,
		ReadTimeout:  cmdTimeout,
		WriteTimeout: cmdTimeout,
		PoolSize:     8,
	})
	ctx, cancel := context.WithTimeout(context.Background(), dialTimeout)
	defer cancel()
	if err := cli.Ping(ctx).Err(); err != nil {
		_ = cli.Close()
		return nil, fmt.Errorf("cache: redis ping %s: %w", addr, err)
	}
	return &Redis{cli: cli, key: DefaultKey}, nil
}

func (r *Redis) Get(ctx context.Context) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	b, err := r.cli.Get(ctx, r.key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, persist.ErrCacheMiss
	}
	if err != nil {
		return nil, fmt.Errorf("cache: get: %w", err)
	}
	return b, nil
}

func (r *Redis) Set(ctx context.Context, blob []byte) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := r.cli.Set(ctx, r.key, blob, 0).Err(); err != nil {
		return fmt.Errorf("cache: set: %w", err)
	}
	return nil
}

func (r *Redis) Close() error {
	if r == nil || r.cli == nil {
		return nil
	}
	return r.cli.Close()
}

var _ persist.Cache = (*Redis)(nil)
