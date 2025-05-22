package server

import (
	"context"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/redis/go-redis/v9"
)

// RedisSessionStore implements SessionStore using Redis as backend.
type RedisSessionStore struct {
	client       *redis.Client
	ctx          context.Context
	prefix       string
	ttl          time.Duration
	local        localSessionStore
	cleanSession time.Duration
}

// NewRedisSessionStore creates a new RedisSessionStore.
func NewRedisSessionStore(addr, password string, db int, prefix string, ttl, cleanSession time.Duration) *RedisSessionStore {
	rdb := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})
	store := &RedisSessionStore{
		client:       rdb,
		ctx:          context.Background(),
		prefix:       prefix,
		ttl:          ttl,
		cleanSession: cleanSession,
	}

	go store.check()

	return store

}

func (store *RedisSessionStore) key(key string) string {
	return store.prefix + key
}

func (store *RedisSessionStore) Load(key string) (ClientSession, bool) {
	cs, ok := store.local.Load(key)
	if ok {
		return cs, true
	}

	sessionID, err := store.client.Get(store.ctx, store.key(key)).Result()
	if err != nil {
		return nil, false
	}

	var sess = &sseSession{
		done:                make(chan struct{}),
		eventQueue:          make(chan string, 100), // Buffer for events
		sessionID:           sessionID,
		notificationChannel: make(chan mcp.JSONRPCNotification, 100),
	}

	store.local.Store(key, sess)

	return sess, true
}

func (store *RedisSessionStore) LoadAndDelete(key string) (ClientSession, bool) {
	pipe := store.client.TxPipeline()
	get := pipe.Get(store.ctx, store.key(key))
	pipe.Del(store.ctx, store.key(key))
	_, err := pipe.Exec(store.ctx)
	if err != nil {
		return nil, false
	}
	sessionID, err := get.Result()
	if err != nil {
		return nil, false
	}

	var sess = &sseSession{
		done:                make(chan struct{}),
		eventQueue:          make(chan string, 100), // Buffer for events
		sessionID:           sessionID,
		notificationChannel: make(chan mcp.JSONRPCNotification, 100),
	}

	store.local.Store(key, sess)

	return sess, true
}

func (store *RedisSessionStore) LoadOrStore(key string, value ClientSession) (ClientSession, bool) {
	set, err := store.client.SetNX(store.ctx, store.key(key), value.SessionID(), store.ttl).Result()
	if err != nil {
		return nil, false
	}
	if set {
		return value, false
	}
	return store.Load(key)
}

func (store *RedisSessionStore) Range(f func(string, ClientSession) bool) {
	iter := store.client.Scan(store.ctx, 0, store.prefix+"*", 0).Iterator()
	for iter.Next(store.ctx) {
		key := iter.Val()

		cs, ok := store.local.Load(key)
		if !ok {
			sessionID, err := store.client.Get(store.ctx, key).Result()
			if err != nil {
				continue
			}

			cs = &sseSession{
				done:                make(chan struct{}),
				eventQueue:          make(chan string, 100), // Buffer for events
				sessionID:           sessionID,
				notificationChannel: make(chan mcp.JSONRPCNotification, 100),
			}

			store.local.Store(key, cs)

		}

		if !f(key[len(store.prefix):], cs) {
			break
		}
	}
}

func (store *RedisSessionStore) check() {
	ticker := time.NewTicker(store.cleanSession)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			store.checkLocalSessions()
		}
	}
}

func (store *RedisSessionStore) checkLocalSessions() {
	sessionInRedis := make(map[string]bool)

	iter := store.client.Scan(store.ctx, 0, store.prefix+"*", 0).Iterator()
	for iter.Next(store.ctx) {
		key := iter.Val()

		sessionID := strings.TrimPrefix(key, store.prefix)

		sessionInRedis[sessionID] = true

	}

	store.local.Range(func(key string, value ClientSession) bool {
		sessionID := value.SessionID()
		if _, ok := sessionInRedis[sessionID]; !ok {
			store.local.Delete(key)
		}
		return true
	})
}
