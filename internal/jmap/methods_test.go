package jmap

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestMailboxRole verifies that standard mailbox names are mapped
// to the correct JMAP roles as defined in RFC 8621 section 2.1.
func TestMailboxRole(t *testing.T) {
	tests := []struct {
		name     string
		expected string
	}{
		{"INBOX", "inbox"},
		{"Sent", "sent"},
		{"Trash", "trash"},
		{"Drafts", "drafts"},
		{"CustomFolder", ""},
		{"", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, mailboxRole(tt.name))
		})
	}
}

// TestParseAddressJMAP verifies address parsing used during JMAP authentication.
func TestParseAddressJMAP(t *testing.T) {
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

// TestCheckPasswordJMAP verifies bcrypt password verification in the JMAP package.
func TestCheckPasswordJMAP(t *testing.T) {
	// Use the same hash as the test user in migrations/001_init.sql
	// Hash of "password" with bcrypt cost 10
	hash := "$2a$10$92IXUNpkjO0rOQ5byMi.Ye4oKoEa3Ro9llC/.og/at2.uheWG/igi"

	assert.True(t, checkPassword("password", hash), "correct password must return true")
	assert.False(t, checkPassword("wrong", hash), "wrong password must return false")
	assert.False(t, checkPassword("", hash), "empty password must return false")
}
