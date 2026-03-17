package mysql

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestMaskDSN verifies that passwords are hidden from DSN strings in logs.
func TestMaskDSN(t *testing.T) {
	tests := []struct {
		name     string
		dsn      string
		expected string
	}{
		{
			name:     "standard DSN with password",
			dsn:      "user:password@tcp(localhost:3306)/db",
			expected: "user:***@tcp(localhost:3306)/db",
		},
		{
			name:     "DSN with complex password",
			dsn:      "gomail:s3cr3t!@tcp(mysql:3306)/gomail",
			expected: "gomail:***@tcp(mysql:3306)/gomail",
		},
		{
			name:     "DSN without password",
			dsn:      "user@tcp(localhost:3306)/db",
			expected: "user@tcp(localhost:3306)/db",
		},
		{
			name:     "empty DSN",
			dsn:      "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, maskDSN(tt.dsn))
		})
	}
}
