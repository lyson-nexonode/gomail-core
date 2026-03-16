package mysql

import (
	"context"
	"fmt"

	"github.com/jmoiron/sqlx"
	_ "github.com/go-sql-driver/mysql" // MySQL driver registration

	"go.uber.org/zap"
)

// Store wraps a sqlx.DB connection and provides mail-specific query methods.
// It is the single point of access to the MySQL database for all services.
type Store struct {
	db  *sqlx.DB
	log *zap.Logger
}

// New opens a MySQL connection and verifies it with a ping.
func New(dsn string, log *zap.Logger) (*Store, error) {
	db, err := sqlx.Connect("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("mysql connect: %w", err)
	}

	// Connection pool tuning for a mail server workload
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)

	log.Info("mysql connected", zap.String("dsn", maskDSN(dsn)))

	return &Store{db: db, log: log}, nil
}

// Close releases the connection pool.
func (s *Store) Close() error {
	return s.db.Close()
}

// Ping checks that the database is reachable.
func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

// maskDSN hides the password from logs.
// Input:  "user:password@tcp(host:port)/db"
// Output: "user:***@tcp(host:port)/db"
func maskDSN(dsn string) string {
	for i, c := range dsn {
		if c == ':' {
			for j := i + 1; j < len(dsn); j++ {
				if dsn[j] == '@' {
					return dsn[:i+1] + "***" + dsn[j:]
				}
			}
		}
	}
	return dsn
}
