package mysql

import (
	"context"
	"fmt"
	"time"
)

// Message represents the metadata of a stored email.
// The raw message body is stored separately in Redis (see redis/store.go).
// This separation keeps MySQL lean and allows fast metadata queries.
type Message struct {
	ID           uint64    `db:"id"`
	MailboxID    uint64    `db:"mailbox_id"`
	UID          uint32    `db:"uid"`
	Flags        string    `db:"flags"`
	SizeBytes    int64     `db:"size_bytes"`
	RawKey       string    `db:"raw_key"`        // Redis key or fallback storage path
	EnvelopeFrom string    `db:"envelope_from"`
	EnvelopeTo   string    `db:"envelope_to"`
	Subject      string    `db:"subject"`
	InternalDate time.Time `db:"internal_date"`
}

// InsertMessage stores a new message record and returns its generated ID.
func (s *Store) InsertMessage(ctx context.Context, msg *Message) (uint64, error) {
	result, err := s.db.ExecContext(ctx,
		`INSERT INTO messages
			(mailbox_id, uid, flags, size_bytes, raw_key, envelope_from, envelope_to, subject, internal_date)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		msg.MailboxID,
		msg.UID,
		msg.Flags,
		msg.SizeBytes,
		msg.RawKey,
		msg.EnvelopeFrom,
		msg.EnvelopeTo,
		msg.Subject,
		msg.InternalDate,
	)
	if err != nil {
		return 0, fmt.Errorf("insert message: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("insert message last id: %w", err)
	}

	return uint64(id), nil
}

// ListMessages returns all messages in a mailbox ordered by UID.
func (s *Store) ListMessages(ctx context.Context, mailboxID uint64) ([]Message, error) {
	var msgs []Message
	err := s.db.SelectContext(ctx, &msgs,
		`SELECT id, mailbox_id, uid, flags, size_bytes, raw_key, envelope_from, envelope_to, subject, internal_date
		 FROM messages WHERE mailbox_id = ? ORDER BY uid ASC`,
		mailboxID,
	)
	if err != nil {
		return nil, fmt.Errorf("list messages: %w", err)
	}
	return msgs, nil
}

// GetMessageByUID returns a single message by its IMAP UID within a mailbox.
func (s *Store) GetMessageByUID(ctx context.Context, mailboxID uint64, uid uint32) (*Message, error) {
	var msg Message
	err := s.db.GetContext(ctx, &msg,
		`SELECT id, mailbox_id, uid, flags, size_bytes, raw_key, envelope_from, envelope_to, subject, internal_date
		 FROM messages WHERE mailbox_id = ? AND uid = ?`,
		mailboxID, uid,
	)
	if err != nil {
		return nil, fmt.Errorf("get message by uid: %w", err)
	}
	return &msg, nil
}
