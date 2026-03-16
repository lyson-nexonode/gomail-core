package storage

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.uber.org/zap"

	"github.com/lyson-nexonode/gomail-core/internal/ports"
	mysqlstore "github.com/lyson-nexonode/gomail-core/internal/storage/mysql"
	redisstore "github.com/lyson-nexonode/gomail-core/internal/storage/redis"
)

// MessageStore implements ports.DeliveryPipeline.
// It orchestrates message persistence across MySQL and Redis.
var _ ports.DeliveryPipeline = (*MessageStore)(nil)

// MessageStore orchestrates message persistence across MySQL and Redis.
var _ ports.DeliveryPipeline = (*MessageStore)(nil)

type MessageStore struct {
	mysql *mysqlstore.Store
	redis *redisstore.Store
	log   *zap.Logger
}

// NewMessageStore creates a MessageStore with the given backends.
func NewMessageStore(mysql *mysqlstore.Store, redis *redisstore.Store, log *zap.Logger) *MessageStore {
	return &MessageStore{mysql: mysql, redis: redis, log: log}
}

// Deliver implements ports.DeliveryPipeline.
// It persists the message for each recipient in the event.
func (ms *MessageStore) Deliver(ctx context.Context, event ports.MessageReceived) error {
	for _, to := range event.To {
		if err := ms.deliverTo(ctx, event.From, to, event.Body); err != nil {
			ms.log.Error("delivery failed",
				zap.String("to", to),
				zap.Error(err),
			)
			// Continue delivering to other recipients even if one fails
		}
	}
	return nil
}

// deliverTo persists the message to a single recipient's INBOX.
func (ms *MessageStore) deliverTo(ctx context.Context, from, to string, body []byte) error {
	username, domain, ok := parseAddress(to)
	if !ok {
		return fmt.Errorf("invalid recipient address: %s", to)
	}

	d, err := ms.mysql.FindDomain(ctx, domain)
	if err != nil {
		return fmt.Errorf("resolve domain: %w", err)
	}

	user, err := ms.mysql.FindUser(ctx, username, d.ID)
	if err != nil {
		return fmt.Errorf("resolve user: %w", err)
	}

	inbox, err := ms.mysql.FindInboxByUserID(ctx, user.ID)
	if err != nil {
		return fmt.Errorf("find inbox: %w", err)
	}

	uid, err := ms.mysql.AllocateUID(ctx, inbox.ID)
	if err != nil {
		return fmt.Errorf("allocate uid: %w", err)
	}

	msg := &mysqlstore.Message{
		MailboxID:    inbox.ID,
		UID:          uid,
		Flags:        "",
		SizeBytes:    int64(len(body)),
		EnvelopeFrom: from,
		EnvelopeTo:   to,
		Subject:      extractSubject(body),
		InternalDate: time.Now().UTC(),
	}

	msgID, err := ms.mysql.InsertMessage(ctx, msg)
	if err != nil {
		return fmt.Errorf("insert message: %w", err)
	}

	// Cache raw body in Redis for fast IMAP FETCH
	// Non-fatal: MySQL is the source of truth
	if err := ms.redis.SetMessageBody(ctx, msgID, body); err != nil {
		ms.log.Warn("failed to cache message body",
			zap.Uint64("message_id", msgID),
			zap.Error(err),
		)
	}

	ms.log.Info("message delivered",
		zap.Uint64("message_id", msgID),
		zap.String("from", from),
		zap.String("to", to),
		zap.Uint32("uid", uid),
	)

	return nil
}

// parseAddress splits "user@domain" into username and domain.
func parseAddress(addr string) (string, string, bool) {
	parts := strings.SplitN(addr, "@", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// extractSubject parses the Subject header from a raw RFC 5322 message.
func extractSubject(body []byte) string {
	lines := strings.Split(string(body), "\n")
	for _, line := range lines {
		if strings.HasPrefix(strings.ToUpper(line), "SUBJECT:") {
			return strings.TrimSpace(line[8:])
		}
		if strings.TrimSpace(line) == "" {
			break
		}
	}
	return ""
}
