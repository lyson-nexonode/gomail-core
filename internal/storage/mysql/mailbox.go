package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Mailbox represents a folder in a user's mail account (INBOX, Sent, Trash...).
// uid_validity and uid_next are required by the IMAP protocol (RFC 3501 section 2.3.1).
type Mailbox struct {
	ID          uint64 `db:"id"`
	UserID      uint64 `db:"user_id"`
	Name        string `db:"name"`
	UIDValidity uint32 `db:"uid_validity"`
	UIDNext     uint32 `db:"uid_next"`
}

// FindMailbox returns the mailbox for the given user and folder name.
func (s *Store) FindMailbox(ctx context.Context, userID uint64, name string) (*Mailbox, error) {
	var m Mailbox
	err := s.db.GetContext(ctx, &m,
		"SELECT id, user_id, name, uid_validity, uid_next FROM mailboxes WHERE user_id = ? AND name = ?",
		userID, name,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("mailbox %q not found for user %d", name, userID)
		}
		return nil, fmt.Errorf("find mailbox: %w", err)
	}
	return &m, nil
}

// ListMailboxes returns all mailboxes for the given user.
func (s *Store) ListMailboxes(ctx context.Context, userID uint64) ([]Mailbox, error) {
	var boxes []Mailbox
	err := s.db.SelectContext(ctx, &boxes,
		"SELECT id, user_id, name, uid_validity, uid_next FROM mailboxes WHERE user_id = ?",
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list mailboxes: %w", err)
	}
	return boxes, nil
}

// AllocateUID atomically increments uid_next and returns the allocated UID.
// This guarantees that each message in a mailbox gets a unique, monotonically
// increasing UID as required by RFC 3501 section 2.3.1.1.
func (s *Store) AllocateUID(ctx context.Context, mailboxID uint64) (uint32, error) {
	tx, err := s.db.BeginTxx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("allocate uid begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var current uint32
	err = tx.QueryRowContext(ctx,
		"SELECT uid_next FROM mailboxes WHERE id = ? FOR UPDATE",
		mailboxID,
	).Scan(&current)
	if err != nil {
		return 0, fmt.Errorf("allocate uid select: %w", err)
	}

	_, err = tx.ExecContext(ctx,
		"UPDATE mailboxes SET uid_next = uid_next + 1 WHERE id = ?",
		mailboxID,
	)
	if err != nil {
		return 0, fmt.Errorf("allocate uid update: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("allocate uid commit: %w", err)
	}

	return current, nil
}

// FindInboxByUserID is a convenience method that returns the INBOX mailbox.
// Every user is guaranteed to have an INBOX created at account setup.
func (s *Store) FindInboxByUserID(ctx context.Context, userID uint64) (*Mailbox, error) {
	mb, err := s.FindMailbox(ctx, userID, "INBOX")
	if err != nil {
		return nil, fmt.Errorf("find inbox: %w", err)
	}
	return mb, nil
}

// GetMailboxByID returns a mailbox by its primary key.
func (s *Store) GetMailboxByID(ctx context.Context, id uint64) (*Mailbox, error) {
	var m Mailbox
	err := s.db.GetContext(ctx, &m,
		"SELECT id, user_id, name, uid_validity, uid_next FROM mailboxes WHERE id = ?",
		id,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("mailbox %d not found", id)
		}
		return nil, fmt.Errorf("get mailbox by id: %w", err)
	}
	return &m, nil
}
