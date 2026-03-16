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

// compile-time checks that MessageStore implements all required ports
var _ ports.MailboxReader = (*MessageStore)(nil)
var _ ports.MessageReader = (*MessageStore)(nil)
var _ ports.DomainResolver = (*MessageStore)(nil)
var _ ports.UserAuthenticator = (*MessageStore)(nil)

// FindDomain implements ports.DomainResolver.
func (ms *MessageStore) FindDomain(ctx context.Context, name string) (*ports.Domain, error) {
	d, err := ms.mysql.FindDomain(ctx, name)
	if err != nil {
		return nil, err
	}
	return &ports.Domain{ID: d.ID, Name: d.Name}, nil
}

// FindUser implements ports.UserAuthenticator.
func (ms *MessageStore) FindUser(ctx context.Context, username string, domainID uint64) (*ports.User, error) {
	u, err := ms.mysql.FindUser(ctx, username, domainID)
	if err != nil {
		return nil, err
	}
	return &ports.User{
		ID:           u.ID,
		DomainID:     u.DomainID,
		Username:     u.Username,
		PasswordHash: u.PasswordHash,
		QuotaBytes:   u.QuotaBytes,
	}, nil
}

// FindMailbox implements ports.MailboxReader.
func (ms *MessageStore) FindMailbox(ctx context.Context, userID uint64, name string) (*ports.Mailbox, error) {
	mb, err := ms.mysql.FindMailbox(ctx, userID, name)
	if err != nil {
		return nil, err
	}
	return &ports.Mailbox{
		ID:          mb.ID,
		UserID:      mb.UserID,
		Name:        mb.Name,
		UIDValidity: mb.UIDValidity,
		UIDNext:     mb.UIDNext,
	}, nil
}

// ListMailboxes implements ports.MailboxReader.
func (ms *MessageStore) ListMailboxes(ctx context.Context, userID uint64) ([]ports.Mailbox, error) {
	boxes, err := ms.mysql.ListMailboxes(ctx, userID)
	if err != nil {
		return nil, err
	}
	result := make([]ports.Mailbox, len(boxes))
	for i, mb := range boxes {
		result[i] = ports.Mailbox{
			ID:          mb.ID,
			UserID:      mb.UserID,
			Name:        mb.Name,
			UIDValidity: mb.UIDValidity,
			UIDNext:     mb.UIDNext,
		}
	}
	return result, nil
}

// GetMailboxByID implements ports.MailboxReader.
func (ms *MessageStore) GetMailboxByID(ctx context.Context, id uint64) (*ports.Mailbox, error) {
	mb, err := ms.mysql.GetMailboxByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return &ports.Mailbox{
		ID:          mb.ID,
		UserID:      mb.UserID,
		Name:        mb.Name,
		UIDValidity: mb.UIDValidity,
		UIDNext:     mb.UIDNext,
	}, nil
}

// ListMessages implements ports.MessageReader.
func (ms *MessageStore) ListMessages(ctx context.Context, mailboxID uint64) ([]ports.Message, error) {
	msgs, err := ms.mysql.ListMessages(ctx, mailboxID)
	if err != nil {
		return nil, err
	}
	result := make([]ports.Message, len(msgs))
	for i, msg := range msgs {
		result[i] = ports.Message{
			ID:           msg.ID,
			MailboxID:    msg.MailboxID,
			UID:          msg.UID,
			Flags:        msg.Flags,
			SizeBytes:    msg.SizeBytes,
			RawKey:       msg.RawKey,
			EnvelopeFrom: msg.EnvelopeFrom,
			EnvelopeTo:   msg.EnvelopeTo,
			Subject:      msg.Subject,
		}
	}
	return result, nil
}

// GetMessageByUID implements ports.MessageReader.
func (ms *MessageStore) GetMessageByUID(ctx context.Context, mailboxID uint64, uid uint32) (*ports.Message, error) {
	msg, err := ms.mysql.GetMessageByUID(ctx, mailboxID, uid)
	if err != nil {
		return nil, err
	}
	return &ports.Message{
		ID:           msg.ID,
		MailboxID:    msg.MailboxID,
		UID:          msg.UID,
		Flags:        msg.Flags,
		SizeBytes:    msg.SizeBytes,
		RawKey:       msg.RawKey,
		EnvelopeFrom: msg.EnvelopeFrom,
		EnvelopeTo:   msg.EnvelopeTo,
		Subject:      msg.Subject,
	}, nil
}

// GetMessageBody implements ports.MessageReader.
// Tries Redis first, returns nil if not cached.
func (ms *MessageStore) GetMessageBody(ctx context.Context, messageID uint64) ([]byte, error) {
	return ms.redis.GetMessageBody(ctx, messageID)
}
