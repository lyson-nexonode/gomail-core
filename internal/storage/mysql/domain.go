package mysql

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Domain represents a mail domain managed by this server.
type Domain struct {
	ID   uint64 `db:"id"`
	Name string `db:"name"`
}

// FindDomain returns the domain record for the given name.
// Returns sql.ErrNoRows if the domain is not managed by this server.
func (s *Store) FindDomain(ctx context.Context, name string) (*Domain, error) {
	var d Domain
	err := s.db.GetContext(ctx, &d,
		"SELECT id, name FROM domains WHERE name = ? AND active = TRUE",
		name,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("domain %q not found", name)
		}
		return nil, fmt.Errorf("find domain: %w", err)
	}
	return &d, nil
}
