package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

const (
	// bodyTTL is how long a raw message body is kept in Redis.
	// After this duration, IMAP will fall back to the permanent storage.
	// 24 hours covers the vast majority of immediate reads after delivery.
	bodyTTL = 24 * time.Hour

	// sessionTTL is the lifetime of an IMAP session cache entry.
	sessionTTL = 30 * time.Minute
)

// Store wraps a Redis client and provides mail-specific cache operations.
type Store struct {
	client *redis.Client
	log    *zap.Logger
}

// New creates a new Redis store and verifies connectivity with a ping.
func New(addr, password string, db int, log *zap.Logger) (*Store, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: password,
		DB:       db,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping: %w", err)
	}

	log.Info("redis connected", zap.String("addr", addr))

	return &Store{client: client, log: log}, nil
}

// Close releases the Redis connection.
func (s *Store) Close() error {
	return s.client.Close()
}

// SetMessageBody stores the raw message body in Redis.
// The key format is "msg:body:{messageID}".
// The body expires after 24 hours — permanent storage is in MySQL.
func (s *Store) SetMessageBody(ctx context.Context, messageID uint64, body []byte) error {
	key := messageBodyKey(messageID)
	if err := s.client.Set(ctx, key, body, bodyTTL).Err(); err != nil {
		return fmt.Errorf("redis set message body: %w", err)
	}
	s.log.Debug("message body cached",
		zap.String("key", key),
		zap.Int("size", len(body)),
	)
	return nil
}

// GetMessageBody retrieves the raw message body from Redis.
// Returns nil, nil if the key has expired or does not exist.
func (s *Store) GetMessageBody(ctx context.Context, messageID uint64) ([]byte, error) {
	key := messageBodyKey(messageID)
	data, err := s.client.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			// Cache miss — caller should fall back to permanent storage
			return nil, nil
		}
		return nil, fmt.Errorf("redis get message body: %w", err)
	}
	return data, nil
}

// SetRateLimit increments the request counter for a given key and sets a TTL.
// Returns the current count after increment.
// Used to rate-limit SMTP connections per IP address.
func (s *Store) SetRateLimit(ctx context.Context, key string, window time.Duration) (int64, error) {
	pipe := s.client.Pipeline()
	incr := pipe.Incr(ctx, "ratelimit:"+key)
	pipe.Expire(ctx, "ratelimit:"+key, window)

	if _, err := pipe.Exec(ctx); err != nil {
		return 0, fmt.Errorf("redis rate limit: %w", err)
	}

	return incr.Val(), nil
}

// messageBodyKey returns the Redis key for a raw message body.
func messageBodyKey(messageID uint64) string {
	return fmt.Sprintf("msg:body:%d", messageID)
}
