package imap

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"golang.org/x/crypto/bcrypt"
)

// TestCheckPassword verifies bcrypt password verification.
func TestCheckPassword(t *testing.T) {
	// Generate a real bcrypt hash for testing
	hash, err := bcrypt.GenerateFromPassword([]byte("correctpassword"), bcrypt.MinCost)
	if err != nil {
		t.Fatal("failed to generate test hash")
	}

	tests := []struct {
		name     string
		password string
		hash     string
		expected bool
	}{
		{
			name:     "correct password returns true",
			password: "correctpassword",
			hash:     string(hash),
			expected: true,
		},
		{
			name:     "wrong password returns false",
			password: "wrongpassword",
			hash:     string(hash),
			expected: false,
		},
		{
			name:     "empty password returns false",
			password: "",
			hash:     string(hash),
			expected: false,
		},
		{
			name:     "invalid hash returns false",
			password: "anypassword",
			hash:     "notahash",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := checkPassword(tt.password, tt.hash)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestParseAddress verifies address parsing used during LOGIN.
func TestParseAddress(t *testing.T) {
	tests := []struct {
		name           string
		addr           string
		expectedLocal  string
		expectedDomain string
		ok             bool
	}{
		{
			name:           "valid address",
			addr:           "test@gomail.local",
			expectedLocal:  "test",
			expectedDomain: "gomail.local",
			ok:             true,
		},
		{
			name: "no at sign",
			addr: "invalid",
			ok:   false,
		},
		{
			name: "empty local part",
			addr: "@gomail.local",
			ok:   false,
		},
		{
			name: "empty domain",
			addr: "test@",
			ok:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			local, domain, ok := parseAddress(tt.addr)
			assert.Equal(t, tt.ok, ok)
			if tt.ok {
				assert.Equal(t, tt.expectedLocal, local)
				assert.Equal(t, tt.expectedDomain, domain)
			}
		})
	}
}
