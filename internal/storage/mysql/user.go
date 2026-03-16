package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// User represents a mail account.
type User struct {
	ID           uint64 `db:"id"`
	DomainID     uint64 `db:"domain_id"`
	Username     string `db:"username"`
	PasswordHash string `db:"password_hash"`
	QuotaBytes   int64  `db:"quota_bytes"`
}

// FindUser returns the user record for the given username and domain ID.
// Returns an error if the user does not exist or is inactive.
func (s *Store) FindUser(ctx context.Context, username string, domainID uint64) (*User, error) {
	var u User
	err := s.db.GetContext(ctx, &u,
		"SELECT id, domain_id, username, password_hash, quota_bytes FROM users WHERE username = ? AND domain_id = ? AND active = TRUE",
		username, domainID,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("user %q not found", username)
		}
		return nil, fmt.Errorf("find user: %w", err)
	}
	return &u, nil
}
