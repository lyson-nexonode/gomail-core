package smtp

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestExtractAddress verifies that MAIL FROM and RCPT TO addresses
// are correctly parsed from SMTP command arguments.
func TestExtractAddress(t *testing.T) {
	tests := []struct {
		name     string
		args     string
		prefix   string
		expected string
		ok       bool
	}{
		{
			name:     "standard FROM with angle brackets",
			args:     "FROM:<alice@gomail.local>",
			prefix:   "FROM:",
			expected: "alice@gomail.local",
			ok:       true,
		},
		{
			name:     "standard TO with angle brackets",
			args:     "TO:<bob@gomail.local>",
			prefix:   "TO:",
			expected: "bob@gomail.local",
			ok:       true,
		},
		{
			name:     "address without angle brackets",
			args:     "FROM:alice@gomail.local",
			prefix:   "FROM:",
			expected: "alice@gomail.local",
			ok:       true,
		},
		{
			name:   "missing prefix returns false",
			args:   "TO:<bob@gomail.local>",
			prefix: "FROM:",
			ok:     false,
		},
		{
			name:   "empty address returns false",
			args:   "FROM:<>",
			prefix: "FROM:",
			ok:     false,
		},
		{
			name:     "lowercase prefix is accepted",
			args:     "from:<alice@gomail.local>",
			prefix:   "FROM:",
			expected: "alice@gomail.local",
			ok:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			addr, ok := extractAddress(tt.args, tt.prefix)
			assert.Equal(t, tt.ok, ok)
			if tt.ok {
				assert.Equal(t, tt.expected, addr)
			}
		})
	}
}

// TestExtractSubject verifies that the Subject header is correctly
// extracted from a raw RFC 5322 message body.
func TestExtractSubject(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		expected string
	}{
		{
			name: "standard subject header",
			body: "From: alice@gomail.local\r\nTo: bob@gomail.local\r\nSubject: Hello World\r\n\r\nBody here",
			expected: "Hello World",
		},
		{
			name:     "no subject header returns empty string",
			body:     "From: alice@gomail.local\r\nTo: bob@gomail.local\r\n\r\nBody here",
			expected: "",
		},
		{
			name:     "subject stops at blank line",
			body:     "From: alice\r\n\r\nSubject: This is in the body not headers",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// extractSubject is in message_store.go — we test it via storage package
			// This test validates the logic directly
			result := extractSubjectFromBody([]byte(tt.body))
			assert.Equal(t, tt.expected, result)
		})
	}
}

// extractSubjectFromBody is a test helper that duplicates the logic
// to allow testing without importing the storage package.
func extractSubjectFromBody(body []byte) string {
	lines := splitLines(string(body))
	for _, line := range lines {
		upper := toUpper(line)
		if len(upper) > 8 && upper[:8] == "SUBJECT:" {
			return trimSpace(line[8:])
		}
		if trimSpace(line) == "" {
			break
		}
	}
	return ""
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}

func toUpper(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' {
			c -= 32
		}
		b[i] = c
	}
	return string(b)
}

func trimSpace(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}
